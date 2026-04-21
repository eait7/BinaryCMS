package main

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/ez8/gocms/internal/auth"
	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
	"github.com/go-chi/chi/v5"
)

func handleListPages(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pages, _ := models.GetAllPages(false)

		content, err := renderTemplate("pages.html", map[string]interface{}{"Pages": pages})
		if err != nil {
			log.Printf("Pages template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		actions := template.HTML(`<a href="/admin/pages/new" class="btn btn-primary d-none d-sm-inline-block">New Page</a>`)
		renderAdminPage(w, r, "Pages", content, actions, pm)
	}
}

func handleNewPage(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			title := r.FormValue("title")
			content := r.FormValue("content")
			status := r.FormValue("status")
			if status == "" {
				status = "draft"
			}
			showInMenu := r.FormValue("show_in_menu") == "on"
			menuOrder, _ := strconv.Atoi(r.FormValue("menu_order"))
			slug := generateSlug(title)

			r.ParseForm()
			requiredRoleStr := strings.Join(r.Form["required_roles"], ",")

			user, _ := auth.GetSessionUser(r)

			page := models.Page{
				Title:           title,
				Slug:            slug,
				Content:         content,
				Status:          status,
				MetaTitle:       r.FormValue("meta_title"),
				MetaDescription: r.FormValue("meta_description"),
				ShowInMenu:      showInMenu,
				MenuOrder:       menuOrder,
				AuthorID:        user.ID,
				FeaturedImage:   r.FormValue("featured_image"),
				RequiredRole:    requiredRoleStr,
			}
			models.CreatePage(page)
			http.Redirect(w, r, "/admin/pages", http.StatusFound)
			return
		}

		content, err := renderTemplate("page_edit.html", map[string]interface{}{
			"IsDemo":         true,
			"FrontendStyles": theme.ExtractFrontendCSS(),
		})
		if err != nil {
			log.Printf("Page edit template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Create Page", content, "", pm)
	}
}

func handleEditPage(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid page ID", http.StatusBadRequest)
			return
		}

		if r.Method == "POST" {
			title := r.FormValue("title")
			content := r.FormValue("content")
			status := r.FormValue("status")
			if status == "" {
				status = "draft"
			}
			showInMenu := r.FormValue("show_in_menu") == "on"
			menuOrder, _ := strconv.Atoi(r.FormValue("menu_order"))
			slug := generateSlug(title)

			r.ParseForm()
			requiredRoleStr := strings.Join(r.Form["required_roles"], ",")

			page := models.Page{
				ID:              id,
				Title:           title,
				Slug:            slug,
				Content:         content,
				Status:          status,
				ShowInMenu:      showInMenu,
				MenuOrder:       menuOrder,
				MetaTitle:       r.FormValue("meta_title"),
				MetaDescription: r.FormValue("meta_description"),
				FeaturedImage:   r.FormValue("featured_image"),
				RequiredRole:    requiredRoleStr,
			}
			models.UpdatePage(page)
			http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
			return
		}

		page, err := models.GetPageByID(id)
		if err != nil {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}

		content, err := renderTemplate("page_edit.html", map[string]interface{}{
			"Page":           page,
			"IsDemo":         true,
			"FrontendStyles": theme.ExtractFrontendCSS(),
		})
		if err != nil {
			log.Printf("Page edit template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Edit Page", content, "", pm)
	}
}

func handleDeletePage(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			models.DeletePage(id)
		}
		http.Redirect(w, r, "/admin/pages", http.StatusFound)
	}
}
