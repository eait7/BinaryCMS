package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ez8/gocms/pkg/plugin"

	_ "modernc.org/sqlite"
)

// MarketplaceHub is the central plugin hub that serves the marketplace catalog API.
type MarketplaceHub struct {
	db *sql.DB
}

// ---- Database Setup ----

func (m *MarketplaceHub) initDB() {
	dbPath := "plugins_data/marketplace_hub.db"
	os.MkdirAll("plugins_data", 0755)

	var err error
	m.db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("MarketplaceHub: Failed to open database: %v", err)
		return
	}

	m.db.Exec(`CREATE TABLE IF NOT EXISTS marketplace_plugins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		version TEXT NOT NULL DEFAULT '1.0.0',
		author TEXT DEFAULT 'BinaryCMS',
		price REAL DEFAULT 0,
		currency TEXT DEFAULT 'USD',
		binary_path TEXT DEFAULT '',
		sha256_hash TEXT DEFAULT '',
		icon_url TEXT DEFAULT '',
		screenshot_url TEXT DEFAULT '',
		category TEXT DEFAULT 'general',
		downloads INTEGER DEFAULT 0,
		min_core_version TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	m.db.Exec(`CREATE TABLE IF NOT EXISTS marketplace_licenses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		license_key TEXT NOT NULL UNIQUE,
		plugin_slug TEXT NOT NULL,
		buyer_email TEXT DEFAULT '',
		domain_locked TEXT DEFAULT '',
		activated_at DATETIME,
		status TEXT DEFAULT 'unused',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
}

// ---- Plugin Interface ----

func (m *MarketplaceHub) PluginName() string { return "Marketplace Hub" }

func (m *MarketplaceHub) HookBeforeFrontPageRender(content string) string { return content }

func (m *MarketplaceHub) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{
		{Label: "Marketplace Hub", URL: "/admin/plugin/marketplace-hub", Icon: "shopping-cart"},
	}
}

func (m *MarketplaceHub) HookDashboardWidget() string  { return "" }
func (m *MarketplaceHub) HookAdminTopRightWidget() string { return "" }
func (m *MarketplaceHub) HookUserProfileTab(userID int) string { return "" }
func (m *MarketplaceHub) HookUserAccountCard(userID int) string { return "" }
func (m *MarketplaceHub) HookUserRegistered(userID int) string { return "" }

// ---- Route Handler ----

func (m *MarketplaceHub) HookAdminRoute(route string) string {
	// Admin panel routes
	if route == "/admin/plugin/marketplace-hub" {
		return m.renderAdminDashboard()
	}

	// Handle admin form submissions
	if strings.HasPrefix(route, "/admin/plugin/marketplace-hub?") {
		return m.handleAdminAction(route)
	}

	// Public API: Catalog
	if route == "/api/plugin/marketplace/plugins" {
		return m.handleAPIPlugins()
	}

	// Public API: Validate license
	if route == "/api/plugin/marketplace/validate" {
		return m.handleAPIValidate(route)
	}

	// Public API: Download plugin binary
	if strings.HasPrefix(route, "/api/plugin/marketplace/download/") {
		slug := strings.TrimPrefix(route, "/api/plugin/marketplace/download/")
		return m.handleAPIDownload(slug)
	}

	return ""
}

// ---- API Handlers ----

func (m *MarketplaceHub) handleAPIPlugins() string {
	rows, err := m.db.Query(`SELECT slug, name, description, version, author, price, currency,
		icon_url, screenshot_url, downloads, sha256_hash, category, min_core_version, updated_at
		FROM marketplace_plugins ORDER BY name ASC`)
	if err != nil {
		return `{"error":"database error"}`
	}
	defer rows.Close()

	type PluginEntry struct {
		Slug          string  `json:"slug"`
		Name          string  `json:"name"`
		Description   string  `json:"description"`
		Version       string  `json:"version"`
		Author        string  `json:"author"`
		Price         float64 `json:"price"`
		Currency      string  `json:"currency"`
		IconURL       string  `json:"icon_url"`
		ScreenshotURL string  `json:"screenshot_url"`
		Downloads     int     `json:"downloads"`
		SHA256        string  `json:"sha256"`
		Category      string  `json:"category"`
		MinCoreVer    string  `json:"min_core_version"`
		UpdatedAt     string  `json:"updated_at"`
	}

	var plugins []PluginEntry
	for rows.Next() {
		var p PluginEntry
		if err := rows.Scan(&p.Slug, &p.Name, &p.Description, &p.Version, &p.Author,
			&p.Price, &p.Currency, &p.IconURL, &p.ScreenshotURL, &p.Downloads,
			&p.SHA256, &p.Category, &p.MinCoreVer, &p.UpdatedAt); err == nil {
			plugins = append(plugins, p)
		}
	}

	if plugins == nil {
		plugins = []PluginEntry{}
	}

	b, _ := json.Marshal(map[string]interface{}{"plugins": plugins})
	return string(b)
}

func (m *MarketplaceHub) handleAPIValidate(route string) string {
	// Parse query parameters (simplified since we receive via route string)
	// In practice, the client sends JSON POST body, but since go-plugin routes are string-based,
	// we handle it via query parameters for compatibility
	parts := strings.SplitN(route, "?", 2)
	if len(parts) < 2 {
		return `{"valid":false,"message":"Missing parameters"}`
	}

	params := parseQueryString(parts[1])
	slug := params["slug"]
	key := params["license_key"]
	domain := params["domain"]

	if slug == "" || key == "" {
		return `{"valid":false,"message":"Slug and license_key are required"}`
	}

	// Check if this license key exists and is valid
	var status string
	var pluginSlug string
	var domainLocked string
	err := m.db.QueryRow("SELECT status, plugin_slug, domain_locked FROM marketplace_licenses WHERE license_key = ?", key).
		Scan(&status, &pluginSlug, &domainLocked)

	if err != nil {
		return `{"valid":false,"message":"License key not found"}`
	}

	if pluginSlug != slug {
		return `{"valid":false,"message":"License key is not valid for this plugin"}`
	}

	if status == "revoked" {
		return `{"valid":false,"message":"This license has been revoked"}`
	}

	// If already domain-locked, verify the domain matches
	if domainLocked != "" && domainLocked != domain {
		return `{"valid":false,"message":"This license is already activated on a different domain"}`
	}

	// Lock to this domain and mark as active
	m.db.Exec("UPDATE marketplace_licenses SET domain_locked = ?, activated_at = ?, status = 'active' WHERE license_key = ?",
		domain, time.Now().Format("2006-01-02 15:04:05"), key)

	return `{"valid":true,"message":"License validated successfully"}`
}

func (m *MarketplaceHub) handleAPIDownload(slug string) string {
	// Look up the binary path for this plugin
	var binaryPath string
	err := m.db.QueryRow("SELECT binary_path FROM marketplace_plugins WHERE slug = ?", slug).Scan(&binaryPath)
	if err != nil || binaryPath == "" {
		return `{"error":"Plugin not found or binary not available"}`
	}

	// Read the binary file
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return `{"error":"Binary file not found on server"}`
	}

	// Increment download count
	m.db.Exec("UPDATE marketplace_plugins SET downloads = downloads + 1 WHERE slug = ?", slug)

	// Return raw binary (base64 encoded for JSON transport in go-plugin context)
	hash := sha256.Sum256(data)
	hexHash := hex.EncodeToString(hash[:])

	// Note: For the initial implementation, we return the binary path.
	// The actual binary download should use a dedicated HTTP endpoint outside go-plugin.
	return fmt.Sprintf(`{"binary_path":"%s","sha256":"%s","size":%d}`, binaryPath, hexHash, len(data))
}

// ---- Admin Dashboard ----

func (m *MarketplaceHub) renderAdminDashboard() string {
	// Count plugins
	var pluginCount int
	m.db.QueryRow("SELECT COUNT(*) FROM marketplace_plugins").Scan(&pluginCount)

	// Count licenses
	var licenseCount int
	m.db.QueryRow("SELECT COUNT(*) FROM marketplace_licenses").Scan(&licenseCount)

	// Count active licenses
	var activeCount int
	m.db.QueryRow("SELECT COUNT(*) FROM marketplace_licenses WHERE status = 'active'").Scan(&activeCount)

	// List all plugins
	rows, _ := m.db.Query("SELECT slug, name, version, price, downloads FROM marketplace_plugins ORDER BY name")
	defer rows.Close()

	var pluginRows string
	for rows.Next() {
		var slug, name, version string
		var price float64
		var downloads int
		rows.Scan(&slug, &name, &version, &price, &downloads)
		priceLabel := "Free"
		if price > 0 {
			priceLabel = fmt.Sprintf("$%.2f", price)
		}
		pluginRows += fmt.Sprintf(`<tr>
			<td><strong>%s</strong></td>
			<td>%s</td>
			<td>v%s</td>
			<td>%s</td>
			<td>%d</td>
			<td><form method="POST" action="/admin/plugin/marketplace-hub?action=delete&slug=%s" style="display:inline;" onsubmit="return confirm('Delete this plugin from the catalog?')">
				<button type="submit" class="btn btn-sm btn-ghost-danger">Remove</button>
			</form></td>
		</tr>`, name, slug, version, priceLabel, downloads, slug)
	}

	if pluginRows == "" {
		pluginRows = `<tr><td colspan="6" class="text-center text-secondary py-4">No plugins in the catalog yet.</td></tr>`
	}

	// List recent licenses
	licRows, _ := m.db.Query("SELECT license_key, plugin_slug, domain_locked, status, created_at FROM marketplace_licenses ORDER BY created_at DESC LIMIT 20")
	defer licRows.Close()

	var licenseRows string
	for licRows.Next() {
		var key, slug, domain, status, created string
		licRows.Scan(&key, &slug, &domain, &status, &created)
		if domain == "" {
			domain = "<em>Not activated</em>"
		}
		statusBadge := `<span class="badge bg-secondary">` + status + `</span>`
		if status == "active" {
			statusBadge = `<span class="badge bg-green">Active</span>`
		} else if status == "unused" {
			statusBadge = `<span class="badge bg-yellow">Unused</span>`
		}
		licenseRows += fmt.Sprintf(`<tr>
			<td><code>%s</code></td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
		</tr>`, key, slug, domain, statusBadge, created)
	}

	if licenseRows == "" {
		licenseRows = `<tr><td colspan="5" class="text-center text-secondary py-4">No licenses generated yet.</td></tr>`
	}

	return fmt.Sprintf(`
	<div class="row row-cards">
		<div class="col-sm-4">
			<div class="card">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Plugins in Catalog</div>
					</div>
					<div class="h1 mb-0 mt-2">%d</div>
				</div>
			</div>
		</div>
		<div class="col-sm-4">
			<div class="card">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Total Licenses</div>
					</div>
					<div class="h1 mb-0 mt-2">%d</div>
				</div>
			</div>
		</div>
		<div class="col-sm-4">
			<div class="card">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Active Licenses</div>
					</div>
					<div class="h1 mb-0 mt-2">%d</div>
				</div>
			</div>
		</div>
	</div>

	<div class="card mt-3">
		<div class="card-header"><h3 class="card-title">Add Plugin to Catalog</h3></div>
		<div class="card-body">
			<form method="POST" action="/admin/plugin/marketplace-hub?action=add">
				<div class="row g-3">
					<div class="col-md-3"><input type="text" class="form-control" name="slug" placeholder="Plugin Slug (e.g. seo_optimizer)" required></div>
					<div class="col-md-3"><input type="text" class="form-control" name="name" placeholder="Display Name" required></div>
					<div class="col-md-2"><input type="text" class="form-control" name="version" placeholder="Version" value="1.0.0"></div>
					<div class="col-md-2"><input type="number" step="0.01" class="form-control" name="price" placeholder="Price (0=free)" value="0"></div>
					<div class="col-md-2"><button type="submit" class="btn btn-primary w-100">Add Plugin</button></div>
				</div>
				<div class="row g-3 mt-1">
					<div class="col-md-6"><input type="text" class="form-control" name="description" placeholder="Description"></div>
					<div class="col-md-6"><input type="text" class="form-control" name="binary_path" placeholder="Binary path (e.g. /app/plugins/seo_optimizer)"></div>
				</div>
			</form>
		</div>
	</div>

	<div class="card mt-3">
		<div class="card-header"><h3 class="card-title">Plugin Catalog</h3></div>
		<div class="table-responsive">
			<table class="table card-table table-vcenter">
				<thead><tr><th>Name</th><th>Slug</th><th>Version</th><th>Price</th><th>Downloads</th><th></th></tr></thead>
				<tbody>%s</tbody>
			</table>
		</div>
	</div>

	<div class="card mt-3">
		<div class="card-header d-flex justify-content-between align-items-center">
			<h3 class="card-title">License Keys</h3>
			<form method="POST" action="/admin/plugin/marketplace-hub?action=generate_license" class="d-flex gap-2">
				<input type="text" class="form-control form-control-sm" name="plugin_slug" placeholder="Plugin slug" required style="width:200px;">
				<input type="email" class="form-control form-control-sm" name="buyer_email" placeholder="Buyer email" style="width:200px;">
				<button type="submit" class="btn btn-sm btn-primary text-nowrap">Generate Key</button>
			</form>
		</div>
		<div class="table-responsive">
			<table class="table card-table table-vcenter">
				<thead><tr><th>License Key</th><th>Plugin</th><th>Domain</th><th>Status</th><th>Created</th></tr></thead>
				<tbody>%s</tbody>
			</table>
		</div>
	</div>
	`, pluginCount, licenseCount, activeCount, pluginRows, licenseRows)
}

// ---- Admin Actions ----

func (m *MarketplaceHub) handleAdminAction(route string) string {
	parts := strings.SplitN(route, "?", 2)
	if len(parts) < 2 {
		return m.renderAdminDashboard()
	}

	params := parseQueryString(parts[1])
	action := params["action"]

	switch action {
	case "add":
		slug := params["slug"]
		name := params["name"]
		version := params["version"]
		price := params["price"]
		desc := params["description"]
		binaryPath := params["binary_path"]

		if slug != "" && name != "" {
			// Compute SHA-256 if binary exists
			sha := ""
			if binaryPath != "" {
				if data, err := os.ReadFile(binaryPath); err == nil {
					h := sha256.Sum256(data)
					sha = hex.EncodeToString(h[:])
				}
			}

			m.db.Exec(`INSERT OR REPLACE INTO marketplace_plugins (slug, name, description, version, price, binary_path, sha256_hash, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				slug, name, desc, version, price, binaryPath, sha, time.Now().Format("2006-01-02 15:04:05"))
		}

	case "delete":
		slug := params["slug"]
		if slug != "" {
			m.db.Exec("DELETE FROM marketplace_plugins WHERE slug = ?", slug)
		}

	case "generate_license":
		pluginSlug := params["plugin_slug"]
		buyerEmail := params["buyer_email"]
		if pluginSlug != "" {
			key := generateLicenseKey()
			m.db.Exec("INSERT INTO marketplace_licenses (license_key, plugin_slug, buyer_email, status) VALUES (?, ?, ?, 'unused')",
				key, pluginSlug, buyerEmail)
		}
	}

	return m.renderAdminDashboard()
}

// ---- Helpers ----

func generateLicenseKey() string {
	segments := make([]string, 4)
	for i := range segments {
		b := make([]byte, 2)
		rand.Read(b)
		segments[i] = strings.ToUpper(hex.EncodeToString(b))
	}
	return "BNCMS-" + strings.Join(segments, "-")
}

func parseQueryString(qs string) map[string]string {
	params := make(map[string]string)
	for _, pair := range strings.Split(qs, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	return params
}

// ---- Entry Point ----

func main() {
	hub := &MarketplaceHub{}
	hub.initDB()
	defer func() {
		if hub.db != nil {
			hub.db.Close()
		}
	}()

	plugin.ServePlugin(hub)
}
