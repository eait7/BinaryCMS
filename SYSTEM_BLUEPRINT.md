# 🚀 Go CareFi (GoCMS) - System Blueprint & Migration Guide

**Welcome to Go CareFi (GoCMS)!** 
If this is your first time reading this on a new system or inside a new AI context window, this document is your definitive "North Star." It explains exactly what we built, why we built it, how the code currently functions, what tech debt needs resolving, and the specific advanced goals we are targeting next.

---

## 📖 1. Business Context: The Origin Story
Originally, the platform was a fragmented legacy system heavily reliant on disjointed Python background daemons (like `wisp-monitor.py` and `wisp-qb-sync.py`), coupled with a monolithic PHP frontend. This caused massive performance overhead, synchronization issues, and difficulty maintaining a clean CRM (Customer Relationship Management) workflow for managing WISP (Wireless Internet Service Provider) clients.

**The Solution:** We engineered **Go CareFi** (built on the lightweight open-source framework `GoCMS`). It was built entirely in pure, deeply-concurrent Go (`1.21+`) and merged all those external bash/python shell scripts into a highly cohesive, natively compiled **Go Plugin architecture**.

The primary objective is to finalize an all-in-one Admin dashboard that handles real-time network telemetry (MikroTik/RouterOS), accounting (QuickBooks Online), and automated alerts (WhatsApp/WAHA) securely and blazingly fast.

---

## 🏗️ 2. Current Architecture & Code Structure

The platform functions as a core router/engine bridging an array of autonomous, hot-pluggable modules.

### A. The Core Engine
- **Language**: Pure Go.
- **Frontend Theme**: We are utilizing the **Tabler UI** framework, leveraging standard Go `html/template` rendering for an incredibly modern and responsive admin dashboard.
- **Data Persistence**: `SQLite3`. The primary database is globally located at `cms.db`, strictly managing core CMS configurations, theme setups, and admin authentication credentials.
- **Plugin Registry (`pkg/plugin`)**: The true magic of the system. This package exposes an interface forcing plugins to declare functions like:
  - `HookAdminMenu()` -> For placing items in the sidebar.
  - `HookAdminRoute()` -> For intercepting specific `/admin/plugin/X` routes and rendering HTML blocks or JSON arrays.
  - `HookDashboardWidget()` -> For pushing quick metrics onto the home screen.

### B. The Ecosystem (Active Plugins)
The legacy scripts were ported into independent micro-apps located inside `/plugins_src/`:
1. **`routeros_connector`**: Securely dials MikroTik arrays fetching live telemetry (Rx/Tx, Uptime) and handles Provisioning / Bulk Blocks.
2. **`quickbooks_billing_sync`**: Continuous CDC daemon pulling Customer AR balances.
3. **`waha_whatsapp_monitor`**: Autonomous loop evaluating Ping telemetry and dispatching alerts to WAHA instances.
4. **`admin_extensions` / `sysmon` / `speedtest_monitor`**: Proxies AI endpoints and tracks hardware.

---

## ⚠️ 3. The "Mess": Current Tech Debt
1. **State Fragmentation (The JSON Chaos)**: Plugins save configs into isolated `.json` stores causing relational disjointing.
2. **Isolated Plugin Boundaries**: Plugins cannot natively trigger each other.
3. **Duplicated Logical Polling**: `ping_monitor` and WAHA monitor run separate OS `ping` threads.
4. **Monolithic Frontend Go Strings**: HTML is concatenated inside `main.go`.

---

## 🎯 4. The Action Plan (Where To Start Next)

### 🟡 Phase A: Centralized GORM State Engine
**Objective**: Deprecate standalone `.json` config files. Use GORM to centralize all structures into `cms.db`.

### 🟡 Phase B: Core App Dependency Injection
**Objective**: Build an `AppCore` registry allowing plugins to register methods natively for inter-communication.

### 🟡 Phase C: The Global Event Bus
**Objective**: Build a Go `chan` Event Bus so plugins can Subscribe/Publish natively without raw HTTP wrapper calls.

### 🟡 Phase D: View/Template Extraction
**Objective**: Move all long-form HTML strings out of `main.go`. All HTMX fragments must be stored in a nested `/templates/plugins/` directory to keep the main binary clean and modular.

---

## 🛡️ 5. Data Integrity & Concurrency Specs

To achieve true enterprise scalability, the new architecture must aggressively adhere to the following principles:

### Schema Design: The Unified `AccountID`
We must abandon disparate string lookups. The GORM `cms.db` schema will revolve around a central **Subscriber** table that maps external relations structurally:
- A `Subscriber` entity holds a unique internal `AccountID`.
- This `AccountID` maps -> `QuickBooks_Customer_ID` (1:1 relation).
- This `AccountID` maps -> `PPPoE_Session` objects (1:N relation; a customer may have multiple internet links).
- **Enforcement**: This guarantees that a change in billing status for an `AccountID` can uniformly identify and target *all* associated PPPoE active tunnels instantaneously.

### Concurrency Management: The 40+ Router "Worker Pool"
Managing 40+ RouterOS devices requires precision. We cannot spawn 600 separate goroutines, causing file-descriptor exhaustion and CPU spikes on the backing T320 hardware.
- The `routeros_connector` must implement a **Worker Pool Pattern**.
- We will spin up a fixed number of workers (e.g., 5-10 concurrent workers).
- Devices needing telemetry updates or provisioning commands will be sent across a buffered `jobQueue <-chan RouterJob`.
- This strict queue prevents CPU-spiking and controls parallel bandwidth natively.

---

## 🔌 6. WISP-Specific Logic: The Automated "Kill-Switch"

The core value proposition of Go CareFi is autonomous ISP management. We need to formalize the **Non-Payment Suspension Workflow (The Kill-Switch)** across our Event Bus: 

1. **Detection**: `quickbooks_billing_sync` polls the APIs on schedule. It identifies an invoice marked as 'Overdue'.
2. **Event Dispatch**: The Quickbooks plugin fires a structured payload onto the Global Event Bus: `Publish("BILLING_OVERDUE", AccountID)`.
3. **Triggered Execution**: `routeros_connector` (which subs to this topic) catches the event.
4. **Automation**: Using the Unified Schema, the connector looks up the associated `PPPoE` identities for that `AccountID`. It dials the appropriate Routerboard queueing a worker, surgically shifts the user to a `"Suspended"` Address Address Profile, and actively terminates (`/ppp/active/remove`) their live tunnel enforcing a captive portal or walled garden.

> **🚀 TO THE AI / DEVELOPER RESUMING WORK:** 
> Acknowledge you have read and internalized this entire master blueprint. Set up the plugin scaffolding strictly according to the new `PLUGIN_DEVELOPER_GUIDE.md` and initiate **Phase A** immediately.
