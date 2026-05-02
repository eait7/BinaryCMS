package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
	"github.com/ez8/gocms/pkg/plugin"
)

// handleAdminMenuArrange serves the drag-and-drop admin menu arrangement page.
func handleAdminMenuArrange(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the current menu layout (exactly what the navbar renders)
		currentMenus := getAdminMenus(pm)

		// Marshal to JSON for the frontend SortableJS tree
		menusJSON, _ := json.Marshal(currentMenus)

		content, err := renderTemplate("admin_menu_arrange.html", map[string]interface{}{
			"Menus":     currentMenus,
			"MenusJSON": template.JS(menusJSON),
		})
		if err != nil {
			log.Printf("Menu arrange template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Arrange Admin Menus", content, "", pm)
	}
}

// handleSaveAdminMenuArrangement saves the drag-and-drop arrangement to backend_menus.json.
func handleSaveAdminMenuArrangement(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var layout []plugin.MenuItem
		if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON: " + err.Error()})
			return
		}

		// Validate — don't allow empty saves
		if len(layout) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Cannot save empty menu layout"})
			return
		}

		// Save to backend_menus.json
		b, err := json.MarshalIndent(layout, "", "  ")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to marshal layout"})
			return
		}

		menuPath := "backend_menus.json"
		if _, err := os.Stat("/app/data"); err == nil {
			menuPath = "/app/data/backend_menus.json"
		} else if _, err := os.Stat("data"); err == nil {
			menuPath = "data/backend_menus.json"
		}

		if err := os.WriteFile(menuPath, b, 0644); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to write file: " + err.Error()})
			return
		}

		// Invalidate template cache so the new layout is visible immediately
		theme.InvalidateCache()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Menu layout saved successfully"})
	}
}

// handleResetAdminMenuArrangement removes the custom layout, reverting to defaults.
func handleResetAdminMenuArrangement(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		menuPath := "backend_menus.json"
		if _, err := os.Stat("/app/data"); err == nil {
			menuPath = "/app/data/backend_menus.json"
		} else if _, err := os.Stat("data"); err == nil {
			menuPath = "data/backend_menus.json"
		}
		os.Remove(menuPath)
		theme.InvalidateCache()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Menu layout reset to defaults"})
	}
}
