package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/models"
)

func main() {
	err := db.Init("cms.db")
	if err != nil {
		log.Fatalf("Failed to initialize db: %v", err)
	}

	dir := "/tmp/tabler_html"
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	// Basic Regexp to extract just the body logic without sidebars
	// Looking for <div class="page-body">...</div>
	// This captures everything from <div class="page-wrapper"> down or just the body
	// Actually Tabler full pages often have a page-header inside the wrapper.
	// We'll capture everything after "<div class=\"page-wrapper\">"
	// up until "<footer class=\"footer"

	reWrapper := regexp.MustCompile(`(?s)<div class="page-wrapper">\s*(.*?)\s*<footer class="footer`)

	count := 0

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".html") {
			continue
		}

		path := filepath.Join(dir, f.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Failed to read %s: %v", f.Name(), err)
			continue
		}

		strContent := string(content)

		matches := reWrapper.FindStringSubmatch(strContent)
		if len(matches) < 2 {
			log.Printf("Skipping %s: could not find page-wrapper", f.Name())
			continue
		}

		bodyContent := matches[1]

		baseName := strings.TrimSuffix(f.Name(), ".html")
		slug := "demo-" + baseName

		// Check if already exists
		existing, _ := models.GetPageBySlug(slug)
		if existing != nil && existing.ID > 0 {
			// Update
			existing.Content = bodyContent
			models.UpdatePage(*existing)
		} else {
			// Insert
			page := models.Page{
				Title:           "Demo: " + strings.Title(strings.ReplaceAll(baseName, "-", " ")),
				Slug:            slug,
				Content:         bodyContent,
				Status:          "published",
				MetaTitle:       "Tabler Demo",
				MetaDescription: "Tabler UI Demo",
				ShowInMenu:      false, // Hidden explicitly to prevent navbar clutter
				MenuOrder:       99,
			}
			models.CreatePage(page)
		}

		count++
		fmt.Printf("Imported: %s\n", slug)
	}

	fmt.Printf("\nCompleted! Successfully imported %d demo HTML layouts natively into GoCMS!\n", count)
}
