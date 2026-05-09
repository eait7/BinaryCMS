package main

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ez8/gocms/pkg/plugin"
	"github.com/google/uuid"
	hplugin "github.com/hashicorp/go-plugin"
	_ "modernc.org/sqlite"
)

type SitemapGeneratorPlugin struct {
	db *sql.DB
}

func (p *SitemapGeneratorPlugin) initDB() {
	os.MkdirAll("plugins_data", 0777)
	dbPath := "plugins_data/sitemap_generator.db"

	var err error
	p.db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("SitemapGenerator: Failed to open DB: %v", err)
		return
	}

	p.db.Exec("PRAGMA journal_mode=WAL")
	p.db.Exec("PRAGMA busy_timeout=5000")
	p.db.Exec("PRAGMA synchronous=NORMAL")

	p.db.Exec(`CREATE TABLE IF NOT EXISTS sitemap_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)

	p.db.Exec(`CREATE TABLE IF NOT EXISTS submission_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		engine TEXT NOT NULL,
		status TEXT NOT NULL,
		response_code INTEGER,
		url_count INTEGER,
		submitted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	p.db.Exec(`CREATE TABLE IF NOT EXISTS custom_urls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL UNIQUE,
		priority REAL DEFAULT 0.5,
		changefreq TEXT DEFAULT 'weekly',
		label TEXT
	)`)

	if p.getSetting("indexnow_key") == "" {
		p.setSetting("indexnow_key", strings.ReplaceAll(uuid.New().String(), "-", ""))
	}
}

func (p *SitemapGeneratorPlugin) getSetting(key string) string {
	var val string
	p.db.QueryRow("SELECT value FROM sitemap_settings WHERE key = ?", key).Scan(&val)
	return val
}

func (p *SitemapGeneratorPlugin) setSetting(key, val string) {
	p.db.Exec("INSERT OR REPLACE INTO sitemap_settings (key, value) VALUES (?, ?)", key, val)
}

func (p *SitemapGeneratorPlugin) PluginName() string { return "Sitemap Generator v1.0" }

func (p *SitemapGeneratorPlugin) HookBeforeFrontPageRender(content string) string {
	key := p.getSetting("indexnow_key")
	if key != "" && strings.Contains(content, "</head>") {
		meta := fmt.Sprintf("\n\t<meta name=\"indexnow-key\" content=\"%s\" />\n", key)
		return strings.Replace(content, "</head>", meta+"</head>", 1)
	}
	return content
}

func (p *SitemapGeneratorPlugin) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{{Label: "Sitemap Generator", URL: "/admin/plugin/sitemap-generator", Icon: "sitemap"}}
}

func (p *SitemapGeneratorPlugin) HookDashboardWidget() string {
	var lastSub string
	err := p.db.QueryRow("SELECT submitted_at FROM submission_log ORDER BY submitted_at DESC LIMIT 1").Scan(&lastSub)
	if err != nil || lastSub == "" {
		lastSub = "Never"
	}
	
	statusColor := "text-green"
	statusText := "● Sitemap Active"
	if p.getSetting("site_url") == "" {
		statusColor = "text-yellow"
		statusText = "● Needs Configuration"
	}

	return fmt.Sprintf(`
	<div class="card">
		<div class="card-body d-flex align-items-center gap-3">
			<span class="%s">%s</span>
			<small class="text-muted">Last submitted: %s</small>
			<a href="/admin/plugin/sitemap-generator" class="btn btn-sm btn-outline-primary ms-auto">Manage</a>
		</div>
	</div>`, statusColor, statusText, lastSub)
}

func (p *SitemapGeneratorPlugin) HookAdminRoute(route string) string {
	routePath := route
	if idx := strings.Index(route, "?"); idx != -1 {
		routePath = route[:idx]
	}

	if routePath == "/sitemap.xml" || routePath == "/api/plugin/sitemap-generator/sitemap.xml" {
		return p.generateSitemapXML()
	}

	if routePath == "/admin/plugin/sitemap-generator/view" {
		xml := p.generateSitemapXML()
		escaped := strings.ReplaceAll(xml, "&", "&amp;")
		escaped = strings.ReplaceAll(escaped, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		
		return fmt.Sprintf(`
		<div class="page-header d-print-none">
			<div class="row align-items-center">
				<div class="col">
					<h2 class="page-title">Sitemap Source</h2>
				</div>
				<div class="col-auto ms-auto d-print-none">
					<a href="/admin/plugin/sitemap-generator" class="btn btn-primary">Back to Dashboard</a>
				</div>
			</div>
		</div>
		<div class="page-body">
			<div class="card">
				<div class="card-body" style="background: #1e1e1e; color: #d4d4d4; padding: 20px; border-radius: 4px; overflow-x: auto;">
					<pre style="margin: 0;"><code>%s</code></pre>
				</div>
			</div>
		</div>
		`, escaped)
	}

	if strings.HasPrefix(route, "/admin/plugin/sitemap-generator?") {
		return p.handleAdminAction(route)
	}

	if routePath == "/admin/plugin/sitemap-generator" {
		return p.renderDashboard()
	}

	return ""
}

func parseQueryString(qs string) map[string]string {
	m := make(map[string]string)
	params, _ := url.ParseQuery(qs)
	for k, v := range params {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

func (p *SitemapGeneratorPlugin) handleAdminAction(route string) string {
	parts := strings.SplitN(route, "?", 2)
	params := parseQueryString(parts[1])
	action := params["action"]

	switch action {
	case "settings":
		p.setSetting("site_url", strings.TrimRight(params["site_url"], "/"))
		p.setSetting("auto_schedule", params["auto_schedule"])
		p.setSetting("default_changefreq", params["default_changefreq"])
		p.setSetting("default_priority", params["default_priority"])
		p.setSetting("exclude_patterns", params["exclude_patterns"])
		return p.redirect("/admin/plugin/sitemap-generator", "Settings saved successfully.")
		
	case "add_url":
		p.db.Exec("INSERT INTO custom_urls (url, priority, changefreq, label) VALUES (?, ?, ?, ?)",
			params["url"], params["priority"], params["changefreq"], params["label"])
		return p.redirect("/admin/plugin/sitemap-generator", "Custom URL added.")

	case "delete_url":
		p.db.Exec("DELETE FROM custom_urls WHERE id = ?", params["id"])
		return p.redirect("/admin/plugin/sitemap-generator", "Custom URL removed.")

	case "generate":
		p.generateSitemapXML()
		return p.redirect("/admin/plugin/sitemap-generator", "Sitemap generated manually.")

	case "submit_all":
		go p.submitToEngines("")
		return p.redirect("/admin/plugin/sitemap-generator", "Submission process started in the background.")

	case "submit_one":
		engine := params["engine"]
		go p.submitToEngines(engine)
		return p.redirect("/admin/plugin/sitemap-generator", "Submission to "+engine+" started in the background.")
	}

	return p.renderDashboard()
}

func (p *SitemapGeneratorPlugin) redirect(dest, msg string) string {
	return fmt.Sprintf(`<div class="alert alert-success">%s</div><script>window.location.href='%s';</script>`, msg, dest)
}

// Structs for XML Sitemap
type UrlSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	Urls    []Url    `xml:"url"`
}

type Url struct {
	Loc        string  `xml:"loc"`
	LastMod    string  `xml:"lastmod,omitempty"`
	ChangeFreq string  `xml:"changefreq,omitempty"`
	Priority   float64 `xml:"priority,omitempty"`
}

func (p *SitemapGeneratorPlugin) generateSitemapXML() string {
	siteURL := p.getSetting("site_url")
	var debugLog string

	if siteURL == "" {
		return debugLog + `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></urlset>`
	}

	defFreq := p.getSetting("default_changefreq")
	if defFreq == "" {
		defFreq = "weekly"
	}
	var defPri float64 = 0.5
	fmt.Sscanf(p.getSetting("default_priority"), "%f", &defPri)

	excludeStr := p.getSetting("exclude_patterns")
	var excludes []*regexp.Regexp
	for _, pattern := range strings.Split(excludeStr, "\n") {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			if r, err := regexp.Compile(pattern); err == nil {
				excludes = append(excludes, r)
			}
		}
	}

	dbPath := "data/cms.db"
	if _, err := os.Stat("/app/data/cms.db"); err == nil {
		dbPath = "/app/data/cms.db"
	} else if _, err := os.Stat("cms.db"); err == nil {
		dbPath = "cms.db"
	}

	cmsDB, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	var urls []Url
	debugLog += fmt.Sprintf("<!-- DB Path: %s -->\n", dbPath)

	if err != nil {
		debugLog += fmt.Sprintf("<!-- Open Error: %v -->\n", err)
	} else {
		defer cmsDB.Close()
		
		// Query Pages
		rows, err := cmsDB.Query("SELECT slug, updated_at FROM pages WHERE status='published'")
		if err != nil {
			debugLog += fmt.Sprintf("<!-- Pages Query Error: %v -->\n", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var slug string
				var updatedAt sql.NullString
				rows.Scan(&slug, &updatedAt)
				
				loc := siteURL
				if slug != "home" && slug != "index" && slug != "/" {
					if !strings.HasPrefix(slug, "/") {
						loc += "/"
					}
					loc += slug
				}

				excluded := false
				for _, r := range excludes {
					if r.MatchString(loc) {
						excluded = true
						break
					}
				}
				
				if !excluded {
					urls = append(urls, Url{
						Loc: loc,
						LastMod: parseDate(updatedAt.String),
						ChangeFreq: defFreq,
						Priority: defPri,
					})
				}
			}
		}

		// Query Posts
		prows, perr := cmsDB.Query("SELECT slug, updated_at FROM posts WHERE status='published'")
		if perr != nil {
			debugLog += fmt.Sprintf("<!-- Posts Query Error: %v -->\n", perr)
		} else {
			defer prows.Close()
			for prows.Next() {
				var slug string
				var updatedAt sql.NullString
				prows.Scan(&slug, &updatedAt)
				
				loc := siteURL + "/post/" + slug

				excluded := false
				for _, r := range excludes {
					if r.MatchString(loc) {
						excluded = true
						break
					}
				}
				
				if !excluded {
					urls = append(urls, Url{
						Loc: loc,
						LastMod: parseDate(updatedAt.String),
						ChangeFreq: defFreq,
						Priority: defPri,
					})
				}
			}
		}
	}

	// Add custom URLs
	cRows, err := p.db.Query("SELECT url, priority, changefreq FROM custom_urls")
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var u, freq string
			var pri float64
			cRows.Scan(&u, &pri, &freq)
			if !strings.HasPrefix(u, "http") {
				u = siteURL + strings.TrimPrefix(u, "/")
			}
			urls = append(urls, Url{Loc: u, Priority: pri, ChangeFreq: freq, LastMod: time.Now().Format("2006-01-02")})
		}
	}

	urlSet := UrlSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		Urls:  urls,
	}

	out, err := xml.MarshalIndent(urlSet, "", "  ")
	if err != nil {
		return ""
	}

	res := xml.Header + string(out)
	p.setSetting("last_generated", time.Now().Format("2006-01-02 15:04:05"))
	p.setSetting("last_url_count", fmt.Sprintf("%d", len(urls)))
	return res
}

func parseDate(dt string) string {
	t, err := time.Parse(time.RFC3339, dt)
	if err == nil {
		return t.Format("2006-01-02")
	}
	t, err = time.Parse("2006-01-02 15:04:05", dt)
	if err == nil {
		return t.Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}

func (p *SitemapGeneratorPlugin) submitToEngines(targetEngine string) {
	siteURL := p.getSetting("site_url")
	key := p.getSetting("indexnow_key")
	if siteURL == "" {
		return
	}

	// Get URL list
	var urls []string
	cmsDB, err := sql.Open("sqlite", "file:cms.db?mode=ro")
	if err == nil {
		defer cmsDB.Close()
		rows, _ := cmsDB.Query("SELECT slug FROM pages WHERE status='published'")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var slug string
				rows.Scan(&slug)
				loc := siteURL
				if slug != "home" && slug != "index" && slug != "/" {
					if !strings.HasPrefix(slug, "/") { loc += "/" }
					loc += slug
				}
				urls = append(urls, loc)
			}
		}
	}
	cRows, err := p.db.Query("SELECT url FROM custom_urls")
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var u string
			cRows.Scan(&u)
			if !strings.HasPrefix(u, "http") { u = siteURL + strings.TrimPrefix(u, "/") }
			urls = append(urls, u)
		}
	}

	urlCount := len(urls)
	if urlCount == 0 { return }
	if urlCount > 10000 { urls = urls[:10000] } // IndexNow limit

	host := strings.TrimPrefix(siteURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.Split(host, "/")[0]

	indexNowReq := map[string]interface{}{
		"host": host,
		"key": key,
		"keyLocation": siteURL + "/" + key + ".txt",
		"urlList": urls,
	}
	indexNowBody, _ := json.Marshal(indexNowReq)

	engines := []struct {
		ID   string
		URL  string
		Type string
	}{
		{"bing", "https://api.indexnow.org/indexnow", "indexnow"},
		{"yandex", "https://yandex.com/indexnow", "indexnow"},
		{"naver", "https://searchadvisor.naver.com/indexnow", "indexnow"},
		{"seznam", "https://search.seznam.cz/indexnow", "indexnow"},
		{"baidu", "http://ping.baidu.com/ping/RPC2", "ping"},
		{"sogou", "http://www.sogou.com/labs/inspire/api.php", "ping"},
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for _, eng := range engines {
		if targetEngine != "" && eng.ID != targetEngine {
			continue
		}

		var req *http.Request
		var err error

		if eng.Type == "indexnow" {
			req, err = http.NewRequest("POST", eng.URL, bytes.NewBuffer(indexNowBody))
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		} else if eng.Type == "ping" {
			sitemapURL := siteURL + "/api/plugin/sitemap-generator/sitemap.xml"
			if eng.ID == "baidu" {
				xmlBody := fmt.Sprintf(`<?xml version="1.0"?><methodCall><methodName>weblogUpdates.extendedPing</methodName><params><param><value><string>%s</string></value></param><param><value><string>%s</string></value></param><param><value><string>%s</string></value></param><param><value><string></string></value></param></params></methodCall>`, siteURL, siteURL, sitemapURL)
				req, err = http.NewRequest("POST", eng.URL, strings.NewReader(xmlBody))
				req.Header.Set("Content-Type", "text/xml")
			} else {
				req, err = http.NewRequest("GET", eng.URL+"?url="+url.QueryEscape(sitemapURL), nil)
			}
		}

		if err != nil {
			continue
		}

		resp, err := client.Do(req)
		code := 0
		status := "error"
		if err == nil {
			code = resp.StatusCode
			if code >= 200 && code < 300 {
				status = "success"
			}
			resp.Body.Close()
		}

		p.db.Exec("INSERT INTO submission_log (engine, status, response_code, url_count) VALUES (?, ?, ?, ?)",
			eng.ID, status, code, urlCount)
	}
}

func (p *SitemapGeneratorPlugin) renderDashboard() string {
	siteURL := p.getSetting("site_url")
	indexNowKey := p.getSetting("indexnow_key")
	autoSchedule := p.getSetting("auto_schedule")
	defChangeFreq := p.getSetting("default_changefreq")
	if defChangeFreq == "" { defChangeFreq = "weekly" }
	defPri := p.getSetting("default_priority")
	if defPri == "" { defPri = "0.5" }
	excludePat := p.getSetting("exclude_patterns")
	lastGen := p.getSetting("last_generated")
	urlCount := p.getSetting("last_url_count")
	if lastGen == "" { lastGen = "Never" }
	if urlCount == "" { urlCount = "0" }

	// Custom URLs table
	var customUrlsRows string
	cRows, _ := p.db.Query("SELECT id, url, priority, changefreq, label FROM custom_urls")
	if cRows != nil {
		defer cRows.Close()
		for cRows.Next() {
			var id int
			var u, f, l string
			var pri float64
			cRows.Scan(&id, &u, &pri, &f, &l)
			customUrlsRows += fmt.Sprintf(`<tr>
				<td>%s <small class="text-muted d-block">%s</small></td>
				<td>%.2f</td>
				<td>%s</td>
				<td>
					<form method="POST" action="/admin/plugin/sitemap-generator" class="m-0">
						<input type="hidden" name="action" value="delete_url">
						<input type="hidden" name="id" value="%d">
						<button class="btn btn-sm btn-ghost-danger">Remove</button>
					</form>
				</td>
			</tr>`, u, l, pri, f, id)
		}
	}
	if customUrlsRows == "" {
		customUrlsRows = `<tr><td colspan="4" class="text-center text-muted">No custom URLs added</td></tr>`
	}

	// Submission Log Table
	var logRows string
	lRows, _ := p.db.Query("SELECT engine, status, response_code, submitted_at, url_count FROM submission_log ORDER BY submitted_at DESC LIMIT 15")
	if lRows != nil {
		defer lRows.Close()
		for lRows.Next() {
			var eng, stat, dt string
			var code, count int
			lRows.Scan(&eng, &stat, &code, &dt, &count)
			badge := `<span class="badge bg-green">Success</span>`
			if stat != "success" { badge = `<span class="badge bg-red">Error</span>` }
			logRows += fmt.Sprintf(`<tr>
				<td class="text-capitalize">%s</td>
				<td>%s</td>
				<td>%d</td>
				<td>%d URLs</td>
				<td>%s</td>
			</tr>`, eng, badge, code, count, dt)
		}
	}

	// Engine Action Table
	engines := []struct{ ID, Name, Type, Info string }{
		{"bing", "Bing", "IndexNow", "Active real-time submission"},
		{"yandex", "Yandex", "IndexNow", "Active real-time submission"},
		{"naver", "Naver", "IndexNow", "Active real-time submission"},
		{"seznam", "Seznam", "IndexNow", "Active real-time submission"},
		{"baidu", "Baidu", "Ping", "XMLRPC Ping"},
		{"sogou", "Sogou", "Ping", "Direct Ping"},
		{"google", "Google", "Manual", `<a href="https://search.google.com/search-console" target="_blank">Search Console</a>`},
		{"duckduckgo", "DuckDuckGo", "Auto", "Uses Bing Index"},
		{"ecosia", "Ecosia", "Auto", "Uses Bing Index"},
		{"mojeek", "Mojeek", "Manual", `<a href="https://www.mojeek.com/support/submit.html" target="_blank">Submit Link</a>`},
		{"brave", "Brave Search", "Manual", `<a href="https://search.brave.com/help/webmaster" target="_blank">Web Discovery</a>`},
	}
	var engineRows string
	for _, e := range engines {
		actionBtn := ""
		if e.Type == "IndexNow" || e.Type == "Ping" {
			actionBtn = fmt.Sprintf(`
			<form method="POST" action="/admin/plugin/sitemap-generator" enctype="multipart/form-data" class="m-0">
				<input type="hidden" name="action" value="submit_one">
				<input type="hidden" name="engine" value="%s">
				<button class="btn btn-sm btn-outline-primary">Submit Now</button>
			</form>`, e.ID)
		}
		
		var lastStatus string
		p.db.QueryRow("SELECT status || ' (' || date(submitted_at) || ')' FROM submission_log WHERE engine=? ORDER BY submitted_at DESC LIMIT 1", e.ID).Scan(&lastStatus)
		if lastStatus == "" { lastStatus = "Never" }

		engineRows += fmt.Sprintf(`<tr>
			<td><strong>%s</strong></td>
			<td><span class="text-muted">%s</span></td>
			<td>%s</td>
			<td>%s</td>
			<td class="text-end">%s</td>
		</tr>`, e.Name, e.Type, e.Info, lastStatus, actionBtn)
	}

	return fmt.Sprintf(`
	<div class="row row-cards mb-3">
		<div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Sitemap URLs</div><div class="h1 mb-0 mt-2">%s</div></div></div></div>
		<div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Last Generated</div><div class="h1 mb-0 mt-2 fs-3">%s</div></div></div></div>
		<div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Sitemap URL</div><div class="mt-2"><a href="/admin/plugin/sitemap-generator/view" class="btn btn-sm btn-ghost-primary">View sitemap.xml</a></div></div></div></div>
	</div>

	<div class="row row-cards">
		<div class="col-lg-8">
			<div class="card">
				<div class="card-header d-flex justify-content-between align-items-center">
					<h3 class="card-title">Search Engines</h3>
					<form method="POST" action="/admin/plugin/sitemap-generator" enctype="multipart/form-data" class="m-0">
						<input type="hidden" name="action" value="submit_all">
						<button class="btn btn-primary">Submit to All Supported</button>
					</form>
				</div>
				<div class="table-responsive">
					<table class="table card-table table-vcenter">
						<thead><tr><th>Engine</th><th>Type</th><th>Info</th><th>Last Submit</th><th></th></tr></thead>
						<tbody>%s</tbody>
					</table>
				</div>
			</div>

			<div class="card mt-3">
				<div class="card-header"><h3 class="card-title">Submission History</h3></div>
				<div class="table-responsive">
					<table class="table card-table table-vcenter table-sm">
						<thead><tr><th>Engine</th><th>Status</th><th>Code</th><th>URLs</th><th>Time</th></tr></thead>
						<tbody>%s</tbody>
					</table>
				</div>
			</div>
		</div>

		<div class="col-lg-4">
			<div class="card mb-3">
				<div class="card-header"><h3 class="card-title">Settings</h3></div>
				<div class="card-body">
					<form method="POST" action="/admin/plugin/sitemap-generator" enctype="multipart/form-data">
						<input type="hidden" name="action" value="settings">
						<div class="mb-3">
							<label class="form-label">Site Base URL</label>
							<input type="url" class="form-control" name="site_url" value="%s" placeholder="https://example.com" required>
						</div>
						<div class="mb-3">
							<label class="form-label">IndexNow API Key <small class="text-muted">(Auto-injected via meta tag)</small></label>
							<input type="text" class="form-control bg-light" value="%s" readonly>
						</div>
						<div class="mb-3">
							<label class="form-label">Auto Schedule</label>
							<select class="form-select" name="auto_schedule">
								<option value="" %s>Off</option>
								<option value="daily" %s>Daily</option>
								<option value="weekly" %s>Weekly</option>
							</select>
						</div>
						<div class="mb-3">
							<label class="form-label">Default Change Frequency</label>
							<select class="form-select" name="default_changefreq">
								<option value="always" %s>Always</option>
								<option value="hourly" %s>Hourly</option>
								<option value="daily" %s>Daily</option>
								<option value="weekly" %s>Weekly</option>
								<option value="monthly" %s>Monthly</option>
								<option value="yearly" %s>Yearly</option>
							</select>
						</div>
						<div class="mb-3">
							<label class="form-label">Default Priority (0.1 - 1.0)</label>
							<input type="number" step="0.1" min="0.1" max="1.0" class="form-control" name="default_priority" value="%s">
						</div>
						<div class="mb-3">
							<label class="form-label">Exclude Patterns (Regex, 1 per line)</label>
							<textarea class="form-control" name="exclude_patterns" rows="3" placeholder="^/private">%s</textarea>
						</div>
						<button class="btn btn-primary w-100">Save Settings</button>
					</form>
				</div>
			</div>

			<div class="card">
				<div class="card-header"><h3 class="card-title">Custom URLs</h3></div>
				<div class="card-body border-bottom">
					<form method="POST" action="/admin/plugin/sitemap-generator" enctype="multipart/form-data">
						<input type="hidden" name="action" value="add_url">
						<div class="row g-2 mb-2">
							<div class="col-8"><input type="text" class="form-control form-control-sm" name="url" placeholder="/custom-page" required></div>
							<div class="col-4"><input type="number" step="0.1" class="form-control form-control-sm" name="priority" value="0.8"></div>
						</div>
						<div class="row g-2 mb-2">
							<div class="col-6"><input type="text" class="form-control form-control-sm" name="label" placeholder="Label (optional)"></div>
							<div class="col-6">
								<select class="form-select form-select-sm" name="changefreq">
									<option value="daily">Daily</option>
									<option value="weekly" selected>Weekly</option>
									<option value="monthly">Monthly</option>
								</select>
							</div>
						</div>
						<button class="btn btn-sm btn-secondary w-100">Add URL</button>
					</form>
				</div>
				<div class="table-responsive">
					<table class="table card-table table-sm">
						<tbody>%s</tbody>
					</table>
				</div>
			</div>
		</div>
	</div>`,
		urlCount, lastGen, engineRows, logRows,
		siteURL, indexNowKey,
		map[bool]string{true:"selected"}[autoSchedule==""],
		map[bool]string{true:"selected"}[autoSchedule=="daily"],
		map[bool]string{true:"selected"}[autoSchedule=="weekly"],
		map[bool]string{true:"selected"}[defChangeFreq=="always"],
		map[bool]string{true:"selected"}[defChangeFreq=="hourly"],
		map[bool]string{true:"selected"}[defChangeFreq=="daily"],
		map[bool]string{true:"selected"}[defChangeFreq=="weekly"],
		map[bool]string{true:"selected"}[defChangeFreq=="monthly"],
		map[bool]string{true:"selected"}[defChangeFreq=="yearly"],
		defPri, excludePat, customUrlsRows,
	)
}

func (p *SitemapGeneratorPlugin) HookAdminTopRightWidget() string { return "" }
func (p *SitemapGeneratorPlugin) HookUserProfileTab(userID int) string { return "" }
func (p *SitemapGeneratorPlugin) HookUserAccountCard(userID int) string { return "" }
func (p *SitemapGeneratorPlugin) HookUserRegistered(userID int) string { return "" }

func (p *SitemapGeneratorPlugin) startScheduler() {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			sched := p.getSetting("auto_schedule")
			if sched == "daily" || sched == "weekly" {
				var lastSub string
				p.db.QueryRow("SELECT submitted_at FROM submission_log ORDER BY submitted_at DESC LIMIT 1").Scan(&lastSub)
				
				shouldRun := false
				if lastSub == "" {
					shouldRun = true
				} else if t, err := time.Parse("2006-01-02 15:04:05", lastSub); err == nil {
					if sched == "daily" && time.Since(t) > 24*time.Hour {
						shouldRun = true
					} else if sched == "weekly" && time.Since(t) > 7*24*time.Hour {
						shouldRun = true
					}
				}

				if shouldRun {
					p.generateSitemapXML()
					p.submitToEngines("")
				}
			}
		}
	}()
}

func main() {
	p := &SitemapGeneratorPlugin{}
	p.initDB()
	p.startScheduler()

	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig,
		Plugins: map[string]hplugin.Plugin{
			"cms_plugin": &plugin.CMSPluginDef{Impl: p},
		},
	})
}
