package main

import (
	"log"
	"os"
	"text/template"
)

func main() {
	t, err := template.ParseFiles("/home/ez8/.gemini/antigravity/scratch/binarycms_production/themes/backend/default/dashboard.html")
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}

	err = t.Execute(os.Stdout, map[string]interface{}{
		"PostCount":       0,
		"PageCount":       0,
		"PendingComments": 0,
		"SubscriberCount": 0,
	})
	if err != nil {
		log.Fatalf("Exec error: %v", err)
	}
	log.Println("OK")
}
