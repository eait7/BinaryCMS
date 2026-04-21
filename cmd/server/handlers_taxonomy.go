package main

import (
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
	"github.com/go-chi/chi/v5"
)

// --- Category Handlers ---

func handleListCategories(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categories, _ := models.GetAllCategories()

		content, err := renderTemplate("categories.html", map[string]interface{}{
			"Categories": categories,
		})
		if err != nil {
			log.Printf("Categories template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Categories", content, "", pm)
	}
}

func handleCreateCategory(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/categories", http.StatusFound)
			return
		}

		name := r.FormValue("name")
		description := r.FormValue("description")
		parentID, _ := strconv.Atoi(r.FormValue("parent_id"))

		if name != "" {
			if err := models.CreateCategory(name, description, parentID); err != nil {
				log.Printf("Error creating category: %v", err)
			}
		}

		http.Redirect(w, r, "/admin/categories", http.StatusFound)
	}
}

func handleEditCategory(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Redirect(w, r, "/admin/categories", http.StatusFound)
			return
		}

		if r.Method == "POST" {
			name := r.FormValue("name")
			description := r.FormValue("description")
			parentID, _ := strconv.Atoi(r.FormValue("parent_id"))
			models.UpdateCategory(id, name, description, parentID)
			http.Redirect(w, r, "/admin/categories", http.StatusFound)
			return
		}

		cat, err := models.GetCategoryByID(id)
		if err != nil {
			http.Error(w, "Category not found", http.StatusNotFound)
			return
		}

		allCategories, _ := models.GetAllCategories()

		content, err := renderTemplate("category_edit.html", map[string]interface{}{
			"Category":   cat,
			"Categories": allCategories,
		})
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Edit Category", content, "", pm)
	}
}

func handleDeleteCategory(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			models.DeleteCategory(id)
		}
		http.Redirect(w, r, "/admin/categories", http.StatusFound)
	}
}

// --- Tag Handlers ---

func handleListTags(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tags, _ := models.GetAllTags()

		content, err := renderTemplate("tags.html", map[string]interface{}{
			"Tags": tags,
		})
		if err != nil {
			log.Printf("Tags template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Tags", content, "", pm)
	}
}

func handleCreateTag(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/tags", http.StatusFound)
			return
		}

		name := r.FormValue("name")
		if name != "" {
			models.CreateTag(name)
		}

		http.Redirect(w, r, "/admin/tags", http.StatusFound)
	}
}

func handleDeleteTag(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			models.DeleteTag(id)
		}
		http.Redirect(w, r, "/admin/tags", http.StatusFound)
	}
}

// --- Comment Handlers ---

func handleListComments(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		comments, _ := models.GetAllComments(status)

		content, err := renderTemplate("comments.html", map[string]interface{}{
			"Comments":      comments,
			"CurrentStatus": status,
		})
		if err != nil {
			log.Printf("Comments template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Comments", content, "", pm)
	}
}

func handleCommentAction(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/comments", http.StatusFound)
			return
		}

		action := chi.URLParam(r, "action")
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Redirect(w, r, "/admin/comments", http.StatusFound)
			return
		}

		switch action {
		case "approve":
			models.UpdateCommentStatus(id, "approved")
		case "spam":
			models.UpdateCommentStatus(id, "spam")
		case "trash":
			models.UpdateCommentStatus(id, "trash")
		case "delete":
			models.DeleteComment(id)
		}

		http.Redirect(w, r, "/admin/comments", http.StatusFound)
	}
}

// --- Search Handler ---

func handleAdminSearch(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		var posts []models.Post
		if query != "" {
			posts, _ = models.SearchPosts(query)
		}

		content, err := renderTemplate("search_results.html", map[string]interface{}{
			"Query":   query,
			"Results": posts,
		})
		if err != nil {
			log.Printf("Search template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Search Results", content, "", pm)
	}
}

// --- Public Frontend Taxonomy Routes ---

func handleFrontendCategory(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		category, err := models.GetCategoryBySlug(slug)
		if err != nil {
			http.Error(w, "Category not found", http.StatusNotFound)
			return
		}

		posts, _ := models.GetPostsByCategory(category.ID)

		t, err := template.ParseFiles(theme.GetFrontendPath("theme_archive.html"))
		if err != nil {
			// Fallback to index template
			t, _ = template.ParseFiles(theme.GetFrontendPath("theme_index.html"))
		}
		t.Execute(w, getFrontendData(r, map[string]interface{}{
			"Category":    category,
			"Posts":       posts,
			"ArchiveType": "Category",
			"ArchiveTitle": category.Name,
		}))
	}
}

func handleFrontendTag(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")

		// Find tag by slug
		tags, _ := models.GetAllTags()
		var foundTag *models.Tag
		for _, t := range tags {
			if t.Slug == slug {
				foundTag = &t
				break
			}
		}
		if foundTag == nil {
			http.Error(w, "Tag not found", http.StatusNotFound)
			return
		}

		posts, _ := models.GetPostsByTag(foundTag.ID)

		t, err := template.ParseFiles(theme.GetFrontendPath("theme_archive.html"))
		if err != nil {
			t, _ = template.ParseFiles(theme.GetFrontendPath("theme_index.html"))
		}
		t.Execute(w, getFrontendData(r, map[string]interface{}{
			"Tag":          foundTag,
			"Posts":        posts,
			"ArchiveType":  "Tag",
			"ArchiveTitle": foundTag.Name,
		}))
	}
}

// handleFrontendSearch handles the public search page.
func handleFrontendSearch(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		var posts []models.Post
		if query != "" {
			posts, _ = models.SearchPosts(query)
		}

		t, err := template.ParseFiles(theme.GetFrontendPath("theme_search.html"))
		if err != nil {
			t, _ = template.ParseFiles(theme.GetFrontendPath("theme_index.html"))
		}
		t.Execute(w, getFrontendData(r, map[string]interface{}{
			"Query":   query,
			"Results": posts,
		}))
	}
}
