package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/ez8/gocms/pkg/plugin"
	hplugin "github.com/hashicorp/go-plugin"
	_ "modernc.org/sqlite"
)

type AnalyticsPlugin struct {
	db *sql.DB
}

func (p *AnalyticsPlugin) PluginName() string {
	return "Advanced Analytics"
}

// Invisible Frontend JS injector
func (p *AnalyticsPlugin) HookBeforeFrontPageRender(content string) string {
	script := `
<script async>
	(function() {
		try {
			var area = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Unknown';
			var pageUrl = window.location.pathname;
			fetch('/api/plugin/advanced_analytics/track?area=' + encodeURIComponent(area) + '&page=' + encodeURIComponent(pageUrl), {
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

func (p *AnalyticsPlugin) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{
		{
			Label: "Live Analytics",
			URL:   "/admin/plugin/advanced_analytics",
			Icon:  "chart-dots", // Nifty Tabler Icon
		},
	}
}

// ----- 1. Parse User Agent Logic -----
func parseUserAgent(ua string) (os string, browser string) {
	uaLower := strings.ToLower(ua)
	
	// Determine OS
	if strings.Contains(uaLower, "windows") {
		os = "Windows"
	} else if strings.Contains(uaLower, "mac os") && !strings.Contains(uaLower, "iphone") && !strings.Contains(uaLower, "ipad") {
		os = "macOS"
	} else if strings.Contains(uaLower, "iphone") || strings.Contains(uaLower, "ipad") {
		os = "iOS"
	} else if strings.Contains(uaLower, "android") {
		os = "Android"
	} else if strings.Contains(uaLower, "linux") {
		os = "Linux"
	} else {
		os = "Unknown"
	}

	// Determine Browser
	if strings.Contains(uaLower, "edge") || strings.Contains(uaLower, "edg/") {
		browser = "Edge"
	} else if strings.Contains(uaLower, "chrome") || strings.Contains(uaLower, "crios") {
		browser = "Chrome"
	} else if strings.Contains(uaLower, "firefox") || strings.Contains(uaLower, "fxios") {
		browser = "Firefox"
	} else if strings.Contains(uaLower, "safari") && !strings.Contains(uaLower, "chrome") {
		browser = "Safari"
	} else if strings.Contains(uaLower, "opera") || strings.Contains(uaLower, "opr/") {
		browser = "Opera"
	} else {
		browser = "Other"
	}

	return os, browser
}


func (p *AnalyticsPlugin) HookAdminRoute(route string) string {
	// EXPOSE TRACKING ENDPOINT
	if strings.HasPrefix(route, "/api/plugin/advanced_analytics/track") {
		u, err := url.Parse(route)
		if err == nil {
			q := u.Query()
			area := q.Get("area")
			ip := q.Get("_client_ip")
			ua := q.Get("_user_agent")
			pageUrl := q.Get("page")
			if pageUrl == "" { pageUrl = "/" }
			now := time.Now().UTC().Format(time.RFC3339)

			osName, browserName := parseUserAgent(ua)
			if area == "" { area = "Unknown" }

			_, err = p.db.Exec(`INSERT INTO advanced_visits (time, area, os, browser, ip, user_agent, page_url) VALUES (?, ?, ?, ?, ?, ?, ?)`, 
				now, area, osName, browserName, ip, ua, pageUrl)
			if err != nil {
				log.Println("Advanced Analytics DB Error:", err)
			}
		}
		return "{}" 
	}

	// INTERNAL DASHBOARD INTERFACE
	if strings.Contains(route, "/admin/plugin/advanced_analytics") {
		u, _ := url.Parse(route)
		timeRange := u.Query().Get("range")
		if timeRange == "" {
			timeRange = "week"
		}
		return p.renderDashboard(timeRange)
	}

	return ""
}

// Hardcoded Open-Source Timezone Geo mapping
// Because loading a 40MB GeoLite2 DB into a tiny plugin is bloated.
var TzGeoMap = map[string][2]float64{
	"Africa/Johannesburg": {-26.2041, 28.0473},
	"Africa/Cairo": {30.0444, 31.2357},
	"Africa/Lagos": {6.5244, 3.3792},
	"Europe/London": {51.5074, -0.1278},
	"Europe/Paris": {48.8566, 2.3522},
	"Europe/Berlin": {52.5200, 13.4050},
	"Europe/Kyiv": {50.4501, 30.5234},
	"Europe/Moscow": {55.7558, 37.6173},
	"Asia/Tokyo": {35.6762, 139.6503},
	"Asia/Shanghai": {31.2304, 121.4737},
	"Asia/Kolkata": {22.5726, 88.3639},
	"Asia/Dubai": {25.2048, 55.2708},
	"Asia/Singapore": {1.3521, 103.8198},
	"America/New_York": {40.7128, -74.0060},
	"America/Los_Angeles": {34.0522, -118.2437},
	"America/Chicago": {41.8781, -87.6298},
	"America/Toronto": {43.6510, -79.3470},
	"America/Sao_Paulo": {-23.5505, -46.6333},
	"Australia/Sydney": {-33.8688, 151.2093},
	"Australia/Melbourne": {-37.8136, 144.9631},
	"Pacific/Auckland": {-36.8485, 174.7633},
}

// Get approximate bounds for timezone
func getCoordsForArea(area string) (float64, float64) {
	if coords, ok := TzGeoMap[area]; ok {
		return coords[0], coords[1]
	}
	// Default to rough continents if precise city missing
	geo := strings.ToLower(area)
	if strings.Contains(geo, "africa") { return 0.0, 20.0 }
	if strings.Contains(geo, "europe") { return 48.0, 14.0 }
	if strings.Contains(geo, "america") && strings.Contains(geo, "north") { return 40.0, -100.0 }
	if strings.Contains(geo, "america") { return 0.0, -60.0 }
	if strings.Contains(geo, "asia") { return 30.0, 100.0 }
	return 0, 0 // Center of map
}

func (p *AnalyticsPlugin) renderDashboard(dtRange string) string {
	
	// Date Filter Calculation
	var timeConstraint string
	switch dtRange {
	case "day":
		timeConstraint = "datetime('now', '-1 day')"
	case "month":
		timeConstraint = "datetime('now', '-30 days')"
	case "year":
		timeConstraint = "datetime('now', '-365 days')"
	case "week": fallthrough
	default:	
		timeConstraint = "datetime('now', '-7 days')"
	}

	// 1. Total Visits in Range
	var totalVisits int
	p.db.QueryRow(fmt.Sprintf("SELECT COUNT(id) FROM advanced_visits WHERE time >= %s", timeConstraint)).Scan(&totalVisits)

	// 2. Unique IPs in Range
	var uniqueIPs int
	p.db.QueryRow(fmt.Sprintf("SELECT COUNT(DISTINCT ip) FROM advanced_visits WHERE time >= %s", timeConstraint)).Scan(&uniqueIPs)

	// 3. Geographic Data for Map & Top Area Scorecard
	rows, _ := p.db.Query(fmt.Sprintf("SELECT area, COUNT(id) as c FROM advanced_visits WHERE time >= %s GROUP BY area ORDER BY c DESC", timeConstraint))
	var topArea = "None"
	type geoPoint struct { Lat float64 `json:"lat"`; Lng float64 `json:"lng"`; Name string `json:"name"`; Count int `json:"count"` }
	var mapPoints []geoPoint
	
	first := true
	for rows.Next() {
		var areaName string; var count int
		rows.Scan(&areaName, &count)
		if first { topArea = areaName; first = false }
		lat, lng := getCoordsForArea(areaName)
		mapPoints = append(mapPoints, geoPoint{Lat: lat, Lng: lng, Name: areaName, Count: count})
	}
	rows.Close()
	mapJson, _ := json.Marshal(mapPoints)

	// 4. OS Distribution for Donut Chart
	rowsOS, _ := p.db.Query(fmt.Sprintf("SELECT os, COUNT(id) as c FROM advanced_visits WHERE time >= %s GROUP BY os ORDER BY c DESC", timeConstraint))
	var osLabels, osSeries []string
	for rowsOS.Next() {
		var n string; var c int
		rowsOS.Scan(&n, &c)
		osLabels = append(osLabels, fmt.Sprintf(`"%s"`, n))
		osSeries = append(osSeries, fmt.Sprintf("%d", c))
	}
	rowsOS.Close()

	// 5. Daily Trend for Line Chart
	rowsTrend, _ := p.db.Query(fmt.Sprintf("SELECT date(time) as d, COUNT(id) as c FROM advanced_visits WHERE time >= %s GROUP BY d ORDER BY d ASC", timeConstraint))
	var trendLabels, trendSeries []string
	for rowsTrend.Next() {
		var d string; var c int
		rowsTrend.Scan(&d, &c)
		trendLabels = append(trendLabels, fmt.Sprintf(`"%s"`, d))
		trendSeries = append(trendSeries, fmt.Sprintf("%d", c))
	}
	rowsTrend.Close()

	// 6. Recent Detailed Log Table
	rowsLog, _ := p.db.Query(fmt.Sprintf("SELECT time, area, os, browser, ip, page_url FROM advanced_visits WHERE time >= %s ORDER BY time DESC LIMIT 200", timeConstraint))
	
	type visitGroup struct {
		LatestTime  string
		Area        string
		OSVal       string
		Browser     string
		IP          string
		Pages       []string
	}
	var groups []*visitGroup
	groupMap := make(map[string]*visitGroup)

	for rowsLog.Next() {
		var tm, area, osVal, browser, ip string
		var pageUrl sql.NullString
		rowsLog.Scan(&tm, &area, &osVal, &browser, &ip, &pageUrl)
		
		pUrl := pageUrl.String
		if pUrl == "" { pUrl = "/" }

		if g, exists := groupMap[ip]; exists {
			g.Pages = append(g.Pages, pUrl)
		} else {
			g := &visitGroup{
				LatestTime: tm,
				Area:       area,
				OSVal:      osVal,
				Browser:    browser,
				IP:         ip,
				Pages:      []string{pUrl},
			}
			groups = append(groups, g)
			groupMap[ip] = g
		}
	}
	rowsLog.Close()

	var tRows string
	for _, g := range groups {
		pdTimestamp, _ := time.Parse(time.RFC3339, g.LatestTime)
		
		pageHtml := ""
		if len(g.Pages) == 1 {
			pageHtml = fmt.Sprintf(`<a href="%s" target="_blank" class="text-reset">%s</a>`, g.Pages[0], g.Pages[0])
		} else {
			listItems := ""
			for _, p := range g.Pages {
				listItems += fmt.Sprintf(`<li><a class="dropdown-item" href="%s" target="_blank">%s</a></li>`, p, p)
			}
			pageHtml = fmt.Sprintf(`
			<div class="dropdown">
				<button class="btn btn-sm btn-outline-secondary dropdown-toggle" type="button" data-bs-toggle="dropdown" aria-expanded="false">%d Pages Viewed</button>
				<ul class="dropdown-menu">
					%s
				</ul>
			</div>`, len(g.Pages), listItems)
		}

		tRows += fmt.Sprintf(`
		<tr>
			<td class="text-muted">%s</td>
			<td><strong>%s</strong></td>
			<td>%s</td>
			<td><span class="badge bg-blue-lt">%s</span></td>
			<td><span class="badge bg-purple-lt">%s</span></td>
			<td><a href="https://dnschecker.org/ip-location.php?ip=%s" target="_blank" class="text-reset">%s</a></td>
		</tr>`, pdTimestamp.Format("Jan 02, 15:04"), g.Area, pageHtml, g.OSVal, g.Browser, g.IP, g.IP)
	}

	if tRows == "" { tRows = `<tr><td colspan="6" class="text-center text-muted">No visits matching this timeframe.</td></tr>` }

	// Ensure empty charts don't break JS
	if len(trendSeries) == 0 { trendLabels = []string{`"No Data"`}; trendSeries = []string{"0"} }
	if len(osSeries) == 0 { osLabels = []string{`"No Data"`}; osSeries = []string{"1"} }

	// ----- HTML DASHBOARD GENERATION -----
	return fmt.Sprintf(`
	<!-- Load Open Source Leaflet Map -->
	<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" integrity="sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=" crossorigin=""/>
	<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" integrity="sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=" crossorigin=""></script>
	<script src="https://cdn.jsdelivr.net/npm/apexcharts"></script>

	<!-- Page Header & Timeframe Filters -->
	<div class="page-header d-print-none mb-4">
		<div class="container-xl">
			<div class="row g-2 align-items-center">
				<div class="col">
					<h2 class="page-title">Advanced Traffic Analytics</h2>
					<div class="text-muted mt-1">Real-time visitor geographic and telemetry tracking.</div>
				</div>
				<div class="col-auto ms-auto d-print-none">
					<div class="btn-group">
						<a href="?range=day" class="btn %s">Past Day</a>
						<a href="?range=week" class="btn %s">Past Week</a>
						<a href="?range=month" class="btn %s">Past Month</a>
						<a href="?range=year" class="btn %s">Past Year</a>
					</div>
				</div>
			</div>
		</div>
	</div>

	<!-- 4 Scorecards -->
	<div class="row row-deck row-cards mb-4">
		<div class="col-sm-6 col-lg-3">
			<div class="card card-sm">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Total Page Views</div>
					</div>
					<div class="h1 mb-3">%d</div>
					<div class="d-flex mb-2"><div class="text-muted">In selected timeframe</div></div>
				</div>
			</div>
		</div>
		<div class="col-sm-6 col-lg-3">
			<div class="card card-sm">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Unique IP Addresses</div>
					</div>
					<div class="h1 mb-3 text-blue">%d</div>
					<div class="d-flex mb-2"><div class="text-muted">Distinct global visitors</div></div>
				</div>
			</div>
		</div>
		<div class="col-sm-6 col-lg-3">
			<div class="card card-sm">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Top Geographic Area</div>
					</div>
					<div class="h1 mb-3 text-green">%s</div>
					<div class="d-flex mb-2"><div class="text-muted">Primary visitor timezone</div></div>
				</div>
			</div>
		</div>
		<div class="col-sm-6 col-lg-3">
			<div class="card card-sm">
				<div class="card-body">
					<div class="d-flex align-items-center">
						<div class="subheader">Database Health</div>
					</div>
					<div class="h1 mb-3 text-purple">Connected</div>
					<div class="d-flex mb-2"><div class="text-muted">SQLite Fast RPC Link</div></div>
				</div>
			</div>
		</div>
	</div>

	<!-- Map & Graps -->
	<div class="row row-cards mb-4">
		<!-- Global Heatmap -->
		<div class="col-lg-12">
			<div class="card">
				<div class="card-header border-0 pb-1">
					<h3 class="card-title">Live Global Distribution</h3>
				</div>
				<div class="card-body" style="padding:0;">
					<div id="vmap" style="height: 400px; width: 100%%; border-bottom-left-radius: 4px; border-bottom-right-radius: 4px;"></div>
				</div>
			</div>
		</div>

		<!-- Trend Line -->
		<div class="col-lg-8">
			<div class="card">
				<div class="card-body">
					<h3 class="card-title">Visitor Volume Matrix</h3>
					<div id="chart-trend" style="min-height: 250px;"></div>
				</div>
			</div>
		</div>

		<!-- OS Donut -->
		<div class="col-lg-4">
			<div class="card">
				<div class="card-body">
					<h3 class="card-title">Operating System Share</h3>
					<div id="chart-os" style="min-height: 250px;"></div>
				</div>
			</div>
		</div>
	</div>

	<!-- Recent Raw Stream -->
	<div class="card">
		<div class="card-header border-0">
			<h3 class="card-title">Recent Connection Stream</h3>
		</div>
		<div class="table-responsive">
			<table class="table card-table table-vcenter table-striped text-nowrap mt-0">
				<thead>
					<tr>
						<th>Timestamp (UTC)</th>
						<th>Area / Timezone</th>
						<th>Page Viewed</th>
						<th>Device OS</th>
						<th>Browser Engine</th>
						<th>Remote IP Address</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>
	</div>

	<script>
		document.addEventListener("DOMContentLoaded", function() {
			// Initialize Charts!
			var trendChart = new ApexCharts(document.getElementById('chart-trend'), {
				chart: { type: "area", height: 250, fontFamily: 'inherit', toolbar: { show: false }},
				series: [{ name: "Visits", data: [%s] }],
				xaxis: { categories: [%s] },
				colors: ['#206bc4'],
				fill: { opacity: 0.16, type: 'solid' },
				stroke: { width: 2, curve: 'smooth' }
			});
			trendChart.render();

			var osChart = new ApexCharts(document.getElementById('chart-os'), {
				chart: { type: "donut", height: 250, fontFamily: 'inherit' },
				series: [%s],
				labels: [%s],
				colors: ['#206bc4', '#4299e1', '#66b3ff', '#99ccff', '#cce5ff']
			});
			osChart.render();

			// Initialize Leaflet Map
			var map = L.map('vmap', {scrollWheelZoom: false}).setView([20, 0], 2);
			L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
				attribution: '&copy; OpenStreetMap &copy; CARTO',
				subdomains: 'abcd',
				maxZoom: 19
			}).addTo(map);

			var geoData = %s;
			geoData.forEach(function(pt) {
				if(pt.lat !== 0 || pt.lng !== 0) {
					// Add a cool circle glowing marker
					var circle = L.circle([pt.lat, pt.lng], {
						color: '#206bc4',
						fillColor: '#206bc4',
						fillOpacity: 0.7,
						radius: 100000 * Math.log2(pt.count + 1) // Scaled
					}).addTo(map);
					circle.bindPopup("<b>" + pt.name + "</b><br>" + pt.count + " recorded visits.");
				}
			});
		});
	</script>
	`, 
	// Button active classes
	getBtnClass(dtRange, "day"), getBtnClass(dtRange, "week"), getBtnClass(dtRange, "month"), getBtnClass(dtRange, "year"),
	// Metrics
	totalVisits, uniqueIPs, topArea,
	// Table Rows
	tRows,
	// Trend Data
	strings.Join(trendSeries, ","), strings.Join(trendLabels, ","),
	// Donut Data
	strings.Join(osSeries, ","), strings.Join(osLabels, ","),
	// JSON Map Data
	string(mapJson),
	)
}

func getBtnClass(current, target string) string {
	if current == target { return "btn-primary" }
	return "btn-outline-primary"
}


func (p *AnalyticsPlugin) HookDashboardWidget() string     { return "" }
func (p *AnalyticsPlugin) HookAdminTopRightWidget() string { return "" }

// User management hooks (v2 stubs)
func (p *AnalyticsPlugin) HookUserProfileTab(userID int) string  { return "" }
func (p *AnalyticsPlugin) HookUserAccountCard(userID int) string { return "" }
func (p *AnalyticsPlugin) HookUserRegistered(userID int) string  { return "" }

func main() {
	db, err := sql.Open("sqlite", "advanced_analytics.db")
	if err != nil {
		log.Fatal("failed to open database: ", err)
	}
	
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS advanced_visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time TEXT,
			area TEXT,
			os TEXT,
			browser TEXT,
			ip TEXT,
			user_agent TEXT
		)
	`)
	if err != nil {
		log.Fatal("Could not initialize DB:", err)
	}
	// Auto-migrate to add page_url if it doesn't exist
	db.Exec(`ALTER TABLE advanced_visits ADD COLUMN page_url TEXT DEFAULT '/'`)

	var pluginMap = map[string]hplugin.Plugin{
		"cms_plugin": &plugin.CMSPluginDef{Impl: &AnalyticsPlugin{db: db}},
	}

	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig,
		Plugins:         pluginMap,
	})
}
