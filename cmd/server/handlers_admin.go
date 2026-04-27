package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ez8/gocms/internal/auth"
	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
	"github.com/ez8/gocms/pkg/plugin"
	"github.com/go-chi/chi/v5"
)

// getAdminMenus builds the sidebar menu by combining core items with plugin menus
// and optionally applying a custom layout from backend_menus.json.
func getAdminMenus(pm *pluginmanager.Manager) []plugin.MenuItem {
	menus := []plugin.MenuItem{
		{Label: "Dashboard", URL: "/admin", Icon: "home"},
		{
			Label: "Content", URL: "", Icon: "file-text",
			Children: []plugin.MenuItem{
				{Label: "Posts", URL: "/admin/posts"},
				{Label: "Pages", URL: "/admin/pages"},
				{Label: "Categories", URL: "/admin/categories"},
				{Label: "Tags", URL: "/admin/tags"},
				{Label: "Comments", URL: "/admin/comments"},
			},
		},
		{
			Label: "Design & Media", URL: "", Icon: "palette",
			Children: []plugin.MenuItem{
				{Label: "Media", URL: "/admin/media"},
				{Label: "Menus", URL: "/admin/menus"},
				{Label: "Appearance", URL: "/admin/themes"},
			},
		},
		{
			Label: "System", URL: "", Icon: "settings",
			Children: []plugin.MenuItem{
				{Label: "Users", URL: "/admin/users"},
				{Label: "Plugins", URL: "/admin/plugins"},
			{Label: "Plugin Store", URL: "/admin/marketplace"},
				{Label: "Core Settings", URL: "/admin/settings"},
			},
		},
	}
	menus = append(menus, pm.GetAdminMenus()...)

	b, err := os.ReadFile("backend_menus.json")
	if err == nil {
		var layout []plugin.MenuItem
		json.Unmarshal(b, &layout)

		usedURLs := make(map[string]bool)

		var build func([]plugin.MenuItem) []plugin.MenuItem
		build = func(items []plugin.MenuItem) []plugin.MenuItem {
			var out []plugin.MenuItem
			for _, item := range items {
				var match plugin.MenuItem
				found := false
				for _, m := range menus {
					if (m.URL == item.URL && m.URL != "") || (m.URL == "" && item.URL == "" && m.Label == item.Label) {
						match = m
						found = true
						break
					}
				}

				if !found {
					if strings.HasPrefix(item.URL, "/admin/plugin/") {
						continue // Drop orphaned plugin routes
					}
					match = item
				} else {
					match.Label = item.Label
					if item.Icon != "" {
						match.Icon = item.Icon
					}
				}

				if match.URL != "" {
					usedURLs[match.URL] = true
				} else {
					usedURLs["LABEL:"+match.Label] = true
				}

				if len(item.Children) > 0 {
					match.Children = build(item.Children)
				}

				// Drop empty parent containers
				if match.URL == "" && len(match.Children) == 0 {
					continue
				}

				out = append(out, match)
			}
			return out
		}

		finalMenus := build(layout)
		for _, m := range menus {
			if m.URL != "" {
				if !usedURLs[m.URL] {
					finalMenus = append(finalMenus, m)
				}
			} else {
				if !usedURLs["LABEL:"+m.Label] {
					finalMenus = append(finalMenus, m)
				}
			}
		}
		return finalMenus
	}

	return menus
}

func generateSlug(title string) string {
	lower := strings.ToLower(title)
	reg := regexp.MustCompile("[^a-z0-9]+")
	slug := reg.ReplaceAllString(lower, "-")
	return strings.Trim(slug, "-")
}

// renderAdminPage wraps content in the admin layout template with proper error handling.
func renderAdminPage(w http.ResponseWriter, r *http.Request, title string, content template.HTML, actions template.HTML, pm *pluginmanager.Manager) {
	t, err := theme.GetCachedTemplate(theme.GetBackendPath("layout.html"))
	if err != nil {
		log.Printf("Layout template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	brandColor := models.GetSetting("brand_color")
	if brandColor == "" {
		brandColor = "#206bc4"
	}

	user, _ := auth.GetSessionUser(r)
	pendingComments := models.GetPendingCommentCount()

	data := map[string]interface{}{
		"Title":           title,
		"Content":         content,
		"Actions":         actions,
		"Menus":           getAdminMenus(pm),
		"BrandColor":      brandColor,
		"Settings":        models.GetAllSettingsMap(),
		"TopRightWidget":  template.HTML(pm.GetAdminTopRightWidgets()),
		"User":            user,
		"PendingComments": pendingComments,
	}
	if err := t.Execute(w, data); err != nil {
		log.Printf("Layout execute error: %v", err)
	}
}

// renderTemplate is a helper that parses a backend template, executes it, and returns the HTML.
func renderTemplate(templateName string, data interface{}) (template.HTML, error) {
	t, err := theme.ParseTemplateWithFuncs(theme.GetBackendPath(templateName))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func handleAdminDashboard(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Plugin widgets
		// Create both a slice and a map to satisfy dashboard.html
		plList := pm.GetPluginsList()
		htmlPluginWidgets := []template.HTML{}
		htmlPluginWidgetsMap := make(map[string]template.HTML)

		for _, p := range plList {
			wd := p.HookDashboardWidget()
			if wd != "" {
				htmlPluginWidgets = append(htmlPluginWidgets, template.HTML(wd))
				// Map the plugin name explicitly 
				// Most plugins use "plugins/pluginname" as their route or SourceURL
				htmlPluginWidgetsMap["plugins/"+p.PluginName()] = template.HTML(wd)
				htmlPluginWidgetsMap[p.PluginName()] = template.HTML(wd)
			}
		}

		// Database-stored widgets
		dbWidgets, _ := models.GetEnabledWidgets()

		// Stats
		posts, _ := models.GetAllPosts(false)
		pages, _ := models.GetAllPages(false)
		pendingComments := models.GetPendingCommentCount()
		users, _ := models.GetUsersByRole("subscriber")

		content, err := renderTemplate("dashboard.html", map[string]interface{}{
			"PluginWidgets":     htmlPluginWidgets,
			"PluginWidgetsHTML": htmlPluginWidgetsMap,
			"Widgets":           dbWidgets,
			"PostCount":         len(posts),
			"PageCount":         len(pages),
			"PendingComments": pendingComments,
			"SubscriberCount": len(users),
		})
		if err != nil {
			log.Printf("Dashboard template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		actions := template.HTML(`<div class="d-flex gap-2 align-items-center">
			<button class="btn btn-success fw-bold align-items-center gap-1" id="save-layout-btn" onclick="saveLayout()"  style="display:none;">
				<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M6 4h10l4 4v10a2 2 0 0 1 -2 2h-12a2 2 0 0 1 -2 -2v-12a2 2 0 0 1 2 -2" /><path d="M12 14m-2 0a2 2 0 1 0 4 0a2 2 0 1 0 -4 0" /><path d="M14 4l0 4l-6 0l0 -4" /></svg>
				Save
			</button>
			<button class="btn btn-outline-primary fw-bold d-inline-flex align-items-center gap-1" id="edit-layout-btn" onclick="toggleEditMode()">
				<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M7 7h-1a2 2 0 0 0 -2 2v9a2 2 0 0 0 2 2h9a2 2 0 0 0 2 -2v-1" /><path d="M20.385 6.585a2.1 2.1 0 0 0 -2.97 -2.97l-8.415 8.385v3h3l8.385 -8.415z" /></svg>
				<span class="edit-label">Edit Layout</span>
			</button>
		</div>`)

		renderAdminPage(w, r, "Dashboard", content, actions, pm)
	}
}

func handleListPosts(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		posts, _ := models.GetAllPosts(false)

		content, err := renderTemplate("posts.html", map[string]interface{}{"Posts": posts})
		if err != nil {
			log.Printf("Posts template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		actions := template.HTML(`<a href="/admin/posts/new" class="btn btn-primary">New Post</a>`)
		renderAdminPage(w, r, "Posts", content, actions, pm)
	}
}

func handleNewPost(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			title := r.FormValue("title")
			content := r.FormValue("content")
			status := r.FormValue("status")
			if status == "" {
				status = "draft"
			}

			slug := generateSlug(title)
			user, _ := auth.GetSessionUser(r)

			post := models.Post{
				Title:           title,
				Slug:            slug,
				Content:         content,
				Status:          status,
				MetaTitle:       r.FormValue("meta_title"),
				MetaDescription: r.FormValue("meta_description"),
				FeaturedImage:   r.FormValue("featured_image"),
				AuthorID:        user.ID,
			}
			postID, err := models.CreatePost(post)
			if err != nil {
				log.Printf("Error creating post: %v", err)
			}

			// Handle categories and tags
			if postID > 0 {
				handlePostTaxonomies(r, int(postID))
			}

			http.Redirect(w, r, "/admin/posts", http.StatusFound)
			return
		}

		categories, _ := models.GetAllCategories()
		tags, _ := models.GetAllTags()

		content, err := renderTemplate("post_edit.html", map[string]interface{}{
			"Categories":     categories,
			"Tags":           tags,
			"FrontendStyles": theme.ExtractFrontendCSS(),
		})
		if err != nil {
			log.Printf("Post edit template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Create Post", content, "", pm)
	}
}

func handleEditPost(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid post ID", http.StatusBadRequest)
			return
		}

		if r.Method == "POST" {
			// Save revision before editing
			existingPost, _ := models.GetPostByID(id)
			if existingPost != nil {
				user, _ := auth.GetSessionUser(r)
				models.CreateRevision(id, existingPost.Title, existingPost.Content, user.ID)
				models.PruneOldRevisions(id, 10) // Keep last 10 revisions
			}

			title := r.FormValue("title")
			content := r.FormValue("content")
			status := r.FormValue("status")
			if status == "" {
				status = "draft"
			}

			user, _ := auth.GetSessionUser(r)

			post := models.Post{
				ID:              id,
				Title:           title,
				Slug:            generateSlug(title),
				Content:         content,
				Status:          status,
				MetaTitle:       r.FormValue("meta_title"),
				MetaDescription: r.FormValue("meta_description"),
				FeaturedImage:   r.FormValue("featured_image"),
				AuthorID:        user.ID,
			}
			models.UpdatePost(post)
			handlePostTaxonomies(r, id)

			http.Redirect(w, r, "/admin/posts", http.StatusSeeOther)
			return
		}

		post, err := models.GetPostByID(id)
		if err != nil {
			http.Error(w, "Post not found", http.StatusNotFound)
			return
		}

		categories, _ := models.GetAllCategories()
		tags, _ := models.GetAllTags()
		postCategories, _ := models.GetPostCategories(id)
		postTags, _ := models.GetPostTags(id)
		revisions, _ := models.GetRevisionsByPost(id)

		// Build selected IDs maps
		selectedCats := make(map[int]bool)
		for _, c := range postCategories {
			selectedCats[c.ID] = true
		}
		selectedTags := make(map[int]bool)
		for _, t := range postTags {
			selectedTags[t.ID] = true
		}

		content, err := renderTemplate("post_edit.html", map[string]interface{}{
			"Post":               post,
			"Categories":         categories,
			"Tags":               tags,
			"SelectedCategories": selectedCats,
			"SelectedTags":       selectedTags,
			"Revisions":          revisions,
			"FrontendStyles":     theme.ExtractFrontendCSS(),
		})
		if err != nil {
			log.Printf("Post edit template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Edit Post", content, "", pm)
	}
}

// handlePostTaxonomies processes category and tag form submissions for a post.
func handlePostTaxonomies(r *http.Request, postID int) {
	r.ParseForm()

	// Categories (checkboxes)
	var catIDs []int
	for _, idStr := range r.Form["categories"] {
		if id, err := strconv.Atoi(idStr); err == nil {
			catIDs = append(catIDs, id)
		}
	}
	models.SetPostCategories(postID, catIDs)

	// Tags (checkboxes)
	var tagIDs []int
	for _, idStr := range r.Form["tags"] {
		if id, err := strconv.Atoi(idStr); err == nil {
			tagIDs = append(tagIDs, id)
		}
	}
	models.SetPostTags(postID, tagIDs)
}

func handleDeletePost(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			models.DeletePost(id)
		}
		http.Redirect(w, r, "/admin/posts", http.StatusFound)
	}
}

func handlePluginAdminRoute(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		route := chi.URLParam(r, "*")
		fullRoute := "/admin/plugin/" + route
		
		if r.Method == "POST" && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			r.ParseMultipartForm(32 << 20) // 32MB max memory
			q := r.URL.Query()
			for k, v := range r.MultipartForm.Value {
				if len(v) > 0 {
					q.Set(k, v[0])
				}
			}
			os.MkdirAll("/tmp/gocms_plugin_uploads", 0755)
			for k, files := range r.MultipartForm.File {
				if len(files) > 0 {
					file, err := files[0].Open()
					if err == nil {
						// Prefix with random string or timestamp to prevent collisions, but keep filename for extension
						tmpPath := filepath.Join("/tmp/gocms_plugin_uploads", files[0].Filename)
						dst, err := os.Create(tmpPath)
						if err == nil {
							io.Copy(dst, file)
							dst.Close()
							q.Set("__file_"+k, tmpPath)
						}
						file.Close()
					}
				}
			}
			r.URL.RawQuery = q.Encode()
		}

		if r.URL.RawQuery != "" {
			fullRoute += "?" + r.URL.RawQuery
		}
		html := pm.RenderAdminRoute(fullRoute)

		// API routes return raw content
		if strings.Contains(fullRoute, "/api/") {
			if strings.HasPrefix(html, "{") && strings.HasSuffix(strings.TrimSpace(html), "}") {
				w.Header().Set("Content-Type", "application/json")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.Write([]byte(html))
			return
		}

		renderAdminPage(w, r, "Plugin Route", template.HTML(html), "", pm)
	}
}

type PluginDisplay struct {
	Filename string
	Name     string
	Active   bool
}

func handleListPlugins(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var displays []PluginDisplay

		files, err := os.ReadDir("plugins")
		if err == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				name := f.Name()
				if strings.HasSuffix(name, ".deleted") {
					continue
				}
				active := !strings.HasSuffix(name, ".disabled")
				displayName := strings.TrimSuffix(name, ".disabled")

				displays = append(displays, PluginDisplay{
					Filename: name,
					Name:     displayName,
					Active:   active,
				})
			}
		}

		content, err := renderTemplate("plugins.html", map[string]interface{}{"Plugins": displays})
		if err != nil {
			log.Printf("Plugins template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Plugins", content, "", pm)
	}
}

func handlePluginState(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/plugins", http.StatusFound)
			return
		}

		action := chi.URLParam(r, "action")
		filename := chi.URLParam(r, "filename")

		// Prevent directory traversal
		if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}

		pluginPath := filepath.Join("plugins", filename)

		switch action {
		case "activate":
			if strings.HasSuffix(filename, ".disabled") {
				newPath := strings.TrimSuffix(pluginPath, ".disabled")
				pm.Cleanup()
				err := os.Rename(pluginPath, newPath)
				pm.ReloadPlugins("plugins")
				if err != nil {
					http.Redirect(w, r, "/admin/plugins?action=error&msg="+err.Error(), http.StatusFound)
					return
				}
			}
		case "deactivate":
			if !strings.HasSuffix(filename, ".disabled") {
				pm.Cleanup()
				err := os.Rename(pluginPath, pluginPath+".disabled")
				pm.ReloadPlugins("plugins")
				if err != nil {
					http.Redirect(w, r, "/admin/plugins?action=error&msg="+err.Error(), http.StatusFound)
					return
				}
			}
		case "delete":
			pm.Cleanup()
			err := os.Remove(pluginPath)
			if err != nil {
				// ETXTBSY fallback: rename then remove
				errRename := os.Rename(pluginPath, pluginPath+".deleted")
				if errRename == nil {
					_ = os.Remove(pluginPath + ".deleted")
				} else {
					pm.ReloadPlugins("plugins")
					http.Redirect(w, r, "/admin/plugins?action=error&msg="+err.Error(), http.StatusFound)
					return
				}
			}
			pm.ReloadPlugins("plugins")
		}

		http.Redirect(w, r, "/admin/plugins?action=success", http.StatusFound)
	}
}

func handleUploadPlugin(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Require password re-entry for plugin uploads (RCE prevention)
		password := r.FormValue("admin_password")
		user, _ := auth.GetSessionUser(r)
		if !auth.CheckPasswordHash(password, user.PasswordHash) {
			http.Redirect(w, r, "/admin/plugins?error=Password+verification+required+for+plugin+uploads", http.StatusFound)
			return
		}

		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		file, handler, err := r.FormFile("plugin_binary")
		if err != nil {
			http.Error(w, "Error retrieving the file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if _, err := os.Stat("plugins"); os.IsNotExist(err) {
			os.Mkdir("plugins", 0755)
		}

		destFilename := handler.Filename
		if !strings.HasSuffix(destFilename, ".disabled") {
			destFilename += ".disabled"
		}
		destPath := filepath.Join("plugins", destFilename)
		tmpPath := destPath + ".tmp"

		dst, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			http.Error(w, "Error saving the file", http.StatusInternalServerError)
			return
		}

		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			http.Error(w, "Error copying the file", http.StatusInternalServerError)
			return
		}
		dst.Close()

		// Atomic swap to avoid ETXTBSY
		if err := os.Rename(tmpPath, destPath); err != nil {
			http.Error(w, "Error replacing plugin binary", http.StatusInternalServerError)
			return
		}

		pm.ReloadPlugins("plugins")
		http.Redirect(w, r, "/admin/plugins?upload=success", http.StatusFound)
	}
}

func handleAdminProfile(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.GetSessionUser(r)

		if r.Method == "POST" {
			target := r.FormValue("target")
			if target == "profile" {
				models.UpdateUserProfile(user.ID, r.FormValue("name"), r.FormValue("email"), r.FormValue("bio"))
				http.Redirect(w, r, "/admin/profile?tab=profile&success=1", http.StatusSeeOther)
				return
			} else if target == "security" {
				newPass := r.FormValue("new_password")
				if newPass != "" {
					models.UpdateUserPassword(user.ID, newPass)
				}
				http.Redirect(w, r, "/admin/profile?tab=security&success=1", http.StatusSeeOther)
				return
			}
		}

		// Reload user data
		user, _ = auth.GetSessionUser(r)

		content, err := renderTemplate("profile.html", map[string]interface{}{"User": user})
		if err != nil {
			http.Error(w, "Profile template missing", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Profile Settings", content, "", pm)
	}
}

func handlePluginPublicRoute(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		route := chi.URLParam(r, "*")
		fullRoute := "/api/plugin/" + route
		if r.URL.RawQuery != "" {
			fullRoute += "?" + r.URL.RawQuery
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			if len(body) > 0 {
				if strings.Contains(fullRoute, "?") {
					fullRoute += "&__body=" + url.QueryEscape(string(body))
				} else {
					fullRoute += "?__body=" + url.QueryEscape(string(body))
				}
				// Pass LemonSqueezy Signature
				sig := r.Header.Get("X-Signature")
				if sig != "" {
					fullRoute += "&__signature=" + url.QueryEscape(sig)
				}
			}
		}
		html := pm.RenderAdminRoute(fullRoute)

		if strings.HasPrefix(html, "{") && strings.HasSuffix(strings.TrimSpace(html), "}") {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Write([]byte(html))
	}
}
