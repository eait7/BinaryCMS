package models

import (
	"log"

	"github.com/ez8/gocms/internal/db"
)

func GetSetting(key string) string {
	var value string
	err := db.DB.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func SetSetting(key, value string) error {
	_, err := db.DB.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?",
		key, value, value,
	)
	return err
}

// GetAllSettingsMap returns all settings as a key-value map.
func GetAllSettingsMap() map[string]string {
	m := make(map[string]string)
	rows, err := db.DB.Query("SELECT key, value FROM settings")
	if err != nil {
		log.Println("Error reading settings:", err)
		return m
	}
	defer rows.Close()

	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err == nil {
			m[key] = val
		}
	}
	return m
}
