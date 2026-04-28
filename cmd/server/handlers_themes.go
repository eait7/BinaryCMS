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
			formAction := r.FormValue("form_action")

			if formAction == "customizer" {
				// Save all customizer settings
				customizerFields := []string{
					"brand_color", "color_secondary", "color_accent",
					"color_text", "color_background", "color_surface",
					"font_heading", "font_body",
					"spacing_section", "spacing_card", "border_radius",
					"nav_style", "nav_position",
				}
				for _, field := range customizerFields {
					val := r.FormValue(field)
					if val != "" {
						models.SetSetting(field, val)
					}
				}
				theme.InvalidateCache()
				http.Redirect(w, r, "/admin/themes?success=1", http.StatusFound)
				return
			}

			// Standard theme activation
			frontend := r.FormValue("frontend_theme")
			backend := r.FormValue("backend_theme")
			color := r.FormValue("brand_color")

			if frontend != "" {
				log.Printf("[DEBUG] Themes POST received frontend: %s", frontend)
				jsonPath := filepath.Join("themes", "frontend", frontend, "pages.json")
				if b, err := os.ReadFile(jsonPath); err == nil {
					var stubs []models.Page
					if err := json.Unmarshal(b, &stubs); err == nil {
						for _, stub := range stubs {
							if _, err := models.GetPageBySlug(stub.Slug); err != nil {
								stub.Status = "published"
								if stub.Title == "" {
									stub.Title = strings.ToUpper(stub.Slug[0:1]) + stub.Slug[1:]
								}
								models.CreatePage(stub)
							}
						}
					}
				}
				models.SetSetting("frontend_theme", frontend)
			}
			if backend != "" {
				models.SetSetting("backend_theme", backend)
			}
			if color != "" {
				models.SetSetting("brand_color", color)
			}

			theme.InvalidateCache()
			http.Redirect(w, r, "/admin/themes?success=1", http.StatusFound)
			return
		}

		// Load current settings
		getS := func(key, def string) string {
			v := models.GetSetting(key)
			if v == "" {
				return def
			}
			return v
		}

		data := map[string]interface{}{
			"FrontendTheme":  getS("frontend_theme", "default"),
			"BackendTheme":   getS("backend_theme", "default"),
			"BrandColor":     getS("brand_color", "#206bc4"),
			"ColorSecondary": getS("color_secondary", "#6c757d"),
			"ColorAccent":    getS("color_accent", "#f76707"),
			"ColorText":      getS("color_text", "#1e293b"),
			"ColorBackground":getS("color_background", "#f8fafc"),
			"ColorSurface":   getS("color_surface", "#ffffff"),
			"FontHeading":    getS("font_heading", "Inter"),
			"FontBody":       getS("font_body", "Inter"),
			"SpacingSection": getS("spacing_section", "4"),
			"SpacingCard":    getS("spacing_card", "1.5"),
			"BorderRadius":   getS("border_radius", "0.75"),
			"NavStyle":       getS("nav_style", "glassmorphic"),
			"NavPosition":    getS("nav_position", "fixed"),
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

func handleExportTheme(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentTheme := models.GetSetting("frontend_theme")
		if currentTheme == "" {
			currentTheme = "default"
		}

		themeDir := filepath.Join("themes", "frontend", currentTheme)
		if _, err := os.Stat(themeDir); os.IsNotExist(err) {
			http.Error(w, "Theme not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+currentTheme+".zip\"")

		zw := zip.NewWriter(w)
		defer zw.Close()

		filepath.Walk(themeDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			relPath, _ := filepath.Rel(themeDir, path)
			f, err := zw.Create(relPath)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			f.Write(data)
			return nil
		})

		// Also include a generated variables.css with current settings
		getS := func(key, def string) string {
			v := models.GetSetting(key)
			if v == "" {
				return def
			}
			return v
		}
		cssVars := `:root {
  --cms-color-primary: ` + getS("brand_color", "#206bc4") + `;
  --cms-color-secondary: ` + getS("color_secondary", "#6c757d") + `;
  --cms-color-accent: ` + getS("color_accent", "#f76707") + `;
  --cms-color-text: ` + getS("color_text", "#1e293b") + `;
  --cms-color-background: ` + getS("color_background", "#f8fafc") + `;
  --cms-color-surface: ` + getS("color_surface", "#ffffff") + `;
  --cms-font-heading: '` + getS("font_heading", "Inter") + `', sans-serif;
  --cms-font-body: '` + getS("font_body", "Inter") + `', sans-serif;
  --cms-spacing-section: ` + getS("spacing_section", "4") + `rem;
  --cms-spacing-card: ` + getS("spacing_card", "1.5") + `rem;
  --cms-radius: ` + getS("border_radius", "0.75") + `rem;
}
`
		if f, err := zw.Create("variables.css"); err == nil {
			f.Write([]byte(cssVars))
		}
	}
}
