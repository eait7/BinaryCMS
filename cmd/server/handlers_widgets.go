package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/go-chi/chi/v5"
)

// handleWidgetManagement serves the widget management page.
func handleWidgetManagement(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		widgets, _ := models.GetAllWidgets()

		content, err := renderTemplate("widgets.html", map[string]interface{}{
			"Widgets": widgets,
		})
		if err != nil {
			log.Printf("Widgets template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		actions := template.HTML(`<a href="/admin/widgets/new" class="btn btn-primary d-none d-sm-inline-block">Add Widget</a>`)
		renderAdminPage(w, r, "Dashboard Widgets", content, actions, pm)
	}
}

// handleNewWidget creates a new dashboard widget.
func handleNewWidget(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			colSpan, _ := strconv.Atoi(r.FormValue("col_span"))
			if colSpan < 3 || colSpan > 12 {
				colSpan = 6
			}

			widget := models.DashboardWidget{
				Title:      r.FormValue("title"),
				WidgetType: r.FormValue("widget_type"),
				SourceURL:  r.FormValue("source_url"),
				ColSpan:    colSpan,
				Config:     r.FormValue("config"),
				Enabled:    r.FormValue("enabled") == "on",
			}

			if widget.Title == "" {
				widget.Title = "Untitled Widget"
			}
			if widget.WidgetType == "" {
				widget.WidgetType = "iframe"
			}

			models.CreateWidget(widget)
			http.Redirect(w, r, "/admin/widgets", http.StatusFound)
			return
		}

		content, err := renderTemplate("widget_edit.html", map[string]interface{}{
			"IsNew": true,
		})
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Add Widget", content, "", pm)
	}
}

// handleEditWidget edits an existing widget.
func handleEditWidget(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil {
			http.Redirect(w, r, "/admin/widgets", http.StatusFound)
			return
		}

		if r.Method == "POST" {
			colSpan, _ := strconv.Atoi(r.FormValue("col_span"))
			if colSpan < 3 || colSpan > 12 {
				colSpan = 6
			}

			widget := models.DashboardWidget{
				ID:         id,
				Title:      r.FormValue("title"),
				WidgetType: r.FormValue("widget_type"),
				SourceURL:  r.FormValue("source_url"),
				ColSpan:    colSpan,
				Config:     r.FormValue("config"),
				Enabled:    r.FormValue("enabled") == "on",
			}
			models.UpdateWidget(widget)
			http.Redirect(w, r, "/admin/widgets", http.StatusFound)
			return
		}

		widget, err := models.GetWidgetByID(id)
		if err != nil {
			http.Redirect(w, r, "/admin/widgets", http.StatusFound)
			return
		}

		content, err := renderTemplate("widget_edit.html", map[string]interface{}{
			"Widget": widget,
		})
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Edit Widget", content, "", pm)
	}
}

// handleDeleteWidget removes a widget.
func handleDeleteWidget(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		models.DeleteWidget(id)
		http.Redirect(w, r, "/admin/widgets", http.StatusFound)
	}
}

// handleReorderWidgets saves widget order via JSON.
func handleReorderWidgets(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var order []struct {
			ID    int `json:"id"`
			Order int `json:"order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&order); err == nil {
			for _, o := range order {
				models.UpdateWidgetOrder(o.ID, o.Order)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleDashboardLayoutAPI handles GET (load) and POST (save) of the dashboard block order.
func handleDashboardLayoutAPI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "POST" {
			var payload struct {
				Order []string `json:"order"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
				return
			}
			if len(payload.Order) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Empty layout"})
				return
			}

			b, _ := json.Marshal(payload.Order)
			models.SetSetting("dashboard_layout", string(b))

			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		// GET — return the saved order
		raw := models.GetSetting("dashboard_layout")
		if raw == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{"order": []string{}})
			return
		}

		var order []string
		if err := json.Unmarshal([]byte(raw), &order); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"order": []string{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"order": order})
	}
}

// =============================
// System Resources API (builtin)
// =============================

type SystemResources struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	CPUs        int     `json:"cpus"`
	GoVersion   string  `json:"go_version"`
	Goroutines  int     `json:"goroutines"`
	MemAlloc    string  `json:"mem_alloc"`
	MemSys      string  `json:"mem_sys"`
	MemGC       int     `json:"mem_gc_cycles"`
	DiskTotal   string  `json:"disk_total"`
	DiskUsed    string  `json:"disk_used"`
	DiskFree    string  `json:"disk_free"`
	DiskPercent float64 `json:"disk_percent"`
	Uptime      string  `json:"uptime"`
	LoadAvg     string  `json:"load_avg"`
}

var serverStartTime = time.Now()

func handleSystemResourcesAPI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		hostname, _ := os.Hostname()

		res := SystemResources{
			Hostname:   hostname,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			CPUs:       runtime.NumCPU(),
			GoVersion:  runtime.Version(),
			Goroutines: runtime.NumGoroutine(),
			MemAlloc:   formatBytes(m.Alloc),
			MemSys:     formatBytes(m.Sys),
			MemGC:      int(m.NumGC),
			Uptime:     formatDuration(time.Since(serverStartTime)),
		}

		// Disk usage for root filesystem
		total, used, free, percent := getDiskSpace()
		if total > 0 {
			res.DiskTotal = formatBytes(total)
			res.DiskUsed = formatBytes(used)
			res.DiskFree = formatBytes(free)
			res.DiskPercent = percent
		}

		// Load average from /proc/loadavg
		if loadData, err := os.ReadFile("/proc/loadavg"); err == nil {
			parts := strings.Fields(string(loadData))
			if len(parts) >= 3 {
				res.LoadAvg = fmt.Sprintf("%s, %s, %s", parts[0], parts[1], parts[2])
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	}
}

// handleSystemResourcesWidget serves an embeddable HTML widget for system resources.
func handleSystemResourcesWidget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(systemResourcesWidgetHTML))
	}
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

const systemResourcesWidgetHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: 'Inter', -apple-system, system-ui, sans-serif;
    background: transparent;
    color: #1e293b;
    font-size: 13px;
    padding: 16px;
  }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .stat-card {
    background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
    border: 1px solid #e2e8f0;
    border-radius: 10px;
    padding: 14px;
    transition: all 0.2s;
  }
  .stat-card:hover { border-color: #94a3b8; transform: translateY(-1px); box-shadow: 0 4px 12px rgba(0,0,0,0.06); }
  .stat-card.wide { grid-column: 1 / -1; }
  .stat-label { font-size: 10px; text-transform: uppercase; letter-spacing: 0.8px; color: #64748b; font-weight: 700; margin-bottom: 4px; }
  .stat-value { font-size: 20px; font-weight: 800; color: #0f172a; line-height: 1.2; }
  .stat-value.small { font-size: 14px; font-weight: 600; }
  .stat-sub { font-size: 11px; color: #94a3b8; margin-top: 2px; }
  .progress-bar {
    height: 6px;
    background: #e2e8f0;
    border-radius: 3px;
    margin-top: 8px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.6s ease;
  }
  .fill-green { background: linear-gradient(90deg, #22c55e, #16a34a); }
  .fill-yellow { background: linear-gradient(90deg, #eab308, #f59e0b); }
  .fill-red { background: linear-gradient(90deg, #ef4444, #dc2626); }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
  .header h3 { font-size: 14px; font-weight: 700; }
  .badge { font-size: 10px; padding: 3px 8px; border-radius: 6px; font-weight: 700; }
  .badge-green { background: #dcfce7; color: #166534; }
  .refresh-time { font-size: 10px; color: #94a3b8; text-align: center; margin-top: 12px; }
</style>
</head>
<body>
  <div class="header">
    <h3>⚡ System Resources</h3>
    <span class="badge badge-green" id="status">Online</span>
  </div>
  <div class="grid" id="stats">
    <div class="stat-card">
      <div class="stat-label">CPU Cores</div>
      <div class="stat-value" id="cpus">—</div>
      <div class="stat-sub" id="arch">—</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Memory Used</div>
      <div class="stat-value" id="mem">—</div>
      <div class="stat-sub" id="mem-sys">—</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Goroutines</div>
      <div class="stat-value" id="goroutines">—</div>
      <div class="stat-sub" id="gc">—</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Uptime</div>
      <div class="stat-value small" id="uptime">—</div>
      <div class="stat-sub" id="hostname">—</div>
    </div>
    <div class="stat-card wide">
      <div class="stat-label">Disk Usage</div>
      <div style="display:flex;justify-content:space-between;align-items:baseline;">
        <div class="stat-value small" id="disk-used">—</div>
        <div class="stat-sub" id="disk-total">— total</div>
      </div>
      <div class="progress-bar">
        <div class="progress-fill fill-green" id="disk-bar" style="width:0%"></div>
      </div>
    </div>
    <div class="stat-card wide">
      <div class="stat-label">Load Average (1m, 5m, 15m)</div>
      <div class="stat-value small" id="load">—</div>
    </div>
  </div>
  <div class="refresh-time" id="refresh">Last updated: —</div>

  <script>
    async function refresh() {
      try {
        const r = await fetch('/admin/api/system-resources');
        const d = await r.json();
        document.getElementById('cpus').textContent = d.cpus;
        document.getElementById('arch').textContent = d.os + '/' + d.arch;
        document.getElementById('mem').textContent = d.mem_alloc;
        document.getElementById('mem-sys').textContent = 'System: ' + d.mem_sys;
        document.getElementById('goroutines').textContent = d.goroutines;
        document.getElementById('gc').textContent = d.mem_gc_cycles + ' GC cycles';
        document.getElementById('uptime').textContent = d.uptime;
        document.getElementById('hostname').textContent = d.hostname;
        document.getElementById('disk-used').textContent = d.disk_used + ' used';
        document.getElementById('disk-total').textContent = d.disk_free + ' free / ' + d.disk_total;
        document.getElementById('load').textContent = d.load_avg || 'N/A';

        const bar = document.getElementById('disk-bar');
        bar.style.width = d.disk_percent.toFixed(1) + '%';
        bar.className = 'progress-fill ' + (d.disk_percent > 90 ? 'fill-red' : d.disk_percent > 70 ? 'fill-yellow' : 'fill-green');

        document.getElementById('refresh').textContent = 'Last updated: ' + new Date().toLocaleTimeString();
        document.getElementById('status').textContent = 'Online';
        document.getElementById('status').className = 'badge badge-green';
      } catch(e) {
        document.getElementById('status').textContent = 'Error';
        document.getElementById('status').className = 'badge badge-red';
      }
    }
    refresh();
    setInterval(refresh, 5000);
  </script>
</body>
</html>`
