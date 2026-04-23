package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
)

// validDomain validates domain names to prevent command injection.
var validDomain = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// validEmail validates email format (basic check).
var validEmail = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func handleSettings(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			homepageType := r.FormValue("homepage_type")
			pageID := r.FormValue("homepage_page_id")
			blogPageID := r.FormValue("homepage_blog_id")

			siteTitle := r.FormValue("site_title")
			siteTagline := r.FormValue("site_tagline")
			siteUrl := r.FormValue("site_url")
			siteLogoUrl := r.FormValue("site_logo_url")
			logoDisplayMode := r.FormValue("logo_display_mode")
			siteFaviconUrl := r.FormValue("site_favicon_url")
			customFooter := r.FormValue("custom_footer")

			if homepageType != "" {
				models.SetSetting("homepage_type", homepageType)
			}
			if pageID != "" {
				models.SetSetting("homepage_page_id", pageID)
			}
			if blogPageID != "" {
				models.SetSetting("homepage_blog_id", blogPageID)
			}
			if siteTitle != "" {
				models.SetSetting("site_title", siteTitle)
			}
			if siteTagline != "" {
				models.SetSetting("site_tagline", siteTagline)
			}

			// Optional fields can be set to empty
			models.SetSetting("site_url", siteUrl)
			models.SetSetting("site_logo_url", siteLogoUrl)
			models.SetSetting("logo_display_mode", logoDisplayMode)
			models.SetSetting("site_favicon_url", siteFaviconUrl)
			models.SetSetting("custom_footer", customFooter)

			sslDomain := r.FormValue("ssl_domain")
			sslEmail := r.FormValue("ssl_email")
			if sslDomain != "" {
				models.SetSetting("ssl_domain", sslDomain)
			}
			if sslEmail != "" {
				models.SetSetting("ssl_email", sslEmail)
			}

			logoSize := r.FormValue("logo_size")
			if logoSize != "" {
				models.SetSetting("logo_size", logoSize)
			}

			// Posts per page setting
			postsPerPage := r.FormValue("posts_per_page")
			if postsPerPage != "" {
				models.SetSetting("posts_per_page", postsPerPage)
			}

			// Comment settings
			commentsEnabled := r.FormValue("comments_enabled")
			commentModeration := r.FormValue("comment_moderation")
			models.SetSetting("comments_enabled", commentsEnabled)
			models.SetSetting("comment_moderation", commentModeration)

			if r.FormValue("generate_ssl") == "1" {
				if sslDomain == "" || sslEmail == "" {
					http.Redirect(w, r, "/admin/settings?error=Domain+and+Email+are+required+for+SSL", http.StatusFound)
					return
				}

				// SECURITY: Validate domain to prevent command injection
				if !validDomain.MatchString(sslDomain) {
					http.Redirect(w, r, "/admin/settings?error=Invalid+domain+name+format", http.StatusFound)
					return
				}
				if !validEmail.MatchString(sslEmail) {
					http.Redirect(w, r, "/admin/settings?error=Invalid+email+format", http.StatusFound)
					return
				}

				logOutput := "Initializing SSL Generation for " + sslDomain + "...\n"

				// Generate Nginx proxy configuration
				port := os.Getenv("PORT")
				if port == "" {
					port = "8080"
				}

				nginxConfig := `server {
    listen 80;
    server_name ` + sslDomain + `;

    location / {
        proxy_pass http://127.0.0.1:` + port + `;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}`
				err := os.WriteFile("/etc/nginx/sites-available/gocms", []byte(nginxConfig), 0644)
				if err != nil {
					logOutput += "Error generating Nginx config: " + err.Error() + "\n"
				} else {
					logOutput += "Nginx proxy configuration written successfully.\n"

					_ = os.Remove("/etc/nginx/sites-enabled/default")
					os.Symlink("/etc/nginx/sites-available/gocms", "/etc/nginx/sites-enabled/gocms")

					cmd := exec.Command("systemctl", "restart", "nginx")
					if err := cmd.Run(); err != nil {
						logOutput += "Failed to restart Nginx: " + err.Error() + "\n"
					} else {
						logOutput += "Nginx restarted.\n"

						certbotCmd := exec.Command("certbot", "--nginx", "-d", sslDomain,
							"--non-interactive", "--agree-tos", "-m", sslEmail, "--redirect")
						out, err := certbotCmd.CombinedOutput()
						logOutput += string(out)
						if err != nil {
							logOutput += "\nCertbot error: " + err.Error()
						} else {
							models.SetSetting("site_url", "https://"+sslDomain)
							logOutput += "\nSSL certificate provisioned! Access at https://" + sslDomain
						}
					}
				}

				renderSettingsPage(w, r, pm, logOutput)
				return
			}

			http.Redirect(w, r, "/admin/settings", http.StatusFound)
			return
		}

		renderSettingsPage(w, r, pm, "")
	}
}

func renderSettingsPage(w http.ResponseWriter, r *http.Request, pm *pluginmanager.Manager, sslLog string) {
	pages, _ := models.GetAllPages(true)
	currentType := models.GetSetting("homepage_type")
	currentID := models.GetSetting("homepage_page_id")
	currentBlogID := models.GetSetting("homepage_blog_id")

	t, err := theme.ParseTemplateWithFuncs(theme.GetBackendPath("settings.html"))
	if err != nil {
		log.Printf("Settings template error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	t.Execute(&buf, map[string]interface{}{
		"Pages":         pages,
		"CurrentType":   currentType,
		"CurrentPageID": currentID,
		"CurrentBlogID": currentBlogID,
		"Settings":      models.GetAllSettingsMap(),
		"SSLLog":        sslLog,
	})

	renderAdminPage(w, r, "Core Settings", template.HTML(buf.String()), "", pm)
}
