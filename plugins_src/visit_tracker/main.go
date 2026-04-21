package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/ez8/gocms/pkg/plugin"
	hplugin "github.com/hashicorp/go-plugin"
	_ "modernc.org/sqlite"
)

type VisitTrackerPlugin struct {
	db *sql.DB
}

func (p *VisitTrackerPlugin) PluginName() string {
	return "Visit Tracker v1.0"
}

func (p *VisitTrackerPlugin) HookBeforeFrontPageRender(content string) string {
	script := `
<script>
	(function() {
		try {
			var area = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Unknown';
			fetch('/api/plugin/visit_tracker/track?area=' + encodeURIComponent(area), {
				method: 'GET',
				cache: 'no-cache'
			});
		} catch (e) {}
	})();
</script>
`
	if strings.Contains(content, "</body>") {
		return strings.Replace(content, "</body>", script+"</body>", 1)
	}
	return content + script
}

func (p *VisitTrackerPlugin) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{
		{
			Label: "Visit Overview",
			URL:   "/admin/plugin/visit_tracker",
			Icon:  "eye",
		},
	}
}

func (p *VisitTrackerPlugin) HookAdminRoute(route string) string {
	// Expose a public tracking endpoint
	if strings.HasPrefix(route, "/api/plugin/visit_tracker/track") {
		u, err := url.Parse(route)
		if err == nil {
			q := u.Query()
			area := q.Get("area")
			ip := q.Get("_client_ip")
			ua := q.Get("_user_agent")
			now := time.Now().Format(time.RFC3339)

			_, err = p.db.Exec(`INSERT INTO visits (time, area, browser, ip) VALUES (?, ?, ?, ?)`, now, area, ua, ip)
			if err != nil {
				log.Println("Visit Tracker plugin DB error:", err)
			}
		}
		// Return valid empty JSON structure for the fetch request
		return "{}"
	}

	// Internal admin interface
	if strings.Contains(route, "/admin/plugin/visit_tracker") {
		return p.renderDashboard()
	}

	return ""
}

func (p *VisitTrackerPlugin) renderDashboard() string {
	rows, err := p.db.Query(`SELECT time, area, browser, ip FROM visits ORDER BY time DESC LIMIT 100`)
	if err != nil {
		return "Error loading visits: " + err.Error()
	}
	defer rows.Close()

	var tableRows string
	for rows.Next() {
		var t, area, browser, ip string
		if err := rows.Scan(&t, &area, &browser, &ip); err == nil {
			parsedTime, parseErr := time.Parse(time.RFC3339, t)
			displayTime := t
			if parseErr == nil && !parsedTime.IsZero() {
				displayTime = parsedTime.Format("2006-01-02 15:04:05")
			}
			tableRows += fmt.Sprintf(`
			<tr>
				<td>%s</td>
				<td>%s</td>
				<td><code>%s</code></td>
				<td class="text-muted" style="max-width:300px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis;" title="%s">%s</td>
			</tr>`, ip, area, displayTime, browser, browser)
		}
	}

	if tableRows == "" {
		tableRows = `<tr><td colspan="4" class="text-center text-muted">No visits recorded yet.</td></tr>`
	}

	return `
	<div class="card">
		<div class="card-header border-0">
			<h3 class="card-title">Website Visit Overview</h3>
		</div>
		<div class="table-responsive">
			<table class="table card-table table-vcenter text-nowrap datatable">
				<thead>
					<tr>
						<th>IP Address</th>
						<th>Area (Timezone)</th>
						<th>Time</th>
						<th>Browser / User-Agent</th>
					</tr>
				</thead>
				<tbody>
					` + tableRows + `
				</tbody>
			</table>
		</div>
	</div>
	`
}

func (p *VisitTrackerPlugin) HookDashboardWidget() string     { return "" }
func (p *VisitTrackerPlugin) HookAdminTopRightWidget() string { return "" }

// User management hooks (v2 stubs)
func (p *VisitTrackerPlugin) HookUserProfileTab(userID int) string  { return "" }
func (p *VisitTrackerPlugin) HookUserAccountCard(userID int) string { return "" }
func (p *VisitTrackerPlugin) HookUserRegistered(userID int) string  { return "" }

func main() {
	db, err := sql.Open("sqlite", "visit_tracker.db")
	if err != nil {
		log.Fatal("failed to open visit_tracker database: ", err)
	}
	
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time TEXT,
			area TEXT,
			browser TEXT,
			ip TEXT
		)
	`)
	if err != nil {
		log.Fatal("Could not initialize visit_tracker DB:", err)
	}

	var pluginMap = map[string]hplugin.Plugin{
		"cms_plugin": &plugin.CMSPluginDef{Impl: &VisitTrackerPlugin{db: db}},
	}

	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig,
		Plugins:         pluginMap,
	})
}
