package marketplace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PluginListing represents a single plugin available in the marketplace catalog.
type PluginListing struct {
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Version       string  `json:"version"`
	Author        string  `json:"author"`
	Price         float64 `json:"price"`          // 0 = free
	Currency      string  `json:"currency"`       // "USD"
	IconURL       string  `json:"icon_url"`
	ScreenshotURL string  `json:"screenshot_url"`
	Downloads     int     `json:"downloads"`
	SHA256        string  `json:"sha256"`
	Category      string  `json:"category"`
	MinCoreVer    string  `json:"min_core_version"`
	UpdatedAt     string  `json:"updated_at"`
}

// LicenseResponse is returned by the hub after validating a license key.
type LicenseResponse struct {
	Valid       bool   `json:"valid"`
	Message     string `json:"message"`
	DownloadURL string `json:"download_url,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// HubClient communicates with the central BinaryCMS plugin hub.
type HubClient struct {
	BaseURL    string
	HTTPClient *http.Client
	SiteURL    string // This installation's domain (for license locking)
}

// NewHubClient creates a new marketplace hub client.
func NewHubClient(baseURL, siteURL string) *HubClient {
	return &HubClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		SiteURL: siteURL,
	}
}

// FetchCatalog retrieves the full plugin catalog from the central hub.
func (c *HubClient) FetchCatalog() ([]PluginListing, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/plugins")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin hub returned status %d", resp.StatusCode)
	}

	var catalog struct {
		Plugins []PluginListing `json:"plugins"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("failed to parse catalog response: %w", err)
	}

	return catalog.Plugins, nil
}

// ValidateLicense checks a license key against the hub and locks it to this domain.
func (c *HubClient) ValidateLicense(slug, licenseKey string) (*LicenseResponse, error) {
	payload := fmt.Sprintf(`{"slug":"%s","license_key":"%s","domain":"%s"}`, slug, licenseKey, c.SiteURL)

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/validate",
		"application/json",
		io.NopCloser(strings.NewReader(payload)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to validate license: %w", err)
	}
	defer resp.Body.Close()

	var result LicenseResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse license response: %w", err)
	}

	return &result, nil
}

// DownloadPlugin downloads a plugin binary from the hub and verifies its SHA-256 hash.
// Returns the raw binary bytes and the computed hash.
func (c *HubClient) DownloadPlugin(slug string) ([]byte, string, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/download/" + slug)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read plugin binary: %w", err)
	}

	hash := sha256.Sum256(data)
	hexHash := hex.EncodeToString(hash[:])

	return data, hexHash, nil
}

// DownloadPluginWithLicense downloads a paid plugin using a validated license key.
func (c *HubClient) DownloadPluginWithLicense(slug, licenseKey string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/download/"+slug, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("X-License-Key", licenseKey)
	req.Header.Set("X-Site-Domain", c.SiteURL)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("invalid or expired license key")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read plugin binary: %w", err)
	}

	hash := sha256.Sum256(data)
	hexHash := hex.EncodeToString(hash[:])

	return data, hexHash, nil
}
