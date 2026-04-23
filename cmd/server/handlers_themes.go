package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/ez8/gocms/internal/theme"
	"github.com/go-chi/chi/v5"
)

type ThemeInfo struct {
	ID          string
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
}

func getAvailableThemes(themeType string) []ThemeInfo {
	var themes []ThemeInfo
	basePath := filepath.Join("themes", themeType)
	entries, err := os.ReadDir(basePath)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				info := ThemeInfo{
					ID:   e.Name(),
					Name: e.Name(),
				}

				// Try to load theme metadata
				jsonPath := filepath.Join(basePath, e.Name(), "theme.json")
				b, err := os.ReadFile(jsonPath)
				if err == nil {
					_ = json.Unmarshal(b, &info)
				}

				themes = append(themes, info)
			}
		}
	}

	if len(themes) == 0 {
		themes = append(themes, ThemeInfo{ID: "default", Name: "Default"})
	}
	return themes
}

func handleThemes(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			frontend := r.FormValue("frontend_theme")
			backend := r.FormValue("backend_theme")
			color := r.FormValue("brand_color")

			if frontend != "" {
				log.Printf("[DEBUG] Themes POST received frontend: %s", frontend)
				// Auto-provision missing required pages for the activated template
				jsonPath := filepath.Join("themes", "frontend", frontend, "pages.json")
				if b, err := os.ReadFile(jsonPath); err == nil {
					log.Printf("[DEBUG] Found pages.json for %s", frontend)
					var stubs []models.Page
					if err := json.Unmarshal(b, &stubs); err == nil {
						log.Printf("[DEBUG] Successfully unmarshaled %d stubs", len(stubs))
						for _, stub := range stubs {
							if _, err := models.GetPageBySlug(stub.Slug); err != nil {
								log.Printf("[DEBUG] Page '%s' does not exist, creating...", stub.Slug)
								// Page doesn't exist, dynamically provision it natively
								stub.Status = "published"
								if stub.Title == "" {
									stub.Title = strings.ToUpper(stub.Slug[0:1]) + stub.Slug[1:]
								}
								createErr := models.CreatePage(stub)
								if createErr != nil {
									log.Printf("[ERROR] Failed to create page '%s': %v", stub.Slug, createErr)
								} else {
									log.Printf("[DEBUG] Successfully created page '%s'", stub.Slug)
								}
							} else {
								log.Printf("[DEBUG] Page '%s' already exists, skipping", stub.Slug)
							}
						}
					} else {
						log.Printf("[ERROR] Failed to unmarshal pages.json: %v", err)
					}
				} else {
					log.Printf("[DEBUG] No pages.json found at %s: %v", jsonPath, err)
				}
				models.SetSetting("frontend_theme", frontend)
			}
			if backend != "" {
				models.SetSetting("backend_theme", backend)
			}
			if color != "" {
				models.SetSetting("brand_color", color)
			}

			// Invalidate template cache on theme change
			theme.InvalidateCache()

			http.Redirect(w, r, "/admin/themes?success=1", http.StatusFound)
			return
		}

		currentFront := models.GetSetting("frontend_theme")
		if currentFront == "" {
			currentFront = "default"
		}
		currentBack := models.GetSetting("backend_theme")
		if currentBack == "" {
			currentBack = "default"
		}
		color := models.GetSetting("brand_color")
		if color == "" {
			color = "#206bc4"
		}

		data := map[string]interface{}{
			"FrontendTheme":  currentFront,
			"BackendTheme":   currentBack,
			"BrandColor":     color,
			"FrontendThemes": getAvailableThemes("frontend"),
			"BackendThemes":  getAvailableThemes("backend"),
		}

		t, err := theme.ParseTemplateWithFuncs(theme.GetBackendPath("themes.html"))
		if err != nil {
			log.Printf("Themes template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		t.Execute(&buf, data)

		renderAdminPage(w, r, "Appearance Themes", template.HTML(buf.String()), "", pm)
	}
}

func handleUploadTheme(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(32 << 20)
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		themeType := r.FormValue("theme_type")
		if themeType != "frontend" && themeType != "backend" {
			http.Error(w, "Invalid theme type", http.StatusBadRequest)
			return
		}

		file, handler, err := r.FormFile("theme_zip")
		if err != nil {
			http.Error(w, "Error retrieving the file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if !strings.HasSuffix(handler.Filename, ".zip") {
			http.Error(w, "Only .zip files are supported", http.StatusBadRequest)
			return
		}

		themeName := strings.TrimSuffix(handler.Filename, ".zip")
		themeDir := filepath.Join("themes", themeType, themeName)
		_ = os.MkdirAll(themeDir, 0755)

		// Write to disk first (zip.Reader needs io.ReaderAt)
		tempZipPath := filepath.Join("themes", themeType, handler.Filename)
		dst, err := os.OpenFile(tempZipPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			http.Error(w, "Failed writing temp zip", http.StatusInternalServerError)
			return
		}
		io.Copy(dst, file)
		dst.Close()
		defer os.Remove(tempZipPath)

		// Extract zip
		zr, err := zip.OpenReader(tempZipPath)
		if err != nil {
			http.Error(w, "Failed opening zip", http.StatusBadRequest)
			return
		}
		defer zr.Close()

		for _, f := range zr.File {
			// Prevent directory traversal
			if strings.Contains(f.Name, "..") {
				continue
			}

			fBasePath := filepath.Base(f.Name)
			if f.FileInfo().IsDir() {
				continue
			}

			// Save screenshots to uploads
			if fBasePath == "screenshot.png" || fBasePath == "screenshot.jpg" || fBasePath == "screenshot.jpeg" {
				dstPath := filepath.Join("uploads", "theme_screenshot_"+themeName+".png")
				dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
				if err == nil {
					srcFile, err := f.Open()
					if err == nil {
						io.Copy(dstFile, srcFile)
						srcFile.Close()
					}
					dstFile.Close()
				}
				continue
			}

			// Extract template files
			fPath := filepath.Join(themeDir, fBasePath)
			dstFile, err := os.OpenFile(fPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err == nil {
				srcFile, err := f.Open()
				if err == nil {
					io.Copy(dstFile, srcFile)
					srcFile.Close()
				}
				dstFile.Close()
			}
		}

		// Invalidate template cache
		theme.InvalidateCache()

		http.Redirect(w, r, "/admin/themes?upload=success", http.StatusFound)
	}
}

func handleDeleteTheme(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		themeType := chi.URLParam(r, "type")
		themeName := chi.URLParam(r, "name")

		if themeType != "frontend" && themeType != "backend" {
			http.Redirect(w, r, "/admin/themes?error=Invalid+theme+type", http.StatusFound)
			return
		}
		if themeName == "" || themeName == "default" || strings.Contains(themeName, "..") {
			http.Redirect(w, r, "/admin/themes?error=Invalid+theme+name", http.StatusFound)
			return
		}

		// Do not allow deleting active theme
		activeTheme := models.GetSetting(themeType + "_theme")
		if activeTheme == themeName {
			http.Redirect(w, r, "/admin/themes?error=Cannot+delete+active+theme", http.StatusFound)
			return
		}

		// Remove template folder
		themeDir := filepath.Join("themes", themeType, themeName)
		os.RemoveAll(themeDir)

		// Remove static assets folder from uploads
		assetsDir := filepath.Join("uploads", themeName)
		os.RemoveAll(assetsDir)

		// Remove screenshot
		screenshotPath := filepath.Join("uploads", "theme_screenshot_"+themeName+".png")
		os.Remove(screenshotPath)

		// Invalidate cache
		theme.InvalidateCache()

		http.Redirect(w, r, "/admin/themes?delete=success", http.StatusFound)
	}
}
