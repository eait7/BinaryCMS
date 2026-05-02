package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/go-chi/chi/v5"
)

func handleListMenus(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		menuItems, _ := models.GetAllMenuItems()
		pages, _ := models.GetAllPages(true)

		content, err := renderTemplate("menus.html", map[string]interface{}{
			"MenuItems": menuItems,
			"Pages":     pages,
		})
		if err != nil {
			log.Printf("Menus template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Menus", content, "", pm)
	}
}

func handleAddMenuPage(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			pageIDStr := r.FormValue("page_id")
			orderStr := r.FormValue("menu_order")
			location := r.FormValue("location")
			openNewTab := r.FormValue("open_new_tab") == "on"

			order, _ := strconv.Atoi(orderStr)
			pageID, _ := strconv.Atoi(pageIDStr)

			page, err := models.GetPageByID(pageID)
			if err == nil {
				url := "/" + page.Slug
				models.CreateMenuItem(page.Title, url, order, location, openNewTab)
			}
		}
		http.Redirect(w, r, "/admin/menus", http.StatusFound)
	}
}

func handleAddMenuLink(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			label := r.FormValue("label")
			url := r.FormValue("url")
			orderStr := r.FormValue("menu_order")
			location := r.FormValue("location")
			openNewTab := r.FormValue("open_new_tab") == "on"

			order, _ := strconv.Atoi(orderStr)
			if label != "" && url != "" {
				models.CreateMenuItem(label, url, order, location, openNewTab)
			}
		}
		http.Redirect(w, r, "/admin/menus", http.StatusFound)
	}
}

func handleDeleteMenu(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			models.DeleteMenuItem(id)
		}
		http.Redirect(w, r, "/admin/menus", http.StatusFound)
	}
}

func handleReorderMenus(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var payload []struct {
				ID       int `json:"id"`
				Location string `json:"location"`
				Children []struct {
					ID int `json:"id"`
				} `json:"children"`
			}

			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				for i, item := range payload {
					models.UpdateMenuItemOrder(item.ID, 0, i+1, item.Location)
					for j, child := range item.Children {
						models.UpdateMenuItemOrder(child.ID, item.ID, j+1, item.Location)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

// handleEditMenuItem allows editing a menu item's label, URL, and new-tab setting.
func handleEditMenuItem(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		label := r.FormValue("label")
		url := r.FormValue("url")
		openNewTab := r.FormValue("open_new_tab") == "on"

		if label != "" && url != "" {
			models.UpdateMenuItem(id, label, url, openNewTab)
		}

		http.Redirect(w, r, "/admin/menus", http.StatusFound)
	}
}
