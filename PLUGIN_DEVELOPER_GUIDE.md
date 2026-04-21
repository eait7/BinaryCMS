# 🧩 Go CareFi - Plugin Developer Guide

This guide establishes the strict standard for building modular, decoupled plugins in the Go CareFi ecosystem. Every feature (MikroTik, QuickBooks, WhatsApp) must be implemented using this exact architectural template to ensure absolute code cleanliness, concurrency safety, and long-term scalability.

---

## 📂 1. Standard Folder Structure

Every plugin residing in `/plugins_src/<plugin_name>` must strictly adhere to the following layout:

```text
/plugins_src/routeros_connector/
├── .gocms.plugin          # JSON manifest (Name, Version, Description)
├── main.go                # Entry point: Plugin instantiation & Interface hooks
├── /models/               # Database GORM structs (e.g., router.go, session.go)
├── /services/             # Isolated business logic (e.g., dialer.go, queue.go)
└── /views/
    └── dashboard.html     # Dedicated HTMX template fragments
```

**Rule**: `main.go` should only act as the traffic router mapping incoming Webhook routes to internal `/services` functions. It must NOT contain inline HTML strings or raw SQL logic.

---

## 🗄️ 2. Database Access (Centralized Handle)

Plugins are strictly prohibited from spawning their own SQLite or JSON instances. All state must be managed via the centralized **GORM Database Instance** residing in `cms.db`.

**The Standard Flow**:
When the core loads the plugin, it will inject the central GORM `*gorm.DB` instance.
Inside your `/models`, define your structs:

```go
package models

import "gorm.io/gorm"

// PPPoESession binds to the core Subscriber AccountID
type PPPoESession struct {
    gorm.Model
    AccountID string `gorm:"index"`
    Username  string
    RouterIP  string
}
```

In `main.go`, your plugin runs automatic migrations safely against the global table:
```go
func (p *MyPlugin) Init(db *gorm.DB) {
    p.DB = db
    // Merge schema definitions safely
    p.DB.AutoMigrate(&models.PPPoESession{}) 
}
```

---

## 📡 3. The Shared 'Bus' (Event-Driven Architecture)

Plugins must never forcefully call a function inside another plugin. All cross-plugin communication occurs across the asynchronous **Global Event Bus**.

### Publishing an Event
If the `quickbooks_billing_sync` plugin detects an unpaid customer, it simply shouts into the void without caring who listens:
```go
bus.Publish("EVENT_BILLING_OVERDUE", bus.Payload{
    "AccountID": "CUST-1044",
    "Amount": 150.00,
})
```

### Subscribing to an Event
The `routeros_connector` plugin (during its `Init()` sequence) actively subscribes to relevant events:
```go
bus.Subscribe("EVENT_BILLING_OVERDUE", func(payload bus.Payload) {
    accountID := payload["AccountID"].(string)
    
    // 1. GORM Query to find PPPoE Username from AccountID
    // 2. Queue a worker to execute the Suspension command!
    services.QueueSuspension(accountID)
})
```

---

## 🌐 4. HTMX Integration & Native UI Fragments

We are banishing monolithic inline HTML strings. To inject components into the **Tabler UI**, plugins utilize HTMX to fetch rendered nested templates asynchronously from `/templates/plugins/`.

1. **The Navigation Tab**:
   Use `HookAdminMenu()` to register the initial route in the sidebar:
   ```go
   func (p *MyPlugin) HookAdminMenu() []plugin.MenuItem {
       return []plugin.MenuItem{{Label: "Router Config", URL: "/admin/plugin/routers", Icon: "router"}}
   }
   ```

2. **The HTMX Route**:
   When the user visits `/admin/plugin/routers`, `main.go` parses the physical `.html` fragment file located logically in `/templates/plugins/<name>/dashboard.html`:
   ```go
   func (p *MyPlugin) HookAdminRoute(route string) string {
       // Pass structured data into the template engine implicitly
       data := p.FetchDashboardMetrics()
       return engine.RenderTemplate("plugins/routeros_connector/dashboard.html", data)
   }
   ```

3. **HTMX Triggers (In `dashboard.html`)**:
   Bind buttons directly to plugin sub-routes flawlessly without page reloads:
   ```html
   <button hx-post="/admin/plugin/routers?action=block&user={{.Username}}" 
           hx-target="#user-status-{{.ID}}" 
           class="btn btn-danger">
       Suspend User
   </button>
   ```

---
**Prepared by Antigravity Core.** These paradigms must be strictly enforced when developing the new "RouterOS Connector" as the standardized v2.0 plugin.
