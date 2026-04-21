package models

import (
	"database/sql"
	"log"
	"time"

	"github.com/ez8/gocms/internal/db"
)

type Comment struct {
	ID          int
	PostID      int
	ParentID    int
	AuthorName  string
	AuthorEmail string
	Content     string
	Status      string // "pending", "approved", "spam", "trash"
	CreatedAt   time.Time
	// Joined data
	PostTitle string
	Children  []Comment
}

// GetCommentsByPost returns approved comments for a post, organized as a tree.
func GetCommentsByPost(postID int) ([]Comment, error) {
	rows, err := db.DB.Query(`
		SELECT id, post_id, COALESCE(parent_id, 0), author_name, COALESCE(author_email, ''), 
			content, status, created_at
		FROM comments 
		WHERE post_id = ? AND status = 'approved'
		ORDER BY created_at ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			continue
		}
		all = append(all, *c)
	}

	return buildCommentTree(0, all), nil
}

// GetAllComments returns all comments for admin moderation (with post title).
func GetAllComments(status string) ([]Comment, error) {
	query := `
		SELECT c.id, c.post_id, COALESCE(c.parent_id, 0), c.author_name, 
			COALESCE(c.author_email, ''), c.content, c.status, c.created_at,
			COALESCE(p.title, 'Deleted Post')
		FROM comments c
		LEFT JOIN posts p ON p.id = c.post_id
	`
	args := []interface{}{}
	if status != "" {
		query += " WHERE c.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY c.created_at DESC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		err := rows.Scan(&c.ID, &c.PostID, &c.ParentID, &c.AuthorName,
			&c.AuthorEmail, &c.Content, &c.Status, &c.CreatedAt, &c.PostTitle)
		if err != nil {
			log.Println("Error scanning comment:", err)
			continue
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// GetPendingCommentCount returns the number of comments awaiting moderation.
func GetPendingCommentCount() int {
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM comments WHERE status = 'pending'").Scan(&count)
	return count
}

func CreateComment(c Comment) error {
	_, err := db.DB.Exec(`
		INSERT INTO comments (post_id, parent_id, author_name, author_email, content, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.PostID, c.ParentID, c.AuthorName, c.AuthorEmail, c.Content, c.Status, time.Now())
	return err
}

func UpdateCommentStatus(id int, status string) error {
	_, err := db.DB.Exec("UPDATE comments SET status = ? WHERE id = ?", status, id)
	return err
}

func DeleteComment(id int) error {
	// Delete children first
	db.DB.Exec("DELETE FROM comments WHERE parent_id = ?", id)
	_, err := db.DB.Exec("DELETE FROM comments WHERE id = ?", id)
	return err
}

func scanComment(scanner interface{ Scan(...interface{}) error }) (*Comment, error) {
	var c Comment
	var parentID sql.NullInt64
	var email sql.NullString
	err := scanner.Scan(&c.ID, &c.PostID, &parentID, &c.AuthorName, &email, &c.Content, &c.Status, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if parentID.Valid {
		c.ParentID = int(parentID.Int64)
	}
	c.AuthorEmail = email.String
	return &c, nil
}

func buildCommentTree(parentID int, comments []Comment) []Comment {
	var branch []Comment
	for _, c := range comments {
		if c.ParentID == parentID {
			c.Children = buildCommentTree(c.ID, comments)
			branch = append(branch, c)
		}
	}
	return branch
}
