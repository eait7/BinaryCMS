package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/models"
)

func main() {
	err := db.Init("cms.db")
	if err != nil {
		panic(err)
	}

	b, err := ioutil.ReadFile("/home/ez8/gocms_local/gocms/themes/frontend/binarycms_quantum_light/theme_index.html")
	if err != nil {
		panic(err)
	}
	content := string(b)

	startStr := "<!-- ── HERO ──────────────────────────────────────────────── -->"
	endStr := "<!-- ── FOOTER ─────────────────────────────────────────────── -->"
	startIdx := strings.Index(content, startStr)
	endIdx := strings.Index(content, endStr)
	
	if startIdx == -1 || endIdx == -1 {
		panic("Cannot find markers")
	}
	
	extracted := content[startIdx:endIdx]

	re := regexp.MustCompile(`\{\{\s*if\s+[^}]+\s*\}\}\{\{\s*[^}]+\s*\}\}\{\{\s*else\s*\}\}(.*?)\{\{\s*end\s*\}\}`)
	extracted = re.ReplaceAllString(extracted, "$1")
    
	re3 := regexp.MustCompile(`\{\{\s*[^}]+\s*\}\}`)
	extracted = re3.ReplaceAllString(extracted, "")

	// Update existing page
	page, err := models.GetPageBySlug("home")
	if err != nil {
		fmt.Println("GetPageBySlug err:", err)
		return
	}
	page.Content = extracted
	err = models.UpdatePage(*page)
	if err != nil {
		fmt.Println("Update error:", err)
	} else {
		fmt.Println("Updated Home page successfully with full template")
	}
}
