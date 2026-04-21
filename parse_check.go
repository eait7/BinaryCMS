package main

import (
	"log"
	"text/template"
)

func main() {
	_, err := template.ParseFiles("/home/ez8/.gemini/antigravity/scratch/binarycms_production/themes/backend/default/page_edit.html")
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	log.Println("OK")
}
