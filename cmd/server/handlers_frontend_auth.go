package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/ez8/gocms/internal/auth"
	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
)

// handleFrontendRegister handles GET (render form) and POST (create user) for /register.
func handleFrontendRegister(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if registration is enabled
		if models.GetSetting("registration_enabled") != "true" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		data := getFrontendData(r, nil)

		if r.Method == "POST" {
			name := strings.TrimSpace(r.FormValue("name"))
			email := strings.TrimSpace(r.FormValue("email"))
			password := r.FormValue("password")
			passwordConfirm := r.FormValue("password_confirm")
			phone := strings.TrimSpace(r.FormValue("phone"))

			// Validation
			var errMsg string
			if name == "" || email == "" {
				errMsg = "Name and email are required."
			} else if !isValidEmail(email) {
				errMsg = "Please enter a valid email address."
			} else if len(password) < 8 {
				errMsg = "Password must be at least 8 characters."
			} else if password != passwordConfirm {
				errMsg = "Passwords do not match."
			}

			// Check for duplicate email
			if errMsg == "" {
				if _, err := models.GetUserByEmail(email); err == nil {
					errMsg = "An account with this email already exists."
				}
			}

			// Generate username from email
			username := generateUsernameFromEmail(email)

			// Check for duplicate username
			if errMsg == "" {
				if _, err := models.GetUserByUsername(username); err == nil {
					// Append a number
					for i := 1; i < 100; i++ {
						candidate := fmt.Sprintf("%s%d", username, i)
						if _, err := models.GetUserByUsername(candidate); err != nil {
							username = candidate
							break
						}
					}
				}
			}

			if errMsg != "" {
				data["Flash"] = map[string]string{"Type": "error", "Message": errMsg}
				renderFrontendTemplate(w, "theme_register.html", data)
				return
			}

			// Create user
			role := models.GetSetting("default_user_role")
			if role == "" {
				role = "subscriber"
			}

			userID, err := models.CreateUserFull(username, password, name, email, role)
			if err != nil {
				log.Printf("Registration error: %v", err)
				data["Flash"] = map[string]string{"Type": "error", "Message": "Registration failed. Please try again."}
				renderFrontendTemplate(w, "theme_register.html", data)
				return
			}

			// Save optional phone
			if phone != "" {
				models.SetUserMeta(int(userID), "phone", phone)
			}

			// Run plugin hooks
			if pm != nil {
				pm.RunUserRegisteredHook(int(userID))
			}

			// Auto-login
			auth.SetSession(w, r, int(userID))

			http.Redirect(w, r, "/my-account", http.StatusFound)
			return
		}

		renderFrontendTemplate(w, "theme_register.html", data)
	}
}

// handleFrontendMyAccount renders the user's account dashboard.
func handleFrontendMyAccount(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.GetSessionUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		data := getFrontendData(r, nil)
		data["User"] = user
		data["UserMeta"] = models.GetAllUserMeta(user.ID)

		// Collect plugin cards for the account page
		if pm != nil {
			pluginCards := pm.GetUserAccountCards(user.ID)
			var htmlCards []template.HTML
			for _, c := range pluginCards {
				htmlCards = append(htmlCards, template.HTML(c))
			}
			data["PluginCards"] = htmlCards
		}

		// Flash messages from query params
		if r.URL.Query().Get("success") == "profile" {
			data["Flash"] = map[string]string{"Type": "success", "Message": "Profile updated successfully."}
		} else if r.URL.Query().Get("success") == "password" {
			data["Flash"] = map[string]string{"Type": "success", "Message": "Password changed successfully."}
		} else if r.URL.Query().Get("error") != "" {
			data["Flash"] = map[string]string{"Type": "error", "Message": r.URL.Query().Get("error")}
		}

		renderFrontendTemplate(w, "theme_my_account.html", data)
	}
}

// handleFrontendUpdateProfile handles profile update POST from /my-account/update.
func handleFrontendUpdateProfile(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.GetSessionUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		email := strings.TrimSpace(r.FormValue("email"))

		if name == "" || email == "" {
			http.Redirect(w, r, "/my-account?error=Name+and+email+are+required", http.StatusFound)
			return
		}

		// Check if email changed and is unique
		if email != user.Email {
			if existing, err := models.GetUserByEmail(email); err == nil && existing.ID != user.ID {
				http.Redirect(w, r, "/my-account?error=Email+already+in+use", http.StatusFound)
				return
			}
		}

		models.UpdateUserProfile(user.ID, name, email, r.FormValue("bio"))

		// Update phone via meta
		phone := strings.TrimSpace(r.FormValue("phone"))
		if phone != "" {
			models.SetUserMeta(user.ID, "phone", phone)
		}

		http.Redirect(w, r, "/my-account?success=profile", http.StatusFound)
	}
}

// handleFrontendChangePassword handles password change POST from /my-account/password.
func handleFrontendChangePassword(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.GetSessionUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		currentPassword := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("new_password_confirm")

		// Verify current password
		if !auth.CheckPasswordHash(currentPassword, user.PasswordHash) {
			http.Redirect(w, r, "/my-account?error=Current+password+is+incorrect", http.StatusFound)
			return
		}

		if len(newPassword) < 8 {
			http.Redirect(w, r, "/my-account?error=New+password+must+be+at+least+8+characters", http.StatusFound)
			return
		}

		if newPassword != confirmPassword {
			http.Redirect(w, r, "/my-account?error=New+passwords+do+not+match", http.StatusFound)
			return
		}

		models.UpdateUserPassword(user.ID, newPassword)
		http.Redirect(w, r, "/my-account?success=password", http.StatusFound)
	}
}

// renderFrontendTemplate is a helper to render a frontend theme template with data.
func renderFrontendTemplate(w http.ResponseWriter, templateName string, data map[string]interface{}) {
	t, err := template.ParseFiles(theme.GetFrontendPath(templateName))
	if err != nil {
		log.Printf("Frontend template error (%s): %v", templateName, err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, data)
}

// generateUsernameFromEmail creates a username from the email prefix.
func generateUsernameFromEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	username := parts[0]
	// Clean: only allow alphanumeric, underscore, hyphen, dot
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	username = reg.ReplaceAllString(username, "")
	if username == "" {
		username = "user"
	}
	return strings.ToLower(username)
}

// isValidEmail does basic email format validation.
func isValidEmail(email string) bool {
	re := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return re.MatchString(email)
}
