package main

import (
	"log"
	"github.com/ez8/gocms/internal/theme"
)

func main() {
	_, err := theme.ParseTemplateWithFuncs("themes/frontend/binarycms_quantum_light/theme_page.html")
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	log.Println("SUCCESS")
}
