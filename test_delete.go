package main

import (
	"fmt"

	"github.com/ez8/gocms/internal/db"
)

func main() {
	db.Init("cms.db")
	_, err := db.DB.Exec("DELETE FROM pages WHERE slug='home'")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Deleted home")
	}
}
