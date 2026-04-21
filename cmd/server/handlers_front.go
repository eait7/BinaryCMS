package main

import (
	"bytes"
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
	"github.com/gomarkdown/markdown"
)

func getFrontendData(r *http.Request, data map[string]interface{}) map[string]interface{} {
	rawMenuItems, _ := models.GetAllMenuItems()
	brandColor := models.GetSetting("brand_color")
	if brandColor == "" {
		brandColor = "#206bc4"
	}

	user, _ := auth.GetSessionUser(r)

	var menuItems []models.MenuItem
	var footerMenu1 []models.MenuItem
	var footerMenu2 []models.MenuItem
	var footerMenu3 []models.MenuItem

	for _, mi := range rawMenuItems {
		allowed := true

		if strings.HasPrefix(mi.URL, "/") && len(mi.URL) > 1 {
			slug := mi.URL[1:]
			page, err := models.GetPageBySlug(slug)
			if err == nil && page.RequiredRole != "" {
				if user.ID == 0 {
					allowed = false
				} else if user.Role != "admin" && !strings.Contains(page.RequiredRole, user.Role) {
					allowed = false
				}
			}
		}

		if allowed {
			switch mi.Location {
			case "footer_1":
				footerMenu1 = append(footerMenu1, mi)
			case "footer_2":
				footerMenu2 = append(footerMenu2, mi)
			case "footer_3":
				footerMenu3 = append(footerMenu3, mi)
			default:
				menuItems = append(menuItems, mi)
			}
		}
	}

	if data == nil {
		data = make(map[string]interface{})
	}
	data["MenuItems"] = menuItems
	data["FooterMenu1"] = footerMenu1
	data["FooterMenu2"] = footerMenu2
	data["FooterMenu3"] = footerMenu3
	data["BrandColor"] = brandColor
	data["Settings"] = models.GetAllSettingsMap()
	data["User"] = user
	return data
}

func handleFrontendIndex(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if a static page is set as homepage
		homepageType := models.GetSetting("homepage_type")
		if homepageType == "page" {
			pageIDStr := models.GetSetting("homepage_page_id")
			pageID, _ := strconv.Atoi(pageIDStr)
			if pageID > 0 {
				page, err := models.GetPageByID(pageID)
				if err == nil && page.Status == "published" {
					// Role-based access check
					if page.RequiredRole != "" {
						user, _ := auth.GetSessionUser(r)
						if user.ID == 0 || (user.Role != "admin" && !strings.Contains(page.RequiredRole, user.Role)) {
							http.Redirect(w, r, "/login?next=/", http.StatusSeeOther)
							return
						}
					}

					htmlContent := markdown.ToHTML([]byte(page.Content), nil, nil)
					t, err := template.ParseFiles(theme.GetFrontendPath("theme_page.html"))
					if err != nil {
						log.Printf("Homepage page template error: %v", err)
						http.Error(w, "Template error", http.StatusInternalServerError)
						return
					}
					data := getFrontendData(r, map[string]interface{}{
						"Page":    page,
						"Content": template.HTML(htmlContent),
					})
					var buf bytes.Buffer
					if err := t.Execute(&buf, data); err != nil {
						log.Printf("Template execution error: %v", err)
						http.Error(w, "Template error", http.StatusInternalServerError)
						return
					}
					finalHTML := buf.String()
					if pm != nil {
						if hooked := pm.RunHook("BeforeFrontPageRender", finalHTML); hooked != nil {
							finalHTML = hooked.(string)
						}
					}
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Write([]byte(finalHTML))
					return
				}
			}
		}

		// Default: paginated post timeline
		pageNum := 1
		if p := r.URL.Query().Get("page"); p != "" {
			pageNum, _ = strconv.Atoi(p)
		}
		perPageStr := models.GetSetting("posts_per_page")
		perPage, _ := strconv.Atoi(perPageStr)
		if perPage < 1 {
			perPage = 10
		}

		posts, total, err := models.GetPaginatedPosts(pageNum, perPage, true)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Convert markdown to HTML
		var postMap []map[string]interface{}
		for _, p := range posts {
			htmlContent := markdown.ToHTML([]byte(p.Content), nil, nil)
			postMap = append(postMap, map[string]interface{}{
				"Post":    p,
				"Content": template.HTML(htmlContent),
			})
		}

		totalPages := (total + perPage - 1) / perPage

		t, err := template.ParseFiles(theme.GetFrontendPath("theme_index.html"))
		if err != nil {
			log.Printf("Index template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		data := getFrontendData(r, map[string]interface{}{
			"Posts":       postMap,
			"CurrentPage": pageNum,
			"TotalPages":  totalPages,
			"TotalPosts":  total,
			"HasPrev":     pageNum > 1,
			"HasNext":     pageNum < totalPages,
			"PrevPage":    pageNum - 1,
			"NextPage":    pageNum + 1,
		})
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		finalHTML := buf.String()
		if pm != nil {
			if hooked := pm.RunHook("BeforeFrontPageRender", finalHTML); hooked != nil {
				finalHTML = hooked.(string)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(finalHTML))
	}
}

func handleFrontendPost(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")

		post, err := models.GetPostBySlug(slug)
		if err != nil || post.Status != "published" {
			http.Error(w, "Post not found", http.StatusNotFound)
			return
		}

		htmlContent := markdown.ToHTML([]byte(post.Content), nil, nil)

		// Load comments if enabled
		commentsEnabled := models.GetSetting("comments_enabled") == "true"
		var comments []models.Comment
		if commentsEnabled {
			comments, _ = models.GetCommentsByPost(post.ID)
		}

		// Load post categories and tags
		categories, _ := models.GetPostCategories(post.ID)
		tags, _ := models.GetPostTags(post.ID)

		t, err := template.ParseFiles(theme.GetFrontendPath("theme_post.html"))
		if err != nil {
			log.Printf("Post template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		data := getFrontendData(r, map[string]interface{}{
			"Post":            post,
			"Content":         template.HTML(htmlContent),
			"Comments":        comments,
			"CommentsEnabled": commentsEnabled,
			"Categories":      categories,
			"Tags":            tags,
		})
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		finalHTML := buf.String()
		if pm != nil {
			if hooked := pm.RunHook("BeforeFrontPageRender", finalHTML); hooked != nil {
				finalHTML = hooked.(string)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(finalHTML))
	}
}

// handleFrontendCommentSubmit handles public comment form submissions.
func handleFrontendCommentSubmit(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		slug := chi.URLParam(r, "slug")
		post, err := models.GetPostBySlug(slug)
		if err != nil || post.Status != "published" {
			http.Error(w, "Post not found", http.StatusNotFound)
			return
		}

		// Honeypot spam check
		if r.FormValue("website") != "" {
			// Bot detected, silently redirect
			http.Redirect(w, r, "/post/"+slug+"#comments", http.StatusFound)
			return
		}

		authorName := strings.TrimSpace(r.FormValue("author_name"))
		authorEmail := strings.TrimSpace(r.FormValue("author_email"))
		content := strings.TrimSpace(r.FormValue("content"))
		parentID, _ := strconv.Atoi(r.FormValue("parent_id"))

		if authorName == "" || content == "" {
			http.Redirect(w, r, "/post/"+slug+"?error=Name+and+comment+are+required#comments", http.StatusFound)
			return
		}

		status := "approved"
		if models.GetSetting("comment_moderation") == "true" {
			status = "pending"
		}

		comment := models.Comment{
			PostID:      post.ID,
			ParentID:    parentID,
			AuthorName:  authorName,
			AuthorEmail: authorEmail,
			Content:     content,
			Status:      status,
		}
		models.CreateComment(comment)

		http.Redirect(w, r, "/post/"+slug+"?comment=submitted#comments", http.StatusFound)
	}
}

func handleFrontendPage(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")

		page, err := models.GetPageBySlug(slug)
		if err != nil || page.Status != "published" {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}

		// Role-based access check
		if page.RequiredRole != "" {
			user, _ := auth.GetSessionUser(r)
			if user.ID == 0 || (user.Role != "admin" && !strings.Contains(page.RequiredRole, user.Role)) {
				http.Redirect(w, r, "/login?next=/"+slug, http.StatusSeeOther)
				return
			}
		}

		var htmlContent []byte
		if strings.HasPrefix(slug, "demo-") {
			htmlContent = []byte(page.Content)
		} else {
			htmlContent = markdown.ToHTML([]byte(page.Content), nil, nil)
		}

		data := getFrontendData(r, map[string]interface{}{
			"Page":    page,
			"Content": template.HTML(htmlContent),
		})

		var t *template.Template
		if strings.HasPrefix(slug, "demo-") {
			t, _ = template.ParseFiles(theme.GetFrontendPath("theme_demo.html"))
		} else {
			t, _ = template.ParseFiles(theme.GetFrontendPath("theme_page.html"))
		}
		if t != nil {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				log.Printf("Template execution error: %v", err)
				http.Error(w, "Template error", http.StatusInternalServerError)
				return
			}
			finalHTML := buf.String()
			if pm != nil {
				if hooked := pm.RunHook("BeforeFrontPageRender", finalHTML); hooked != nil {
					finalHTML = hooked.(string)
				}
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(finalHTML))
		} else {
			http.Error(w, "Template not found", http.StatusInternalServerError)
		}
	}
}
