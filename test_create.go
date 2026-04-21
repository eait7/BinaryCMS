package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/models"
)

func main() {
	err := db.Init("cms.db")
	if err != nil {
		fmt.Println("DB Init:", err)
		return
	}

	frontend := "default"
	jsonPath := filepath.Join("themes", "frontend", frontend, "pages.json")
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Println("ReadFile error:", err)
		return
	}
	var stubs []models.Page
	err = json.Unmarshal(b, &stubs)
	if err != nil {
		fmt.Println("Unmarshal error:", err)
		return
	}
	for _, stub := range stubs {
		stub.Status = "published"
		err := models.CreatePage(stub)
		if err != nil {
			fmt.Printf("CreatePage error for %s: %v\n", stub.Slug, err)
		} else {
			fmt.Printf("Created page %s\n", stub.Slug)
		}
	}
}
