package models

import (
	"log"
	"time"

	"github.com/ez8/gocms/internal/db"
)

type Revision struct {
	ID        int
	PostID    int
	Title     string
	Content   string
	AuthorID  int
	CreatedAt time.Time
}

// CreateRevision saves a snapshot of the current post state before an edit.
func CreateRevision(postID int, title, content string, authorID int) error {
	_, err := db.DB.Exec(`
		INSERT INTO revisions (post_id, title, content, author_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, postID, title, content, authorID, time.Now())
	return err
}

// GetRevisionsByPost returns all revisions for a post, newest first.
func GetRevisionsByPost(postID int) ([]Revision, error) {
	rows, err := db.DB.Query(`
		SELECT id, post_id, title, content, author_id, created_at
		FROM revisions WHERE post_id = ?
		ORDER BY created_at DESC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.PostID, &r.Title, &r.Content, &r.AuthorID, &r.CreatedAt); err != nil {
			log.Println("Error scanning revision:", err)
			continue
		}
		revisions = append(revisions, r)
	}
	return revisions, nil
}

// GetRevisionByID returns a single revision.
func GetRevisionByID(id int) (*Revision, error) {
	var r Revision
	err := db.DB.QueryRow(`
		SELECT id, post_id, title, content, author_id, created_at
		FROM revisions WHERE id = ?
	`, id).Scan(&r.ID, &r.PostID, &r.Title, &r.Content, &r.AuthorID, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRevision removes a single revision.
func DeleteRevision(id int) error {
	_, err := db.DB.Exec("DELETE FROM revisions WHERE id = ?", id)
	return err
}

// PruneOldRevisions keeps only the latest N revisions per post.
func PruneOldRevisions(postID int, keepCount int) error {
	_, err := db.DB.Exec(`
		DELETE FROM revisions WHERE post_id = ? AND id NOT IN (
			SELECT id FROM revisions WHERE post_id = ? ORDER BY created_at DESC LIMIT ?
		)
	`, postID, postID, keepCount)
	return err
}
