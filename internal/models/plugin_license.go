package models

import (
	"time"

	"github.com/ez8/gocms/internal/db"
)

// PluginLicense stores a validated license key for a marketplace plugin.
type PluginLicense struct {
	ID          int
	Slug        string
	LicenseKey  string
	Domain      string
	ActivatedAt time.Time
	ExpiresAt   *time.Time
	Status      string // "active", "expired", "revoked"
}

// CreatePluginLicensesTable creates the plugin_licenses table if it doesn't exist.
func CreatePluginLicensesTable() {
	db.DB.Exec(`CREATE TABLE IF NOT EXISTS plugin_licenses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL UNIQUE,
		license_key TEXT NOT NULL,
		domain TEXT NOT NULL,
		activated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME,
		status TEXT DEFAULT 'active'
	)`)
}

// SavePluginLicense stores a validated license key for a plugin.
func SavePluginLicense(slug, key, domain string) error {
	_, err := db.DB.Exec(
		`INSERT INTO plugin_licenses (slug, license_key, domain, activated_at, status)
		 VALUES (?, ?, ?, ?, 'active')
		 ON CONFLICT(slug) DO UPDATE SET license_key = ?, domain = ?, activated_at = ?, status = 'active'`,
		slug, key, domain, time.Now(), key, domain, time.Now(),
	)
	return err
}

// GetPluginLicense returns the license for a specific plugin slug, or nil if not licensed.
func GetPluginLicense(slug string) *PluginLicense {
	var lic PluginLicense
	var activatedAt string
	var expiresAt *string

	err := db.DB.QueryRow(
		"SELECT id, slug, license_key, domain, activated_at, expires_at, status FROM plugin_licenses WHERE slug = ?",
		slug,
	).Scan(&lic.ID, &lic.Slug, &lic.LicenseKey, &lic.Domain, &activatedAt, &expiresAt, &lic.Status)

	if err != nil {
		return nil
	}

	lic.ActivatedAt, _ = time.Parse("2006-01-02 15:04:05", activatedAt)
	if expiresAt != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *expiresAt)
		lic.ExpiresAt = &t
	}

	return &lic
}

// GetAllPluginLicenses returns all active plugin licenses.
func GetAllPluginLicenses() []PluginLicense {
	var licenses []PluginLicense

	rows, err := db.DB.Query("SELECT id, slug, license_key, domain, activated_at, status FROM plugin_licenses")
	if err != nil {
		return licenses
	}
	defer rows.Close()

	for rows.Next() {
		var lic PluginLicense
		var activatedAt string
		if err := rows.Scan(&lic.ID, &lic.Slug, &lic.LicenseKey, &lic.Domain, &activatedAt, &lic.Status); err == nil {
			lic.ActivatedAt, _ = time.Parse("2006-01-02 15:04:05", activatedAt)
			licenses = append(licenses, lic)
		}
	}

	return licenses
}

// RevokePluginLicense removes a plugin license.
func RevokePluginLicense(slug string) error {
	_, err := db.DB.Exec("DELETE FROM plugin_licenses WHERE slug = ?", slug)
	return err
}

// GetPluginLicenseMap returns a map of slug -> license_key for quick lookup.
func GetPluginLicenseMap() map[string]string {
	m := make(map[string]string)
	rows, err := db.DB.Query("SELECT slug, license_key FROM plugin_licenses WHERE status = 'active'")
	if err != nil {
		return m
	}
	defer rows.Close()

	for rows.Next() {
		var slug, key string
		if err := rows.Scan(&slug, &key); err == nil {
			m[slug] = key
		}
	}
	return m
}
