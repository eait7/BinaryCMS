package main

import (
	"log"
	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/theme"
)

func main() {
	db.Init("/home/ez8/.gemini/antigravity/scratch/binarycms_production/cms.db")
	path := theme.GetFrontendPath("theme_page.html")
	log.Printf("Parsing path: %v", path)
	_, err := theme.ParseTemplateWithFuncs(path)
	if err != nil {
		log.Fatalf("Parse Error: %v", err)
	}
	log.Println("SUCCESS")
}
