package models

import (
	"log"

	"github.com/ez8/gocms/internal/db"
)

type MenuItem struct {
	ID        int
	Label     string
	URL       string
	MenuOrder int
	ParentID  int
	Location  string
	Children  []MenuItem
}

// GetAllMenuItems returns all menu items organized as a tree.
func GetAllMenuItems() ([]MenuItem, error) {
	rows, err := db.DB.Query("SELECT id, label, url, menu_order, COALESCE(parent_id, 0), COALESCE(location, 'header') FROM menu_items ORDER BY menu_order ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allItems []MenuItem
	for rows.Next() {
		var mi MenuItem
		if err := rows.Scan(&mi.ID, &mi.Label, &mi.URL, &mi.MenuOrder, &mi.ParentID, &mi.Location); err != nil {
			log.Println("Error scanning menu item:", err)
			continue
		}
		allItems = append(allItems, mi)
	}

	return buildMenuTree(0, allItems), nil
}

// buildMenuTree recursively assembles a parent-child hierarchy.
func buildMenuTree(parentID int, items []MenuItem) []MenuItem {
	var branch []MenuItem
	for _, item := range items {
		if item.ParentID == parentID {
			children := buildMenuTree(item.ID, items)
			if len(children) > 0 {
				item.Children = children
			}
			branch = append(branch, item)
		}
	}
	return branch
}

func CreateMenuItem(label, url string, order int, location string) error {
	if location == "" {
		location = "header"
	}
	_, err := db.DB.Exec("INSERT INTO menu_items (label, url, menu_order, location) VALUES (?, ?, ?, ?)", label, url, order, location)
	return err
}

// DeleteMenuItem removes a menu item and all its children.
func DeleteMenuItem(id int) error {
	db.DB.Exec("DELETE FROM menu_items WHERE parent_id = ?", id)
	_, err := db.DB.Exec("DELETE FROM menu_items WHERE id = ?", id)
	return err
}

func UpdateMenuItemOrder(id int, parentID int, order int, location string) error {
	if location == "" {
		location = "header"
	}
	_, err := db.DB.Exec("UPDATE menu_items SET menu_order = ?, parent_id = ?, location = ? WHERE id = ?", order, parentID, location, id)
	return err
}
