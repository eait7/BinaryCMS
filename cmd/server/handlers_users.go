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
	"github.com/go-chi/chi/v5"
)

// --- Unified User Management Handlers ---

func handleListUsers(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		roleFilter := r.URL.Query().Get("role")
		statusFilter := r.URL.Query().Get("status")
		pageStr := r.URL.Query().Get("page")
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}

		users, total, _ := models.SearchUsers(query, roleFilter, statusFilter, page, 20)
		roleCounts := models.CountUsersByRole()
		statusCounts := models.CountUsersByStatus()
		totalUsers := models.GetTotalUserCount()

		totalPages := (total + 19) / 20

		content, err := renderTemplate("users.html", map[string]interface{}{
			"Users":        users,
			"Query":        query,
			"RoleFilter":   roleFilter,
			"StatusFilter": statusFilter,
			"CurrentPage":  page,
			"TotalPages":   totalPages,
			"TotalUsers":   totalUsers,
			"TotalResults": total,
			"RoleCounts":   roleCounts,
			"StatusCounts": statusCounts,
			"HasPrev":      page > 1,
			"HasNext":      page < totalPages,
			"PrevPage":     page - 1,
			"NextPage":     page + 1,
		})
		if err != nil {
			log.Printf("Users template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		actions := template.HTML(`<div class="d-flex gap-2">
			<a href="/admin/users/developer-guide" class="btn btn-outline-info d-none d-sm-inline-block fw-bold">
				<svg xmlns="http://www.w3.org/2000/svg" class="icon me-1" width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" /><path d="M10 13l-1 2l1 2" /><path d="M14 13l1 2l-1 2" /></svg>
				Plugin Dev Guide
			</a>
			<a href="/admin/users/new" class="btn btn-primary d-none d-sm-inline-block fw-bold">
				<svg xmlns="http://www.w3.org/2000/svg" class="icon me-1" width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M8 7a4 4 0 1 0 8 0a4 4 0 0 0 -8 0" /><path d="M16 19h6" /><path d="M19 16v6" /><path d="M6 21v-2a4 4 0 0 1 4 -4h4" /></svg>
				Add User
			</a>
		</div>`)
		renderAdminPage(w, r, "Users", content, actions, pm)
	}
}

func handleNewUser(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			username := strings.TrimSpace(r.FormValue("username"))
			password := r.FormValue("password")
			name := strings.TrimSpace(r.FormValue("name"))
			email := strings.TrimSpace(r.FormValue("email"))
			role := r.FormValue("role")
			if role == "" {
				role = "subscriber"
			}

			if username != "" && password != "" {
				userID, err := models.CreateUserFull(username, password, name, email, role)
				if err != nil {
					log.Printf("Error creating user: %v", err)
				} else {
					// Save optional fields via meta
					phone := strings.TrimSpace(r.FormValue("phone"))
					if phone != "" {
						models.SetUserMeta(int(userID), "phone", phone)
					}
					// Run plugin hooks
					if pm != nil {
						pm.RunUserRegisteredHook(int(userID))
					}
				}
			}
			http.Redirect(w, r, "/admin/users", http.StatusFound)
			return
		}

		content, err := renderTemplate("user_edit.html", map[string]interface{}{
			"IsNew": true,
		})
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Add User", content, "", pm)
	}
}

func handleEditUser(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Redirect(w, r, "/admin/users", http.StatusFound)
			return
		}

		user, err := models.GetUserByID(id)
		if err != nil {
			http.Redirect(w, r, "/admin/users", http.StatusFound)
			return
		}

		if r.Method == "POST" {
			target := r.FormValue("target")

			switch target {
			case "profile":
				user.Name = strings.TrimSpace(r.FormValue("name"))
				user.Email = strings.TrimSpace(r.FormValue("email"))
				user.Bio = r.FormValue("bio")
				user.Phone = strings.TrimSpace(r.FormValue("phone"))
				user.Role = r.FormValue("role")
				if user.Role == "" {
					user.Role = "subscriber"
				}
				models.UpdateUserFull(user)

			case "security":
				password := r.FormValue("new_password")
				if password != "" {
					models.UpdateUserPassword(id, password)
				}

			case "meta":
				metaKey := strings.TrimSpace(r.FormValue("meta_key"))
				metaValue := r.FormValue("meta_value")
				if metaKey != "" {
					models.SetUserMeta(id, metaKey, metaValue)
				}

			case "delete_meta":
				metaKey := r.FormValue("meta_key")
				if metaKey != "" {
					models.DeleteUserMeta(id, metaKey)
				}
			}

			http.Redirect(w, r, "/admin/users/edit/"+idStr+"?success=1", http.StatusFound)
			return
		}

		userMeta := models.GetAllUserMeta(id)

		// Collect plugin tabs
		var pluginTabs []template.HTML
		if pm != nil {
			for _, tab := range pm.GetUserProfileTabs(id) {
				pluginTabs = append(pluginTabs, template.HTML(tab))
			}
		}

		content, err := renderTemplate("user_edit.html", map[string]interface{}{
			"User":       user,
			"UserMeta":   userMeta,
			"PluginTabs": pluginTabs,
			"Success":    r.URL.Query().Get("success") == "1",
		})
		if err != nil {
			log.Printf("User edit template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Edit User: "+user.Name, content, "", pm)
	}
}

func handleDeleteUser(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err == nil {
			// Never delete the super admin (ID 1)
			if id != 1 {
				models.DeleteUser(id)
			}
		}
		http.Redirect(w, r, "/admin/users", http.StatusFound)
	}
}

func handleToggleUserStatus(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id == 1 {
			http.Redirect(w, r, "/admin/users", http.StatusFound)
			return
		}

		user, err := models.GetUserByID(id)
		if err != nil {
			http.Redirect(w, r, "/admin/users", http.StatusFound)
			return
		}

		if user.Status == "suspended" {
			models.ActivateUser(id)
		} else {
			models.SuspendUser(id)
		}

		http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
	}
}

// handleFrontendProfile renders the public user profile page.
func handleFrontendProfile(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.GetSessionUser(r)
		// Redirect to the new my-account page
		if user.ID > 0 {
			http.Redirect(w, r, "/my-account", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

// handleDevGuide renders the plugin developer guide for user management.
func handleDevGuide(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		content, err := renderTemplate("users_dev_guide.html", nil)
		if err != nil {
			log.Printf("Dev guide template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		renderAdminPage(w, r, "Plugin Developer Guide — User Hooks", content, "", pm)
	}
}
