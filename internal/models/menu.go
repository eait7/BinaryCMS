package models

import (
	"log"

	"github.com/ez8/gocms/internal/db"
)

type MenuItem struct {
	ID         int
	Label      string
	URL        string
	MenuOrder  int
	ParentID   int
	Location   string
	OpenNewTab bool
	Children   []MenuItem
}

// GetAllMenuItems returns all menu items organized as a tree.
func GetAllMenuItems() ([]MenuItem, error) {
	rows, err := db.DB.Query("SELECT id, label, url, menu_order, COALESCE(parent_id, 0), COALESCE(location, 'header'), COALESCE(open_new_tab, 0) FROM menu_items ORDER BY menu_order ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allItems []MenuItem
	for rows.Next() {
		var mi MenuItem
		if err := rows.Scan(&mi.ID, &mi.Label, &mi.URL, &mi.MenuOrder, &mi.ParentID, &mi.Location, &mi.OpenNewTab); err != nil {
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

func CreateMenuItem(label, url string, order int, location string, openNewTab bool) error {
	if location == "" {
		location = "header"
	}
	_, err := db.DB.Exec("INSERT INTO menu_items (label, url, menu_order, location, open_new_tab) VALUES (?, ?, ?, ?, ?)", label, url, order, location, openNewTab)
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

// UpdateMenuItem updates a menu item's label, URL, and open_new_tab setting.
func UpdateMenuItem(id int, label, url string, openNewTab bool) error {
	_, err := db.DB.Exec("UPDATE menu_items SET label = ?, url = ?, open_new_tab = ? WHERE id = ?", label, url, openNewTab, id)
	return err
}
