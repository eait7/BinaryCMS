package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ez8/gocms/internal/models"
)

// Injected by ldflags at CI build time
var (
	CoreVersion = "v1.0.0" // Hardcoded semantic version
	GitCommit   = "development"
	BuildTime   = "unknown"
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
			CurrentVersion:  CoreVersion,
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

	updateAvailable := false

	if downloadURL != "" {
		if release.TagName == "latest" {
			// Legacy rolling release fallback
			installedVersion := models.GetSetting("installed_release_asset_time")
			if installedVersion != "" && installedVersion == assetUpdatedAt {
				updateAvailable = false
			} else if BuildTime != "unknown" && BuildTime != "" {
				pubTime, errP := time.Parse(time.RFC3339, release.PublishedAt)
				bldTime, errB := time.Parse(time.RFC3339, BuildTime)
				if errP == nil && errB == nil && pubTime.After(bldTime.Add(15*time.Minute)) {
					updateAvailable = true
				}
			} else {
				if installedVersion == "" && assetUpdatedAt != "" {
					assetTime, err := time.Parse(time.RFC3339, assetUpdatedAt)
					if err == nil && time.Since(assetTime) < 7*24*time.Hour {
						updateAvailable = true
					}
				} else if installedVersion != assetUpdatedAt {
					updateAvailable = true
				}
			}
		} else {
			// Semantic versioning
			if release.TagName != CoreVersion && release.TagName != "" {
				updateAvailable = true
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UpdaterCheckResponse{
		CurrentVersion:  CoreVersion,
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

	// 1. Determine the path of the currently running binary
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Updater error: Cannot determine executable path: %v", err)
		http.Error(w, `{"error": "Cannot determine executable path"}`, http.StatusInternalServerError)
		return
	}
	log.Printf("Updater: Current executable: %s", execPath)

	// 2. Download the new binary to a temp file in the same directory as the binary
	// (same filesystem ensures os.Rename is atomic and doesn't cross device boundaries)
	tmpPath := execPath + ".update_tmp"
	log.Printf("Updater: Downloading new core binary from %s", req.DownloadURL)
	resp, err := http.Get(req.DownloadURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		log.Printf("Updater error: Failed to download binary: %v (status: %d)", err, statusCode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to download update from GitHub"})
		return
	}
	defer resp.Body.Close()

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		log.Printf("Updater error: Failed to create temp file at %s: %v", tmpPath, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to prepare update file — check write permissions"})
		return
	}

	written, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
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
	log.Printf("Updater: Downloaded %d bytes successfully to %s", written, tmpPath)

	// 3. Atomically rename the new binary over the current executable.
	// On Linux, renaming onto a running binary is safe — the OS keeps the old
	// inode mapped in memory, and only new exec() calls pick up the new file.
	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		log.Printf("Updater error: Failed to replace binary at %s: %v", execPath, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to replace binary — permission denied. Ensure the process owns the executable file."})
		return
	}
	log.Printf("Updater: Binary successfully replaced at %s (%d bytes).", execPath, written)

	// 4. Record the installed version so the update notice goes away
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

	// 5. Send success response before restarting
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Update installed. System is restarting...",
	})

	// 6. Exit cleanly — Docker's restart policy (unless-stopped) will re-launch
	// the container, which will exec the newly placed binary directly.
	go func() {
		time.Sleep(1 * time.Second)
		log.Println("Updater: Shutting down for restart...")
		os.Exit(0)
	}()
}

