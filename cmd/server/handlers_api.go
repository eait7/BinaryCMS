package main

import (
	"encoding/json"
	"net/http"

	"github.com/ez8/gocms/internal/models"
	"github.com/go-chi/chi/v5"
)

func handleAPIPosts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		posts, err := models.GetAllPosts(true)
		if err != nil {
			http.Error(w, "Failed to get posts", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(posts)
	}
}

func handleAPIPost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		post, err := models.GetPostBySlug(slug)
		if err != nil || post.Status != "published" {
			http.Error(w, "Post not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(post)
	}
}

func handleAPIPages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// GetPublicPages filters to published + no required_role to prevent
		// leaking restricted page content to unauthenticated API consumers.
		pages, err := models.GetPublicPages()
		if err != nil {
			http.Error(w, "Failed to get pages", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pages)
	}
}

func handleAPIPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		page, err := models.GetPageBySlug(slug)
		if err != nil || page.Status != "published" {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}
		// Block access to role-restricted pages via the public API.
		if page.RequiredRole != "" {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	}
}

// handleAPISearch provides a JSON search endpoint.
func handleAPISearch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]models.Post{})
			return
		}

		posts, err := models.SearchPosts(query)
		if err != nil {
			http.Error(w, "Search failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(posts)
	}
}
