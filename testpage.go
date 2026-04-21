package main

import (
	"log"
	"os"
	"strings"
	"text/template"
	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/models"
)

func main() {
	err := db.Init("/home/ez8/.gemini/antigravity/scratch/binarycms_production/data/cms.db")
	if err != nil {
		log.Fatalf("DB err: %v", err)
	}

	page, err := models.GetPageByID(20)
	if err != nil {
		log.Fatalf("Page err: %v", err)
	}

	isDemo := strings.HasPrefix(page.Slug, "demo-")

	t, err := template.ParseFiles("/home/ez8/.gemini/antigravity/scratch/binarycms_production/themes/backend/default/page_edit.html")
	if err != nil {
		log.Fatalf("Parse err: %v", err)
	}

	err = t.Execute(os.Stdout, map[string]interface{}{
		"Page": page,
		"IsDemo": isDemo,
	})
	if err != nil {
		log.Fatalf("Exec err: %v", err)
	}
	log.Println("SUCCESS")
}
