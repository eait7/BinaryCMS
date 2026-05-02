package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ez8/gocms/internal/models"
)

// Injected by ldflags at CI build time
var (
	GitCommit = "development"
	BuildTime = "unknown"
)

type GitHubRelease struct {
	TagName         string `json:"tag_name"`
	Name            string `json:"name"`
	PublishedAt     string `json:"published_at"`
	TargetCommitish string `json:"target_commitish"`
	Assets          []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		UpdatedAt          string `json:"updated_at"`
	} `json:"assets"`
}

// UpdaterCheckResponse represents the API response for the update check
type UpdaterCheckResponse struct {
	CurrentVersion  string `json:"current_version"`
	BuildTime       string `json:"build_time"`
	UpdateAvailable bool   `json:"update_available"`
	LatestRelease   string `json:"latest_release"`
	ReleaseNotes    string `json:"release_notes"`
	DownloadURL     string `json:"download_url"`
}

// CheckUpdate polls GitHub for the latest release and compares it with the current build.
func CheckUpdate(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get("https://api.github.com/repos/eait7/binarycms/releases/latest")
	if err != nil {
		http.Error(w, `{"error": "Failed to check GitHub releases"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// No releases found or rate limited
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UpdaterCheckResponse{
			CurrentVersion:  GitCommit,
			BuildTime:       BuildTime,
			UpdateAvailable: false,
		})
		return
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		http.Error(w, `{"error": "Failed to parse release data"}`, http.StatusInternalServerError)
		return
	}

	downloadURL := ""
	assetUpdatedAt := ""
	for _, asset := range release.Assets {
		if asset.Name == "gocms_server_linux_amd64" {
			downloadURL = asset.BrowserDownloadURL
			assetUpdatedAt = asset.UpdatedAt
			break
		}
	}

	// Determine if update is available using multiple strategies:
	// 1. Check if we already installed this exact release (saved in DB)
	// 2. Compare build timestamps
	// 3. For development builds, compare asset updated time with last known install
	updateAvailable := false

	if downloadURL != "" {
		// Check if user already installed this version
		installedVersion := models.GetSetting("installed_release_asset_time")

		if installedVersion != "" && installedVersion == assetUpdatedAt {
			// Already installed this exact release asset
			updateAvailable = false
		} else if BuildTime != "unknown" && BuildTime != "" {
			// CI-built binary: compare build time against release publish time
			pubTime, errP := time.Parse(time.RFC3339, release.PublishedAt)
			bldTime, errB := time.Parse(time.RFC3339, BuildTime)
			if errP == nil && errB == nil {
				// Only show update if the release is newer than our build + tolerance
				if pubTime.After(bldTime.Add(15 * time.Minute)) {
					updateAvailable = true
				}
			}
		} else {
			// Local/development build: check if we previously installed an update
			if installedVersion == "" {
				// First run — check if asset is newer than 10 minutes
				if assetUpdatedAt != "" {
					assetTime, err := time.Parse(time.RFC3339, assetUpdatedAt)
					if err == nil {
						// Only show update if the release asset is less than 7 days old
						if time.Since(assetTime) < 7*24*time.Hour {
							updateAvailable = true
						}
					}
				}
			} else if installedVersion != assetUpdatedAt {
				// We installed a previous version but a newer asset is available
				updateAvailable = true
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UpdaterCheckResponse{
		CurrentVersion:  GitCommit,
		BuildTime:       BuildTime,
		UpdateAvailable: updateAvailable,
		LatestRelease:   release.TagName,
		ReleaseNotes:    release.Name,
		DownloadURL:     downloadURL,
	})
}

// InstallUpdate downloads the latest binary and hot-swaps the current executable.
func InstallUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DownloadURL == "" {
		http.Error(w, `{"error": "Invalid download URL"}`, http.StatusBadRequest)
		return
	}

	// Validate the URL is from GitHub
	if !strings.HasPrefix(req.DownloadURL, "https://github.com/eait7/BinaryCMS/") &&
		!strings.HasPrefix(req.DownloadURL, "https://github.com/eait7/binarycms/") {
		http.Error(w, `{"error": "Invalid download source"}`, http.StatusBadRequest)
		return
	}

	// 1. Download the new binary to a temporary file
	log.Printf("Updater: Downloading new core binary from %s", req.DownloadURL)
	resp, err := http.Get(req.DownloadURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Updater error: Failed to download binary: %v (status: %d)", err, resp.StatusCode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to download update from GitHub"})
		return
	}
	defer resp.Body.Close()

	tmpPath := "/tmp/gocms_server_update"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		log.Printf("Updater error: Failed to create temp file: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to prepare update file"})
		return
	}

	written, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		log.Printf("Updater error: Failed to write temp file: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save update"})
		return
	}

	// Basic sanity check — binary should be at least 1MB
	if written < 1024*1024 {
		os.Remove(tmpPath)
		log.Printf("Updater error: Downloaded file too small (%d bytes), likely not a valid binary", written)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Downloaded file appears invalid (too small)"})
		return
	}

	log.Printf("Updater: Downloaded %d bytes successfully", written)

	// 2. Identify current executable path
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Updater error: Cannot resolve executable: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Cannot resolve current binary path"})
		return
	}

	// 3. Hot-swap the binary
	// First try to remove the old binary
	if err := os.Remove(exe); err != nil && !os.IsNotExist(err) {
		// If we can't remove, try renaming as backup
		os.Rename(exe, exe+".old")
	}

	// Move new binary into place
	if err := os.Rename(tmpPath, exe); err != nil {
		// Cross-device link fails in Docker (/tmp is different filesystem), use cp
		log.Printf("Updater: Rename failed (cross-device), falling back to cp: %v", err)
		cpCmd := exec.Command("cp", tmpPath, exe)
		if cpErr := cpCmd.Run(); cpErr != nil {
			log.Printf("Updater error: cp also failed: %v", cpErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to replace binary"})
			return
		}
		os.Remove(tmpPath)
	}

	os.Chmod(exe, 0755)

	// 4. Record the installed version so we don't show the notification again
	releaseResp, err := http.Get("https://api.github.com/repos/eait7/binarycms/releases/latest")
	if err == nil {
		defer releaseResp.Body.Close()
		var rel GitHubRelease
		if json.NewDecoder(releaseResp.Body).Decode(&rel) == nil {
			for _, asset := range rel.Assets {
				if asset.Name == "gocms_server_linux_amd64" {
					models.SetSetting("installed_release_asset_time", asset.UpdatedAt)
					log.Printf("Updater: Recorded installed version: %s", asset.UpdatedAt)
					break
				}
			}
		}
	}

	log.Printf("Updater: Binary successfully swapped at %s (%d bytes). Triggering restart.", exe, written)

	// 5. Send success response before restarting
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Update installed. System is restarting...",
	})

	// 6. Restart — exit cleanly and let Docker/systemd restart the process
	go func() {
		time.Sleep(1 * time.Second)
		log.Println("Updater: Shutting down for restart...")
		os.Exit(0) // Docker restart policy or systemd will bring us back with the new binary
	}()
}
