package corelock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CoreLock represents the lock manifest
type CoreLock struct {
	Version  string            `json:"version"`
	LockedAt string            `json:"locked_at"`
	Commit   string            `json:"commit"`
	Files    map[string]string `json:"files"`
}

// Violation represents a single integrity violation
type Violation struct {
	File   string
	Reason string // "modified", "deleted", "added"
}

// coreDirs are directories whose contents are considered core
var coreDirs = []string{
	"cmd",
	"internal",
	"pkg",
	"themes/backend",
	"static",
}

// coreFiles are individual files at the root that are core
var coreFiles = []string{
	"backend_menus.json",
	"backend_menus_available.json",
	"go.mod",
	"go.sum",
	"Dockerfile",
	"install.sh",
}

// excludePatterns are paths to skip even inside core dirs
var excludePatterns = []string{
	".git",
	"plugins/",
	"plugins_data/",
	"plugins_src/",
	"uploads/",
	"data/",
	"themes/frontend/",
	"core.lock",
	"data/core.lock",
}

// getLockFilePath returns the path to the core.lock file.
// It checks data/core.lock first (Docker), then falls back to core.lock (local dev).
func getLockFilePath() string {
	if _, err := os.Stat("data"); err == nil {
		return "data/core.lock"
	}
	os.MkdirAll("data", 0755)
	return "data/core.lock"
}

// hashFile computes SHA256 of a file
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isExcluded checks if a path should be skipped
func isExcluded(path string) bool {
	for _, pattern := range excludePatterns {
		if strings.HasPrefix(path, pattern) || strings.Contains(path, "/"+pattern) {
			return true
		}
	}
	// Skip binary files, db files, logs
	ext := strings.ToLower(filepath.Ext(path))
	skipExts := map[string]bool{
		".db": true, ".db-shm": true, ".db-wal": true,
		".log": true, ".exe": true, ".DS_Store": true,
	}
	if skipExts[ext] {
		return true
	}
	// Skip the compiled binaries
	base := filepath.Base(path)
	if base == "server" || base == "server_local" || base == "gocms_server" {
		return true
	}
	return false
}

// collectCoreFiles walks core directories and collects all file paths
func collectCoreFiles() ([]string, error) {
	var files []string

	// Walk core directories
	for _, dir := range coreDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip unreadable files
			}
			if info.IsDir() {
				return nil
			}
			if isExcluded(path) {
				return nil
			}
			files = append(files, filepath.ToSlash(path))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Add individual core files
	for _, f := range coreFiles {
		if _, err := os.Stat(f); err == nil {
			if !isExcluded(f) {
				files = append(files, f)
			}
		}
	}

	sort.Strings(files)
	return files, nil
}

// GenerateLock scans all core files and writes core.lock
func GenerateLock(commit string) error {
	files, err := collectCoreFiles()
	if err != nil {
		return fmt.Errorf("failed to collect core files: %w", err)
	}

	lock := CoreLock{
		Version:  "1.0.0",
		LockedAt: time.Now().UTC().Format(time.RFC3339),
		Commit:   commit,
		Files:    make(map[string]string),
	}

	for _, path := range files {
		hash, err := hashFile(path)
		if err != nil {
			fmt.Printf("  ⚠ Skipping %s: %v\n", path, err)
			continue
		}
		lock.Files[path] = hash
	}

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lock: %w", err)
	}

	if err := os.WriteFile(getLockFilePath(), data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", getLockFilePath(), err)
	}

	fmt.Printf("🔒 Core locked! %d files hashed and saved to %s\n", len(lock.Files), getLockFilePath())
	fmt.Printf("   Commit: %s\n", lock.Commit)
	fmt.Printf("   Locked at: %s\n", lock.LockedAt)
	return nil
}

// VerifyLock checks all core files against core.lock and returns violations
func VerifyLock() ([]Violation, error) {
	data, err := os.ReadFile(getLockFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no core.lock found — system is unlocked (run --lock-core to generate)")
		}
		return nil, fmt.Errorf("failed to read %s: %w", getLockFilePath(), err)
	}

	var lock CoreLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", getLockFilePath(), err)
	}

	var violations []Violation

	// Check each locked file still matches
	for path, expectedHash := range lock.Files {
		currentHash, err := hashFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				violations = append(violations, Violation{File: path, Reason: "deleted"})
			} else {
				violations = append(violations, Violation{File: path, Reason: fmt.Sprintf("unreadable: %v", err)})
			}
			continue
		}
		if currentHash != expectedHash {
			violations = append(violations, Violation{File: path, Reason: "modified"})
		}
	}

	// Check for new files added to core dirs that aren't in the lock
	currentFiles, err := collectCoreFiles()
	if err != nil {
		return violations, fmt.Errorf("failed to scan current files: %w", err)
	}
	for _, path := range currentFiles {
		if _, exists := lock.Files[path]; !exists {
			violations = append(violations, Violation{File: path, Reason: "added (not in lock)"})
		}
	}

	return violations, nil
}

// HasLockFile checks if core.lock exists
func HasLockFile() bool {
	_, err := os.Stat(getLockFilePath())
	return err == nil
}
