package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/go-chi/chi/v5"
	"io"
)

type MediaFile struct {
	Name string
	URL  string
}

func handleMediaLibrary(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var media []MediaFile

		files, err := os.ReadDir("uploads")
		if err == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				media = append(media, MediaFile{
					Name: f.Name(),
					URL:  "/uploads/" + f.Name(),
				})
			}
		}

		content, err := renderTemplate("media.html", map[string]interface{}{"Files": media})
		if err != nil {
			log.Printf("Media template error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		renderAdminPage(w, r, "Media Library", content, "", pm)
	}
}

// handleMediaJSON returns all uploaded images as JSON for GrapesJS Asset Manager.
func handleMediaJSON() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var assets []string
		imgExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
		files, err := os.ReadDir("uploads")
		if err == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(f.Name()))
				if imgExts[ext] {
					assets = append(assets, "/uploads/"+f.Name())
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": assets})
	}
}

// Allowed media file extensions (SVG excluded for XSS safety).
var allowedMediaExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".pdf": true, ".mp4": true, ".mp3": true,
	".csv": true, ".txt": true,
}

func handleMediaUpload(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// EasyMDE uses "image", standard forms use "media_file"
		var file io.ReadCloser
		var hFilename string

		// Try multiple field names: GrapesJS uses "files[]", EasyMDE uses "image", form uses "media_file"
		file, fHandler, err := r.FormFile("files[]")
		if err != nil {
			file, fHandler, err = r.FormFile("image")
		}
		if err != nil {
			file, fHandler, err = r.FormFile("media_file")
		}
		if err != nil {
			if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "multipart") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "No file provided"})
			} else {
				http.Error(w, "Error retrieving the file", http.StatusBadRequest)
			}
			return
		}
		defer file.Close()
		hFilename = fHandler.Filename

		_ = os.MkdirAll("uploads", 0755)

		// Sanitize filename to prevent path traversal
		hFilename = filepath.Base(hFilename)

		// Extension allowlist check
		ext := strings.ToLower(filepath.Ext(hFilename))
		if !allowedMediaExt[ext] {
			if strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "File extension not permitted"})
			} else {
				http.Error(w, "Forbidden file format", http.StatusForbidden)
			}
			return
		}

		destPath := filepath.Join("uploads", hFilename)

		dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			if strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to save file"})
			} else {
				http.Error(w, "Failed saving file", http.StatusInternalServerError)
			}
			return
		}
		io.Copy(dst, file)
		dst.Close()

		// JSON response for AJAX uploads (GrapesJS, EasyMDE, and native API)
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []string{"/uploads/" + hFilename},
			})
			return
		}

		http.Redirect(w, r, "/admin/media?upload=success", http.StatusFound)
	}
}

func handleMediaDelete(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/media", http.StatusFound)
			return
		}

		filename := chi.URLParam(r, "filename")
		if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}

		destPath := filepath.Join("uploads", filename)
		_ = os.Remove(destPath)

		http.Redirect(w, r, "/admin/media?delete=success", http.StatusFound)
	}
}
