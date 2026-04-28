package main

import (
	"fmt"
	"time"

	"github.com/ez8/gocms/internal/db"
)

func main() {
	err := db.Init("data/cms.db")
	if err != nil {
		fmt.Println("DB Init:", err)
		return
	}
	rows, err := db.DB.Query("SELECT created_at FROM pages LIMIT 1")
	if err != nil {
		fmt.Println("Query:", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t time.Time
		err := rows.Scan(&t)
		if err != nil {
			fmt.Println("Scan error:", err)
			
			// Test raw string
			var s string
			rows, _ = db.DB.Query("SELECT created_at FROM pages LIMIT 1")
			rows.Next()
			rows.Scan(&s)
			fmt.Println("Raw string value:", s)
			return
		}
		fmt.Println("Success:", t)
	}
}
