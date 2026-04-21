package models

import (
	"database/sql"
	"log"
	"time"

	"github.com/ez8/gocms/internal/db"
)

type Post struct {
	ID              int
	Title           string
	Slug            string
	Excerpt         string
	Content         string
	Status          string
	MetaTitle       string
	MetaDescription string
	FeaturedImage   string
	AuthorID        int
	PublishedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// Joined data (not stored directly)
	Categories []Category
	Tags       []Tag
}

const postColumns = `id, title, slug, excerpt, content, status, meta_title, meta_description, featured_image, author_id, created_at, updated_at`

// scanPost extracts a Post from a sql.Row or sql.Rows scanner.
func scanPost(scanner interface{ Scan(...interface{}) error }) (*Post, error) {
	var p Post
	var slug, excerpt, status, metaTitle, metaDesc, featuredImage sql.NullString
	var authorID sql.NullInt64
	var updatedAt sql.NullTime

	err := scanner.Scan(
		&p.ID, &p.Title, &slug, &excerpt, &p.Content,
		&status, &metaTitle, &metaDesc, &featuredImage, &authorID,
		&p.CreatedAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.Slug = slug.String
	p.Excerpt = excerpt.String
	p.Status = status.String
	if p.Status == "" {
		p.Status = "draft"
	}
	p.MetaTitle = metaTitle.String
	p.MetaDescription = metaDesc.String
	p.FeaturedImage = featuredImage.String
	if authorID.Valid {
		p.AuthorID = int(authorID.Int64)
	}
	if updatedAt.Valid {
		p.UpdatedAt = updatedAt.Time
	}
	return &p, nil
}

// GetAllPosts returns posts ordered by creation date. If onlyPublished is true,
// only returns posts with status "published".
func GetAllPosts(onlyPublished bool) ([]Post, error) {
	query := "SELECT " + postColumns + " FROM posts"
	if onlyPublished {
		query += " WHERE status = 'published'"
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			log.Println("Error scanning post:", err)
			continue
		}
		posts = append(posts, *p)
	}
	return posts, nil
}

// GetPaginatedPosts returns a page of posts with total count for pagination.
func GetPaginatedPosts(page, perPage int, onlyPublished bool) ([]Post, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	offset := (page - 1) * perPage

	// Count total
	countQuery := "SELECT COUNT(*) FROM posts"
	if onlyPublished {
		countQuery += " WHERE status = 'published'"
	}
	var total int
	if err := db.DB.QueryRow(countQuery).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch page
	query := "SELECT " + postColumns + " FROM posts"
	if onlyPublished {
		query += " WHERE status = 'published'"
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"

	rows, err := db.DB.Query(query, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			log.Println("Error scanning post:", err)
			continue
		}
		posts = append(posts, *p)
	}
	return posts, total, nil
}

func CreatePost(post Post) (int64, error) {
	now := time.Now()
	query := `INSERT INTO posts (title, slug, excerpt, content, status, meta_title, meta_description, featured_image, author_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := db.DB.Exec(query,
		post.Title, post.Slug, post.Excerpt, post.Content, post.Status,
		post.MetaTitle, post.MetaDescription, post.FeaturedImage, post.AuthorID, now, now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func GetPostByID(id int) (*Post, error) {
	row := db.DB.QueryRow("SELECT "+postColumns+" FROM posts WHERE id = ?", id)
	return scanPost(row)
}

func GetPostBySlug(slug string) (*Post, error) {
	row := db.DB.QueryRow("SELECT "+postColumns+" FROM posts WHERE slug = ?", slug)
	return scanPost(row)
}

func UpdatePost(post Post) error {
	query := `UPDATE posts SET title=?, slug=?, excerpt=?, content=?, status=?, meta_title=?, meta_description=?, featured_image=?, updated_at=? WHERE id=?`
	_, err := db.DB.Exec(query,
		post.Title, post.Slug, post.Excerpt, post.Content, post.Status,
		post.MetaTitle, post.MetaDescription, post.FeaturedImage, time.Now(), post.ID,
	)
	return err
}

func DeletePost(id int) error {
	// Clean up related data
	db.DB.Exec("DELETE FROM post_categories WHERE post_id = ?", id)
	db.DB.Exec("DELETE FROM post_tags WHERE post_id = ?", id)
	db.DB.Exec("DELETE FROM comments WHERE post_id = ?", id)
	db.DB.Exec("DELETE FROM revisions WHERE post_id = ?", id)
	_, err := db.DB.Exec("DELETE FROM posts WHERE id = ?", id)
	return err
}

// SearchPosts searches posts using FTS5 full-text search.
func SearchPosts(query string) ([]Post, error) {
	rows, err := db.DB.Query(
		`SELECT p.`+postColumns+` FROM posts p
		 INNER JOIN posts_fts ON posts_fts.rowid = p.id
		 WHERE posts_fts MATCH ?
		 AND p.status = 'published'
		 ORDER BY rank
		 LIMIT 50`, query)
	if err != nil {
		// Fallback to LIKE search if FTS5 not available
		return searchPostsFallback(query)
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

// searchPostsFallback uses LIKE queries when FTS5 is unavailable.
func searchPostsFallback(query string) ([]Post, error) {
	like := "%" + query + "%"
	rows, err := db.DB.Query(
		"SELECT "+postColumns+" FROM posts WHERE status = 'published' AND (title LIKE ? OR content LIKE ?) ORDER BY created_at DESC LIMIT 50",
		like, like,
	)
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

// PublishScheduledPosts transitions scheduled posts to published status.
func PublishScheduledPosts() (int64, error) {
	result, err := db.DB.Exec(
		`UPDATE posts SET status='published' WHERE status='scheduled' AND published_at <= ?`,
		time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
