package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ez8/gocms/internal/marketplace"
	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
)

// handleMarketplaceInstall handles POST /admin/plugins/store/install
func handleMarketplaceInstall(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Slug       string `json:"slug"`
			LicenseKey string `json:"license_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Slug == "" {
			jsonError(w, "Plugin slug is required", http.StatusBadRequest)
			return
		}

		// Prevent directory traversal
		if strings.Contains(req.Slug, "/") || strings.Contains(req.Slug, "\\") || strings.Contains(req.Slug, "..") {
			jsonError(w, "Invalid plugin slug", http.StatusBadRequest)
			return
		}

		hubURL := models.GetSetting("marketplace_hub_url")
		if hubURL == "" {
			hubURL = "https://binarycms.com/api/plugin/marketplace-hub"
		}
		siteURL := models.GetSetting("site_url")
		client := marketplace.NewHubClient(hubURL, siteURL)

		var binaryData []byte
		var computedHash string
		var err error

		if req.LicenseKey != "" {
			// Paid plugin: validate license first
			licResp, err := client.ValidateLicense(req.Slug, req.LicenseKey)
			if err != nil {
				jsonError(w, "License validation failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			if !licResp.Valid {
				jsonError(w, "Invalid license key: "+licResp.Message, http.StatusForbidden)
				return
			}

			// Download with license
			binaryData, computedHash, err = client.DownloadPluginWithLicense(req.Slug, req.LicenseKey)
			if err != nil {
				jsonError(w, "Download failed: "+err.Error(), http.StatusBadGateway)
				return
			}

			// Store the license locally
			if saveErr := models.SavePluginLicense(req.Slug, req.LicenseKey, siteURL); saveErr != nil {
				log.Printf("Marketplace: Failed to save license for %s: %v", req.Slug, saveErr)
			}
		} else {
			// Free plugin: download directly
			binaryData, computedHash, err = client.DownloadPlugin(req.Slug)
			if err != nil {
				jsonError(w, "Download failed: "+err.Error(), http.StatusBadGateway)
				return
			}
		}

		// Ensure plugins directory exists
		os.MkdirAll("plugins", 0755)

		// Write to plugins directory as disabled by default (safe activation)
		destPath := filepath.Join("plugins", req.Slug+".disabled")
		tmpPath := destPath + ".tmp"

		if err := os.WriteFile(tmpPath, binaryData, 0755); err != nil {
			jsonError(w, "Failed to save plugin binary", http.StatusInternalServerError)
			return
		}

		// Atomic rename
		if err := os.Rename(tmpPath, destPath); err != nil {
			os.Remove(tmpPath)
			jsonError(w, "Failed to install plugin", http.StatusInternalServerError)
			return
		}

		log.Printf("Marketplace: Installed plugin %s (SHA-256: %s)", req.Slug, computedHash)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Plugin installed successfully! Go to Plugins to activate it.",
			"slug":    req.Slug,
			"sha256":  computedHash,
		})
	}
}

// handleMarketplaceActivate handles POST /admin/plugins/store/activate
func handleMarketplaceActivate(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Slug       string `json:"slug"`
			LicenseKey string `json:"license_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Slug == "" || req.LicenseKey == "" {
			jsonError(w, "Plugin slug and license key are required", http.StatusBadRequest)
			return
		}

		hubURL := models.GetSetting("marketplace_hub_url")
		if hubURL == "" {
			hubURL = "https://binarycms.com/api/plugin/marketplace-hub"
		}
		siteURL := models.GetSetting("site_url")
		client := marketplace.NewHubClient(hubURL, siteURL)

		// Validate the license key against the hub
		licResp, err := client.ValidateLicense(req.Slug, req.LicenseKey)
		if err != nil {
			jsonError(w, "License validation failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if !licResp.Valid {
			jsonError(w, "Invalid license key: "+licResp.Message, http.StatusForbidden)
			return
		}

		// Store the license locally
		if err := models.SavePluginLicense(req.Slug, req.LicenseKey, siteURL); err != nil {
			jsonError(w, "Failed to save license", http.StatusInternalServerError)
			return
		}

		log.Printf("Marketplace: License activated for plugin %s", req.Slug)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "License activated successfully!",
			"slug":    req.Slug,
		})
	}
}

// handleMarketplaceStatus returns JSON of all installed plugins and their license status.
func handleMarketplaceStatus(pm *pluginmanager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		installedPlugins := make(map[string]bool)
		files, _ := os.ReadDir("plugins")
		for _, f := range files {
			if f.IsDir() || strings.HasSuffix(f.Name(), ".deleted") {
				continue
			}
			name := strings.TrimSuffix(f.Name(), ".disabled")
			installedPlugins[name] = true
		}

		licenseMap := models.GetPluginLicenseMap()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"installed": installedPlugins,
			"licenses":  licenseMap,
		})
	}
}

// jsonError sends a JSON error response.
func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
