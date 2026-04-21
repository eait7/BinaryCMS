package models

import (
	"database/sql"
	"log"
	"time"

	"github.com/ez8/gocms/internal/db"
)

type Page struct {
	ID              int
	Title           string
	Slug            string
	Content         string
	Status          string
	MetaTitle       string
	MetaDescription string
	FeaturedImage   string
	ShowInMenu      bool
	MenuOrder       int
	AuthorID        int
	RequiredRole    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const pageSelectColumns = `id, title, slug, content, status, meta_title, meta_description, featured_image, required_role, created_at, updated_at`

// scanPage extracts a Page from a sql.Row or sql.Rows scanner.
func scanPage(scanner interface{ Scan(...interface{}) error }) (*Page, error) {
	var p Page
	var updatedAt sql.NullTime
	var requiredRole sql.NullString
	var featuredImage sql.NullString

	err := scanner.Scan(
		&p.ID, &p.Title, &p.Slug, &p.Content, &p.Status,
		&p.MetaTitle, &p.MetaDescription, &featuredImage, &requiredRole,
		&p.CreatedAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.RequiredRole = requiredRole.String
	p.FeaturedImage = featuredImage.String
	if updatedAt.Valid {
		p.UpdatedAt = updatedAt.Time
	}
	return &p, nil
}

// GetAllPages returns all pages. If onlyPublished is true, filters to published.
func GetAllPages(onlyPublished bool) ([]Page, error) {
	query := "SELECT " + pageSelectColumns + " FROM pages"
	if onlyPublished {
		query += " WHERE status = 'published'"
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			log.Println("Error scanning page:", err)
			continue
		}
		pages = append(pages, *p)
	}
	return pages, nil
}

// GetMenuPages returns published pages marked for menu display.
func GetMenuPages() ([]Page, error) {
	rows, err := db.DB.Query("SELECT id, title, slug, menu_order FROM pages WHERE status = 'published' AND show_in_menu = 1 ORDER BY menu_order ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.Title, &p.Slug, &p.MenuOrder); err != nil {
			log.Println("Error scanning menu page:", err)
			continue
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func CreatePage(p Page) error {
	now := time.Now()
	query := `INSERT INTO pages (title, slug, content, status, show_in_menu, menu_order, author_id, meta_title, meta_description, featured_image, required_role, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.DB.Exec(query,
		p.Title, p.Slug, p.Content, p.Status, p.ShowInMenu, p.MenuOrder,
		p.AuthorID, p.MetaTitle, p.MetaDescription, p.FeaturedImage, p.RequiredRole, now, now,
	)
	return err
}

func GetPageByID(id int) (*Page, error) {
	row := db.DB.QueryRow("SELECT "+pageSelectColumns+" FROM pages WHERE id = ?", id)
	return scanPage(row)
}

func GetPageBySlug(slug string) (*Page, error) {
	row := db.DB.QueryRow(
		"SELECT id, title, slug, content, status, COALESCE(meta_title,''), COALESCE(meta_description,''), COALESCE(featured_image,''), COALESCE(required_role,''), created_at, updated_at FROM pages WHERE slug = ?",
		slug,
	)
	p, err := scanPage(row)
	if err != nil {
		return nil, err
	}

	// Also load menu fields
	var showInMenu sql.NullBool
	var menuOrder sql.NullInt64
	var authorID sql.NullInt64
	db.DB.QueryRow("SELECT show_in_menu, menu_order, author_id FROM pages WHERE id = ?", p.ID).
		Scan(&showInMenu, &menuOrder, &authorID)
	if showInMenu.Valid {
		p.ShowInMenu = showInMenu.Bool
	}
	if menuOrder.Valid {
		p.MenuOrder = int(menuOrder.Int64)
	}
	if authorID.Valid {
		p.AuthorID = int(authorID.Int64)
	}

	return p, nil
}

func UpdatePage(p Page) error {
	query := `UPDATE pages SET title=?, slug=?, content=?, status=?, show_in_menu=?, menu_order=?, meta_title=?, meta_description=?, featured_image=?, required_role=?, updated_at=? WHERE id=?`
	_, err := db.DB.Exec(query,
		p.Title, p.Slug, p.Content, p.Status, p.ShowInMenu, p.MenuOrder,
		p.MetaTitle, p.MetaDescription, p.FeaturedImage, p.RequiredRole, time.Now(), p.ID,
	)
	return err
}

func DeletePage(id int) error {
	_, err := db.DB.Exec("DELETE FROM pages WHERE id = ?", id)
	return err
}
