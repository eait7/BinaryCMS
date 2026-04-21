package models

import (
	"log"
	"regexp"
	"strings"

	"github.com/ez8/gocms/internal/db"
)

type Category struct {
	ID          int
	Name        string
	Slug        string
	Description string
	ParentID    int
	PostCount   int // Computed field
}

type Tag struct {
	ID        int
	Name      string
	Slug      string
	PostCount int // Computed field
}

func generateTaxonomySlug(name string) string {
	lower := strings.ToLower(name)
	reg := regexp.MustCompile("[^a-z0-9]+")
	slug := reg.ReplaceAllString(lower, "-")
	return strings.Trim(slug, "-")
}

// --- Categories ---

func GetAllCategories() ([]Category, error) {
	rows, err := db.DB.Query(`
		SELECT c.id, c.name, c.slug, COALESCE(c.description, ''), c.parent_id,
			(SELECT COUNT(*) FROM post_categories pc WHERE pc.category_id = c.id) as post_count
		FROM categories c ORDER BY c.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ParentID, &c.PostCount); err != nil {
			log.Println("Error scanning category:", err)
			continue
		}
		cats = append(cats, c)
	}
	return cats, nil
}

func GetCategoryByID(id int) (*Category, error) {
	var c Category
	err := db.DB.QueryRow(
		"SELECT id, name, slug, COALESCE(description, ''), parent_id FROM categories WHERE id = ?", id,
	).Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ParentID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func GetCategoryBySlug(slug string) (*Category, error) {
	var c Category
	err := db.DB.QueryRow(
		"SELECT id, name, slug, COALESCE(description, ''), parent_id FROM categories WHERE slug = ?", slug,
	).Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ParentID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func CreateCategory(name, description string, parentID int) error {
	slug := generateTaxonomySlug(name)
	_, err := db.DB.Exec(
		"INSERT INTO categories (name, slug, description, parent_id) VALUES (?, ?, ?, ?)",
		name, slug, description, parentID,
	)
	return err
}

func UpdateCategory(id int, name, description string, parentID int) error {
	slug := generateTaxonomySlug(name)
	_, err := db.DB.Exec(
		"UPDATE categories SET name=?, slug=?, description=?, parent_id=? WHERE id=?",
		name, slug, description, parentID, id,
	)
	return err
}

func DeleteCategory(id int) error {
	db.DB.Exec("DELETE FROM post_categories WHERE category_id = ?", id)
	_, err := db.DB.Exec("DELETE FROM categories WHERE id = ?", id)
	return err
}

// SetPostCategories replaces all category associations for a post.
func SetPostCategories(postID int, categoryIDs []int) error {
	db.DB.Exec("DELETE FROM post_categories WHERE post_id = ?", postID)
	for _, catID := range categoryIDs {
		db.DB.Exec("INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)", postID, catID)
	}
	return nil
}

// GetPostCategories returns all categories for a given post.
func GetPostCategories(postID int) ([]Category, error) {
	rows, err := db.DB.Query(`
		SELECT c.id, c.name, c.slug, COALESCE(c.description, ''), c.parent_id
		FROM categories c
		INNER JOIN post_categories pc ON pc.category_id = c.id
		WHERE pc.post_id = ?
		ORDER BY c.name ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ParentID); err != nil {
			continue
		}
		cats = append(cats, c)
	}
	return cats, nil
}

// GetPostsByCategory returns published posts in a given category.
func GetPostsByCategory(categoryID int) ([]Post, error) {
	rows, err := db.DB.Query(`
		SELECT p.`+postColumns+` FROM posts p
		INNER JOIN post_categories pc ON pc.post_id = p.id
		WHERE pc.category_id = ? AND p.status = 'published'
		ORDER BY p.created_at DESC
	`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			continue
		}
		posts = append(posts, *p)
	}
	return posts, nil
}

// --- Tags ---

func GetAllTags() ([]Tag, error) {
	rows, err := db.DB.Query(`
		SELECT t.id, t.name, t.slug,
			(SELECT COUNT(*) FROM post_tags pt WHERE pt.tag_id = t.id) as post_count
		FROM tags t ORDER BY t.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.PostCount); err != nil {
			continue
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func CreateTag(name string) error {
	slug := generateTaxonomySlug(name)
	_, err := db.DB.Exec("INSERT OR IGNORE INTO tags (name, slug) VALUES (?, ?)", name, slug)
	return err
}

func DeleteTag(id int) error {
	db.DB.Exec("DELETE FROM post_tags WHERE tag_id = ?", id)
	_, err := db.DB.Exec("DELETE FROM tags WHERE id = ?", id)
	return err
}

// SetPostTags replaces all tag associations for a post.
func SetPostTags(postID int, tagIDs []int) error {
	db.DB.Exec("DELETE FROM post_tags WHERE post_id = ?", postID)
	for _, tagID := range tagIDs {
		db.DB.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", postID, tagID)
	}
	return nil
}

// GetPostTags returns all tags for a given post.
func GetPostTags(postID int) ([]Tag, error) {
	rows, err := db.DB.Query(`
		SELECT t.id, t.name, t.slug
		FROM tags t
		INNER JOIN post_tags pt ON pt.tag_id = t.id
		WHERE pt.post_id = ?
		ORDER BY t.name ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug); err != nil {
			continue
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// GetPostsByTag returns published posts with a given tag.
func GetPostsByTag(tagID int) ([]Post, error) {
	rows, err := db.DB.Query(`
		SELECT p.`+postColumns+` FROM posts p
		INNER JOIN post_tags pt ON pt.post_id = p.id
		WHERE pt.tag_id = ? AND p.status = 'published'
		ORDER BY p.created_at DESC
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			continue
		}
		posts = append(posts, *p)
	}
	return posts, nil
}
