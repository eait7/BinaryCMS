package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"net/url"
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
	primaryPath := "plugins_data/marketplace_hub.db"
	fallbackPath := "data/marketplace_hub.db" // /app/data is always writable (used by self-updater)

	os.MkdirAll("plugins_data", 0777)
	os.Chmod("plugins_data", 0777)
	os.MkdirAll("data", 0777)

	// Try to chmod existing DB files (succeeds only if we own them)
	for _, p := range []string{primaryPath, primaryPath + "-wal", primaryPath + "-shm"} {
		os.Chmod(p, 0666)
	}

	dbPath := primaryPath

	// Test write access on the primary path
	if !dbFileWritable(primaryPath) {
		log.Printf("MarketplaceHub: %s is not writable, falling back to %s", primaryPath, fallbackPath)
		// Migrate existing data if the fallback doesn't exist yet
		if _, err := os.Stat(fallbackPath); os.IsNotExist(err) {
			if src, err := os.ReadFile(primaryPath); err == nil {
				os.WriteFile(fallbackPath, src, 0666)
				log.Printf("MarketplaceHub: Migrated existing DB to %s", fallbackPath)
			}
		}
		dbPath = fallbackPath
	}

	log.Printf("MarketplaceHub: Using DB at %s", dbPath)

	var err error
	m.db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("MarketplaceHub: Failed to open database: %v", err)
		return
	}

	m.db.Exec("PRAGMA journal_mode=WAL")
	m.db.Exec("PRAGMA busy_timeout=5000")
	m.db.Exec("PRAGMA synchronous=NORMAL")

	if _, pingErr := m.db.Exec("PRAGMA user_version"); pingErr != nil {
		log.Printf("MarketplaceHub: DB write check failed: %v", pingErr)
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
		buy_url TEXT DEFAULT '',
		downloads INTEGER DEFAULT 0,
		min_core_version TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Ensure buy_url column exists for older databases
	m.db.Exec(`ALTER TABLE marketplace_plugins ADD COLUMN buy_url TEXT DEFAULT ''`)

	// Add new rich fields
	m.db.Exec(`ALTER TABLE marketplace_plugins ADD COLUMN long_description TEXT DEFAULT ''`)
	m.db.Exec(`ALTER TABLE marketplace_plugins ADD COLUMN features TEXT DEFAULT ''`)

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

	m.db.Exec(`CREATE TABLE IF NOT EXISTS marketplace_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
}

// dbFileWritable returns true if the file at path can be opened for writing.
func dbFileWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return false
	}
	f.Close()
	return true
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
	// Strip query params for path matching
	routePath := route
	if idx := strings.Index(route, "?"); idx != -1 {
		routePath = route[:idx]
	}

	// Handle admin form submissions
	if strings.HasPrefix(route, "/admin/plugin/marketplace-hub?") {
		return m.handleAdminAction(route)
	}

	// Admin panel routes
	if routePath == "/admin/plugin/marketplace-hub" {
		return m.renderAdminDashboard("")
	}

	// Public API: Catalog
	if routePath == "/api/plugin/marketplace-hub/plugins" {
		return m.handleAPIPlugins()
	}

	// Public API: Validate license
	if routePath == "/api/plugin/marketplace-hub/validate" {
		return m.handleAPIValidate(route)
	}

	// Public API: Download plugin binary
	if strings.HasPrefix(routePath, "/api/plugin/marketplace-hub/download/") {
		slug := strings.TrimPrefix(routePath, "/api/plugin/marketplace-hub/download/")
		return m.handleAPIDownload(route, slug)
	}

	// Webhook: LemonSqueezy
	if strings.HasPrefix(routePath, "/api/plugin/marketplace-hub/webhook/lemonsqueezy") {
		return m.handleLemonSqueezyWebhook(route)
	}

	// Debug route — shows DB state and write test
	if routePath == "/admin/plugin/marketplace-hub/debug" {
		return m.handleDebug()
	}

	return ""
}

func (m *MarketplaceHub) handleDebug() string {
	dbPath := "plugins_data/marketplace_hub.db"
	info, statErr := os.Stat(dbPath)
	var fileInfo string
	if statErr != nil {
		fileInfo = fmt.Sprintf("STAT ERROR: %v", statErr)
	} else {
		fileInfo = fmt.Sprintf("Size=%d Mode=%s", info.Size(), info.Mode())
	}
	writeErr := ""
	_, err := m.db.Exec("INSERT OR REPLACE INTO marketplace_settings (key, value) VALUES ('debug_test', ?)", fmt.Sprintf("%d", time.Now().Unix()))
	if err != nil {
		writeErr = "WRITE FAILED: " + err.Error()
	} else {
		writeErr = "WRITE OK"
	}
	rows, _ := m.db.Query("SELECT slug, name FROM marketplace_plugins")
	var plugins string
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var s, n string
			rows.Scan(&s, &n)
			plugins += fmt.Sprintf("<li>%s &mdash; %s</li>", s, n)
		}
	}
	if plugins == "" {
		plugins = "<li>(none)</li>"
	}
	cwd, _ := os.Getwd()
	return fmt.Sprintf(`<div class="card mt-3"><div class="card-header"><h3 class="card-title">Marketplace Hub Debug</h3></div><div class="card-body">
<p><strong>CWD:</strong> %s</p>
<p><strong>DB path:</strong> %s</p>
<p><strong>DB file:</strong> %s</p>
<p><strong>Write test:</strong> <code>%s</code></p>
<h4>Plugins in DB:</h4><ul>%s</ul>
<p><a href="/admin/plugin/marketplace-hub" class="btn btn-secondary">Back</a></p>
</div></div>`, cwd, dbPath, fileInfo, writeErr, plugins)
}


// ---- Settings Helpers ----

func (m *MarketplaceHub) getSetting(key string) string {
	var val string
	m.db.QueryRow("SELECT value FROM marketplace_settings WHERE key = ?", key).Scan(&val)
	return val
}

func (m *MarketplaceHub) setSetting(key, val string) {
	m.db.Exec("INSERT OR REPLACE INTO marketplace_settings (key, value) VALUES (?, ?)", key, val)
}

// ---- API Handlers ----

func (m *MarketplaceHub) handleAPIPlugins() string {
	rows, err := m.db.Query(`SELECT slug, name, description, version, author, price, currency,
		icon_url, screenshot_url, downloads, sha256_hash, category, buy_url, min_core_version, long_description, features, updated_at
		FROM marketplace_plugins ORDER BY name ASC`)
	if err != nil {
		return `{"error":"database error"}`
	}
	defer rows.Close()

	type PluginEntry struct {
		Slug             string  `json:"slug"`
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		Version          string  `json:"version"`
		Author           string  `json:"author"`
		Price            float64 `json:"price"`
		Currency         string  `json:"currency"`
		IconURL          string  `json:"icon_url"`
		ScreenshotURL    string  `json:"screenshot_url"`
		Downloads        int     `json:"downloads"`
		SHA256           string  `json:"sha256"`
		Category         string  `json:"category"`
		BuyURL           string  `json:"buy_url"`
		MinCoreVer       string  `json:"min_core_version"`
		LongDescription  string  `json:"long_description"`
		Features         string  `json:"features"`
		UpdatedAt        string  `json:"updated_at"`
	}

	var plugins []PluginEntry
	for rows.Next() {
		var p PluginEntry
		if err := rows.Scan(&p.Slug, &p.Name, &p.Description, &p.Version, &p.Author,
			&p.Price, &p.Currency, &p.IconURL, &p.ScreenshotURL, &p.Downloads,
			&p.SHA256, &p.Category, &p.BuyURL, &p.MinCoreVer, &p.LongDescription, &p.Features, &p.UpdatedAt); err == nil {
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

func (m *MarketplaceHub) handleAPIDownload(route string, slug string) string {
	parts := strings.SplitN(route, "?", 2)
	var qs string
	if len(parts) == 2 {
		qs = parts[1]
	}
	params := parseQueryString(qs)
	licenseKey := params["_license_key"]
	siteDomain := params["_site_domain"]

	// Look up the binary path and price for this plugin
	var binaryPath string
	var price float64
	err := m.db.QueryRow("SELECT binary_path, price FROM marketplace_plugins WHERE slug = ?", slug).Scan(&binaryPath, &price)
	if err != nil || binaryPath == "" {
		return `{"error":"Plugin not found or binary not available"}`
	}

	// If it's a paid plugin, validate the license
	if price > 0 {
		if licenseKey == "" {
			return `{"error":"License key is required to download this premium plugin"}`
		}

		var status, domainLocked, pluginSlug string
		err := m.db.QueryRow("SELECT status, domain_locked, plugin_slug FROM marketplace_licenses WHERE license_key = ?", licenseKey).
			Scan(&status, &domainLocked, &pluginSlug)

		if err != nil {
			return `{"error":"Invalid license key"}`
		}
		if pluginSlug != slug {
			return `{"error":"License key is not valid for this plugin"}`
		}
		if status == "revoked" {
			return `{"error":"This license has been revoked"}`
		}
		if domainLocked != "" && domainLocked != siteDomain {
			return `{"error":"This license is locked to a different domain"}`
		}
	}

	// Read the binary file
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return `{"error":"Binary file not found on server"}`
	}

	// Increment download count
	m.db.Exec("UPDATE marketplace_plugins SET downloads = downloads + 1 WHERE slug = ?", slug)

	// Return raw binary string directly via RPC. The CMS handler will detect it doesn't
	// look like JSON (starts with '{' and ends with '}') and will serve it as application/octet-stream.
	return string(data)
}

// ---- Admin Dashboard ----

func (m *MarketplaceHub) renderAdminDashboard(editSlug string) string {
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
	rows, err := m.db.Query("SELECT slug, name, version, price, downloads, buy_url FROM marketplace_plugins ORDER BY name")
	var pluginRows string
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var slug, name, version, buyUrl string
			var price float64
			var downloads int
			rows.Scan(&slug, &name, &version, &price, &downloads, &buyUrl)
			priceLabel := "Free"
			if price > 0 {
				priceLabel = fmt.Sprintf("$%.2f", price)
			}
			buyLabel := ""
			if buyUrl != "" {
				buyLabel = `<span class="badge bg-purple">Has Buy Link</span>`
			}
			pluginRows += fmt.Sprintf(`<tr>
				<td><strong>%s</strong> %s</td>
				<td>%s</td>
				<td>v%s</td>
				<td>%s</td>
				<td>%d</td>
				<td><form method="POST" action="/admin/plugin/marketplace-hub" enctype="multipart/form-data" style="display:inline;">
					<input type="hidden" name="action" value="edit_form">
					<input type="hidden" name="slug" value="%s">
					<button type="submit" class="btn btn-sm btn-ghost-primary me-2">Edit</button>
				</form>
				<form method="POST" action="/admin/plugin/marketplace-hub" enctype="multipart/form-data" style="display:inline;" onsubmit="return confirm('Delete this plugin from the catalog?')">
					<input type="hidden" name="action" value="delete">
					<input type="hidden" name="slug" value="%[1]s">
					<button type="submit" class="btn btn-sm btn-ghost-danger">Remove</button>
				</form></td>
			</tr>`, name, buyLabel, slug, version, priceLabel, downloads, slug)
		}
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

	// Handle Edit Mode Data
	formTitle := "Add Plugin to Catalog"
	submitBtn := "Add Plugin"
	actionName := "add"
	var eSlug, eName, eDesc, eVersion, eBuyUrl, eLongDesc, eFeatures string
	var ePrice float64
	eVersion = "1.0.0"
	ePrice = 0

	if editSlug != "" {
		m.db.QueryRow("SELECT slug, name, description, version, price, buy_url, long_description, features FROM marketplace_plugins WHERE slug = ?", editSlug).
			Scan(&eSlug, &eName, &eDesc, &eVersion, &ePrice, &eBuyUrl, &eLongDesc, &eFeatures)
		if eSlug != "" {
			formTitle = "Edit Plugin: " + eName
			submitBtn = "Save Changes"
			actionName = "edit"
		}
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
		<div class="card-header d-flex justify-content-between align-items-center">
			<h3 class="card-title">%s</h3>
			%s
		</div>
		<div class="card-body">
			<form method="POST" action="/admin/plugin/marketplace-hub" enctype="multipart/form-data">
				<input type="hidden" name="action" value="%s">
				<input type="hidden" name="original_slug" value="%s">
				<div class="row g-3">
					<div class="col-md-3"><input type="text" class="form-control" name="slug" placeholder="Plugin Slug (e.g. seo_optimizer)" value="%s" required></div>
					<div class="col-md-3"><input type="text" class="form-control" name="name" placeholder="Display Name" value="%s" required></div>
					<div class="col-md-2"><input type="text" class="form-control" name="version" placeholder="Version" value="%s"></div>
					<div class="col-md-2"><input type="number" step="0.01" class="form-control" name="price" placeholder="Price (0=free)" value="%.2f"></div>
					<div class="col-md-2"><button type="submit" class="btn btn-primary w-100">%s</button></div>
				</div>
				<div class="row g-3 mt-2">
					<div class="col-md-6"><input type="text" class="form-control" name="description" placeholder="Short Description (1 sentence)" value="%s"></div>
					<div class="col-md-6"><input type="text" class="form-control" name="buy_url" placeholder="LemonSqueezy Buy URL (optional)" value="%s"></div>
				</div>
				<div class="row g-3 mt-2">
					<div class="col-md-4">
						<label class="form-label">Plugin Icon (Image)</label>
						<input type="file" class="form-control" name="icon_image" accept="image/*">
					</div>
					<div class="col-md-4">
						<label class="form-label">Screenshot (Image)</label>
						<input type="file" class="form-control" name="screenshot_image" accept="image/*">
					</div>
					<div class="col-md-4">
						<label class="form-label">Plugin Binary (Executable)</label>
						<input type="file" class="form-control" name="plugin_binary" title="Upload compiled plugin binary">
					</div>
				</div>
				<div class="row g-3 mt-2">
					<div class="col-md-6">
						<label class="form-label">Long Description (Markdown/HTML)</label>
						<textarea class="form-control" name="long_description" rows="5" placeholder="Detailed description...">%s</textarea>
					</div>
					<div class="col-md-6">
						<label class="form-label">Features (Markdown/HTML)</label>
						<textarea class="form-control" name="features" rows="5" placeholder="List of features...">%s</textarea>
					</div>
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

	<div class="card mt-3">
		<div class="card-header"><h3 class="card-title">Hub Settings</h3></div>
		<div class="card-body">
			<form method="POST" action="/admin/plugin/marketplace-hub?action=settings">
				<div class="row g-3">
					<div class="col-md-6">
						<label class="form-label">LemonSqueezy Webhook Secret</label>
						<input type="text" class="form-control" name="ls_secret" value="%s">
						<small class="form-hint">Used to verify webhooks from LemonSqueezy</small>
					</div>
				</div>
				<h4 class="mt-4 mb-2">SMTP Settings (for license emails)</h4>
				<div class="row g-3">
					<div class="col-md-3"><label class="form-label">Host</label><input type="text" class="form-control" name="smtp_host" value="%s"></div>
					<div class="col-md-2"><label class="form-label">Port</label><input type="text" class="form-control" name="smtp_port" value="%s"></div>
					<div class="col-md-3"><label class="form-label">Username</label><input type="text" class="form-control" name="smtp_user" value="%s"></div>
					<div class="col-md-4"><label class="form-label">Password</label><input type="password" class="form-control" name="smtp_pass" value="%s"></div>
				</div>
				<div class="mt-3">
					<button type="submit" class="btn btn-primary">Save Settings</button>
				</div>
			</form>
		</div>
	</div>
	`, pluginCount, licenseCount, activeCount, formTitle, map[bool]string{true: `<a href="/admin/plugin/marketplace-hub" class="btn btn-sm">Cancel Edit</a>`, false: ""}[actionName == "edit"], actionName, eSlug, eSlug, eName, eVersion, ePrice, submitBtn, eDesc, eBuyUrl, eLongDesc, eFeatures, pluginRows, licenseRows,
		m.getSetting("ls_secret"), m.getSetting("smtp_host"), m.getSetting("smtp_port"), m.getSetting("smtp_user"), m.getSetting("smtp_pass"))
}

// redirectResponse returns a self-redirecting page for SUCCESSES.
func redirectResponse(dest, message string) string {
	return fmt.Sprintf(`<div class="alert alert-success">%s</div>
<script>window.location.href = '%s';</script>`, message, dest)
}

// errorResponse returns a permanent error page (no redirect) so the user can read the error.
func errorResponse(message string) string {
	return fmt.Sprintf(`<div class="alert alert-danger" style="padding:1rem;margin:1rem 0;"><strong>Error:</strong> %s</div>
<p><a href="/admin/plugin/marketplace-hub" class="btn btn-secondary">← Back to Marketplace Hub</a></p>`, message)
}

// ---- Admin Actions ----

func (m *MarketplaceHub) handleAdminAction(route string) string {
	parts := strings.SplitN(route, "?", 2)
	if len(parts) < 2 {
		log.Printf("[MarketplaceHub] handleAdminAction: no query string in route: %q", route)
		return m.renderAdminDashboard("")
	}

	params := parseQueryString(parts[1])
	action := params["action"]
	log.Printf("[MarketplaceHub] handleAdminAction: action=%q slug=%q name=%q (route len=%d)", action, params["slug"], params["name"], len(route))

	switch action {
	case "edit_form":
		// Edit form is a GET-like action — render the full dashboard with edit mode.
		// This is fine since it comes from a button click, not a file-upload POST.
		return m.renderAdminDashboard(params["slug"])

	case "add", "edit":
		slug := params["slug"]
		name := params["name"]
		version := params["version"]
		price_str := params["price"]
		desc := params["description"]
		uploadedFile := params["__file_plugin_binary"]
		buyUrl := params["buy_url"]
		longDesc := params["long_description"]
		features := params["features"]

		log.Printf("[MarketplaceHub] %s: slug=%q name=%q version=%q price=%q uploadedFile=%q", action, slug, name, version, price_str, uploadedFile)

		var price float64
		fmt.Sscanf(price_str, "%f", &price)

		var finalBinaryPath, sha, iconUrl, screenshotUrl string

		uploadedIcon := params["__file_icon_image"]
		if uploadedIcon != "" {
			os.MkdirAll("public/uploads/hub", 0755)
			iconName := slug + "_icon.png"
			if err := moveFile(uploadedIcon, "public/uploads/hub/"+iconName); err != nil {
				log.Printf("[MarketplaceHub] icon move error: %v", err)
			} else {
				iconUrl = "/uploads/hub/" + iconName
			}
		}

		uploadedScreenshot := params["__file_screenshot_image"]
		if uploadedScreenshot != "" {
			os.MkdirAll("public/uploads/hub", 0755)
			ssName := slug + "_screenshot.png"
			if err := moveFile(uploadedScreenshot, "public/uploads/hub/"+ssName); err != nil {
				log.Printf("[MarketplaceHub] screenshot move error: %v", err)
			} else {
				screenshotUrl = "/uploads/hub/" + ssName
			}
		}

		if slug == "" || name == "" {
			log.Printf("[MarketplaceHub] missing slug or name, returning error")
			return errorResponse(fmt.Sprintf("Slug and Name are required. Received: slug=%q, name=%q. Check that the form fields are not empty and that the CMS is passing multipart form data correctly.", slug, name))
		}

		if uploadedFile != "" {
			os.MkdirAll("plugins_data/hub_binaries", 0755)
			finalBinaryPath = "plugins_data/hub_binaries/" + slug
			if data, err := os.ReadFile(uploadedFile); err == nil {
				h := sha256.Sum256(data)
				sha = hex.EncodeToString(h[:])
				if err2 := moveFile(uploadedFile, finalBinaryPath); err2 != nil {
					log.Printf("[MarketplaceHub] binary move error: %v", err2)
				}
				if err3 := os.Chmod(finalBinaryPath, 0755); err3 != nil {
					log.Printf("[MarketplaceHub] chmod error: %v", err3)
				}
			} else {
				log.Printf("[MarketplaceHub] ReadFile error for %q: %v", uploadedFile, err)
				finalBinaryPath = ""
			}
		}

		if action == "edit" {
			var existBin, existSha, existIcon, existSs string
			m.db.QueryRow("SELECT binary_path, sha256_hash, icon_url, screenshot_url FROM marketplace_plugins WHERE slug = ?", params["original_slug"]).
				Scan(&existBin, &existSha, &existIcon, &existSs)
			if finalBinaryPath == "" {
				finalBinaryPath = existBin
				sha = existSha
			}
			if iconUrl == "" {
				iconUrl = existIcon
			}
			if screenshotUrl == "" {
				screenshotUrl = existSs
			}
			_, err := m.db.Exec(`UPDATE marketplace_plugins SET slug=?, name=?, description=?, version=?, price=?, binary_path=?, sha256_hash=?, buy_url=?, icon_url=?, screenshot_url=?, long_description=?, features=?, updated_at=? WHERE slug=?`,
				slug, name, desc, version, price, finalBinaryPath, sha, buyUrl, iconUrl, screenshotUrl, longDesc, features, time.Now().Format("2006-01-02 15:04:05"), params["original_slug"])
			if err != nil {
				log.Printf("[MarketplaceHub] UPDATE error: %v", err)
				return errorResponse("DB Error updating plugin: " + err.Error())
			}
			log.Printf("[MarketplaceHub] Plugin updated: %s", slug)
			return redirectResponse("/admin/plugin/marketplace-hub", "Plugin '"+name+"' updated successfully.")
		}

		// add
		_, err := m.db.Exec(`INSERT OR REPLACE INTO marketplace_plugins (slug, name, description, version, price, binary_path, sha256_hash, buy_url, icon_url, screenshot_url, long_description, features, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			slug, name, desc, version, price, finalBinaryPath, sha, buyUrl, iconUrl, screenshotUrl, longDesc, features, time.Now().Format("2006-01-02 15:04:05"))
		if err != nil {
			log.Printf("[MarketplaceHub] INSERT error: %v", err)
			return errorResponse("DB Error saving plugin: " + err.Error())
		}
		log.Printf("[MarketplaceHub] Plugin added: %s", slug)
		return redirectResponse("/admin/plugin/marketplace-hub", "Plugin '"+name+"' added to catalog successfully.")

	case "delete":
		slug := params["slug"]
		if slug != "" {
			m.db.Exec("DELETE FROM marketplace_plugins WHERE slug = ?", slug)
			log.Printf("[MarketplaceHub] Plugin deleted: %s", slug)
		}
		return redirectResponse("/admin/plugin/marketplace-hub", "Plugin removed from catalog.")

	case "generate_license":
		pluginSlug := params["plugin_slug"]
		buyerEmail := params["buyer_email"]
		if pluginSlug != "" {
			key := generateLicenseKey()
			m.db.Exec("INSERT INTO marketplace_licenses (license_key, plugin_slug, buyer_email, status) VALUES (?, ?, ?, 'unused')",
				key, pluginSlug, buyerEmail)
			log.Printf("[MarketplaceHub] License generated for plugin %s, buyer %s", pluginSlug, buyerEmail)
			return redirectResponse("/admin/plugin/marketplace-hub", "License key generated: <strong>"+key+"</strong>")
		}
		return redirectResponse("/admin/plugin/marketplace-hub", "Error: Plugin slug is required to generate a license.")

	case "settings":
		m.setSetting("ls_secret", params["ls_secret"])
		m.setSetting("smtp_host", params["smtp_host"])
		m.setSetting("smtp_port", params["smtp_port"])
		m.setSetting("smtp_user", params["smtp_user"])
		m.setSetting("smtp_pass", params["smtp_pass"])
		return redirectResponse("/admin/plugin/marketplace-hub", "Settings saved.")
	}

	return m.renderAdminDashboard("")
}

// ---- Helpers ----

// moveFile copies src to dst and removes src. Unlike os.Rename, this works
// across filesystem boundaries (e.g. /tmp → /app/plugins_data).
func moveFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("open dst %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	out.Close()
	in.Close()
	os.Remove(src) // best-effort cleanup
	return nil
}

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
			k, _ := url.QueryUnescape(kv[0])
			v, _ := url.QueryUnescape(kv[1])
			// Also replace '+' with space manually as QueryUnescape might not handle it in all contexts depending on Go version
			k = strings.ReplaceAll(k, "+", " ")
			v = strings.ReplaceAll(v, "+", " ")
			params[k] = v
		}
	}
	return params
}

// ---- Webhook Handlers ----

func (m *MarketplaceHub) handleLemonSqueezyWebhook(route string) string {
	params := parseQueryString(strings.SplitN(route, "?", 2)[1])
	body, _ := url.QueryUnescape(params["__body"])
	sig, _ := url.QueryUnescape(params["__signature"])

	secret := m.getSetting("ls_secret")
	if secret != "" {
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(body))
		expectedSig := hex.EncodeToString(h.Sum(nil))
		if sig != expectedSig {
			return `{"error":"invalid signature"}`
		}
	}

	var payload struct {
		Meta struct {
			EventName string `json:"event_name"`
		} `json:"meta"`
		Data struct {
			Attributes struct {
				Status      string `json:"status"`
				UserEmail   string `json:"user_email"`
				FirstOrderItem struct {
					VariantID int `json:"variant_id"`
				} `json:"first_order_item"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return `{"error":"invalid payload"}`
	}

	// Only process successful order creations
	if payload.Meta.EventName != "order_created" || payload.Data.Attributes.Status != "paid" {
		return `{"status":"ignored"}`
	}

	buyerEmail := payload.Data.Attributes.UserEmail
	// Here you would map VariantID or ProductID to a plugin slug. 
	// For simplicity, we assume one premium plugin for now or we could store variant_id in DB.
	// Since we don't have variant_id in DB yet, let's just log it and generate a key for the first premium plugin
	var pluginSlug, pluginName string
	err := m.db.QueryRow("SELECT slug, name FROM marketplace_plugins WHERE price > 0 LIMIT 1").Scan(&pluginSlug, &pluginName)
	if err != nil {
		log.Printf("Webhook: No premium plugin found to associate with order")
		return `{"error":"no premium plugin found"}`
	}

	key := generateLicenseKey()
	m.db.Exec("INSERT INTO marketplace_licenses (license_key, plugin_slug, buyer_email, status) VALUES (?, ?, ?, 'unused')",
		key, pluginSlug, buyerEmail)

	log.Printf("Webhook processed: generated key %s for %s", key, buyerEmail)

	// Send email
	go m.sendLicenseEmail(buyerEmail, pluginName, key)

	return `{"status":"success"}`
}

func (m *MarketplaceHub) sendLicenseEmail(to, pluginName, licenseKey string) {
	host := m.getSetting("smtp_host")
	port := m.getSetting("smtp_port")
	user := m.getSetting("smtp_user")
	pass := m.getSetting("smtp_pass")

	if host == "" || port == "" {
		log.Printf("Email not sent: SMTP settings not configured")
		return
	}

	auth := smtp.PlainAuth("", user, pass, host)
	msg := []byte("To: " + to + "\r\n" +
		"Subject: Your BinaryCMS Plugin License Key\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n\r\n" +
		"<h2>Thank you for your purchase!</h2>" +
		"<p>Your license key for <strong>" + pluginName + "</strong> is:</p>" +
		"<h3><code>" + licenseKey + "</code></h3>" +
		"<p>Enter this key in the Plugin Store within your BinaryCMS dashboard to activate the plugin.</p>")

	err := smtp.SendMail(host+":"+port, auth, user, []string{to}, msg)
	if err != nil {
		log.Printf("Failed to send license email: %v", err)
	} else {
		log.Printf("License email sent successfully to %s", to)
	}
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
