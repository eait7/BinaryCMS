package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Injected by ldflags
var (
	GitCommit = "development"
	BuildTime = "unknown"
)

type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// UpdaterCheckResponse represents the API response for the update check
type UpdaterCheckResponse struct {
	CurrentVersion string `json:"current_version"`
	BuildTime      string `json:"build_time"`
	UpdateAvailable bool  `json:"update_available"`
	LatestRelease  string `json:"latest_release"`
	ReleaseNotes   string `json:"release_notes"`
	DownloadURL    string `json:"download_url"`
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
		json.NewEncoder(w).Encode(UpdaterCheckResponse{
			CurrentVersion: GitCommit,
			BuildTime:      BuildTime,
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
	for _, asset := range release.Assets {
		if asset.Name == "gocms_server_linux_amd64" {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	// We determine an update is available if the release's published date is newer than our build time,
	// or if we're on a "development" commit and there's a valid release available.
	updateAvailable := false
	if downloadURL != "" {
		if BuildTime == "unknown" || BuildTime == "" {
			updateAvailable = true
		} else {
			pubTime, _ := time.Parse(time.RFC3339, release.PublishedAt)
			bldTime, _ := time.Parse(time.RFC3339, BuildTime)
			if pubTime.After(bldTime) {
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

	// 1. Download the new binary to a temporary file
	log.Printf("Updater: Downloading new core binary from %s", req.DownloadURL)
	resp, err := http.Get(req.DownloadURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Updater error: Failed to download binary: %v", err)
		http.Error(w, `{"error": "Failed to download update"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	tmpPath := "/tmp/gocms_server_update"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		log.Printf("Updater error: Failed to create temp file: %v", err)
		http.Error(w, `{"error": "Failed to prepare update"}`, http.StatusInternalServerError)
		return
	}
	
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		log.Printf("Updater error: Failed to write temp file: %v", err)
		http.Error(w, `{"error": "Failed to save update"}`, http.StatusInternalServerError)
		return
	}

	// 2. Identify current executable path
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Updater error: Cannot resolve executable: %v", err)
		http.Error(w, `{"error": "Cannot resolve executable path"}`, http.StatusInternalServerError)
		return
	}

	// 3. Hot-swap the binary
	// Remove the old binary first (required if it's currently running/locked by OS)
	if err := os.Remove(exe); err != nil && !os.IsNotExist(err) {
		// Fallback: move it aside
		os.Rename(exe, exe+".old")
	}

	// Move the new binary into place
	if err := os.Rename(tmpPath, exe); err != nil {
		// If cross-device link fails, try copy
		exec.Command("cp", tmpPath, exe).Run()
		os.Remove(tmpPath)
	}

	// Ensure it's executable
	os.Chmod(exe, 0755)

	log.Printf("Updater: Binary successfully swapped at %s. Triggering graceful reboot.", exe)

	// Send success response before exiting
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Update installed. System is restarting...",
	})

	// 4. Restart the server
	go func() {
		time.Sleep(2 * time.Second)
		// We can use syscall.Exec to replace the current process natively
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Printf("Updater error: Failed to Exec new binary: %v. Exiting.", err)
			os.Exit(0) // Let Docker/systemd restart it
		}
	}()
}
