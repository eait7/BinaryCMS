package main

import (
	"log"
	"github.com/ez8/gocms/internal/db"
)

func main() {
	err := db.Init("/home/ez8/.gemini/antigravity/scratch/binarycms_production/data/cms.db")
	if err != nil {
		log.Fatal(err)
	}

	rows, _ := db.DB.Query("SELECT title, source_url, widget_type FROM widgets WHERE widget_type = 'plugin_hook';")
	for rows.Next() {
		var t, s, w string
		rows.Scan(&t, &s, &w)
		log.Printf("%s | %s | %s", t, s, w)
	}
}
