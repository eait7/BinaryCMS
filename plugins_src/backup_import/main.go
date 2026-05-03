package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ez8/gocms/pkg/plugin"
	_ "modernc.org/sqlite"
)

type BackupImportPlugin struct {
	db *sql.DB
}

type Page struct {
	ID              int       `json:"id"`
	Title           string    `json:"title"`
	Slug            string    `json:"slug"`
	Content         string    `json:"content"`
	Status          string    `json:"status"`
	MetaTitle       string    `json:"meta_title"`
	MetaDescription string    `json:"meta_description"`
	FeaturedImage   string    `json:"featured_image"`
	ShowInMenu      bool      `json:"show_in_menu"`
	MenuOrder       int       `json:"menu_order"`
	AuthorID        int       `json:"author_id"`
	RequiredRole    string    `json:"required_role"`
	FullWidth       bool      `json:"full_width"`
	HideTitle       bool      `json:"hide_title"`
	HideBreadcrumbs bool      `json:"hide_breadcrumbs"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Post struct {
	ID              int       `json:"id"`
	Title           string    `json:"title"`
	Slug            string    `json:"slug"`
	Excerpt         string    `json:"excerpt"`
	Content         string    `json:"content"`
	Status          string    `json:"status"`
	MetaTitle       string    `json:"meta_title"`
	MetaDescription string    `json:"meta_description"`
	FeaturedImage   string    `json:"featured_image"`
	AuthorID        int       `json:"author_id"`
	PublishedAt     time.Time `json:"published_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type BackupData struct {
	Pages []Page `json:"pages"`
	Posts []Post `json:"posts"`
}

func (p *BackupImportPlugin) HookBeforeFrontPageRender(content string) string { return content }
func (p *BackupImportPlugin) PluginName() string                              { return "Backup & Import" }

func (p *BackupImportPlugin) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{
		{
			Label: "Backup & Restore",
			URL:   "/admin/plugin/backup_import/ui",
			Icon:  "database-export",
		},
	}
}

func (p *BackupImportPlugin) HookAdminRoute(route string) string {
	u, _ := url.Parse(route)
	path := u.Path

	if path == "/admin/plugin/backup_import/ui" {
		return p.renderUI()
	}

	if path == "/api/plugin/backup_import/export" {
		return string(p.generateBackup())
	}

	if path == "/admin/plugin/backup_import/import" {
		if u.Query().Get("__file_backup_file") != "" {
			return p.handleImport(u.Query().Get("__file_backup_file"))
		}
		return "<div class='alert alert-warning'>No file provided.</div>"
	}

	return "Invalid route."
}

func (p *BackupImportPlugin) HookDashboardWidget() string           { return "" }
func (p *BackupImportPlugin) HookAdminTopRightWidget() string       { return "" }
func (p *BackupImportPlugin) HookUserProfileTab(userID int) string  { return "" }
func (p *BackupImportPlugin) HookUserAccountCard(userID int) string { return "" }
func (p *BackupImportPlugin) HookUserRegistered(userID int) string  { return "" }

func (p *BackupImportPlugin) renderUI() string {
	return `
	<div class="card shadow-sm mb-4">
		<div class="card-header bg-primary text-white">
			<h3 class="card-title mb-0">Export Backup</h3>
		</div>
		<div class="card-body">
			<p>Download a full backup of all Pages, Posts, and Plugin binaries.</p>
			<a href="/api/plugin/backup_import/export" class="btn btn-success" download="binarycms_backup.zip">
				<svg xmlns="http://www.w3.org/2000/svg" class="icon" width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none" stroke-linecap="round" stroke-linejoin="round"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" /><path d="M7 11l5 5l5 -5" /><path d="M12 4l0 12" /></svg>
				Download Backup
			</a>
		</div>
	</div>

	<div class="card shadow-sm">
		<div class="card-header bg-dark text-white">
			<h3 class="card-title mb-0">Import Backup</h3>
		</div>
		<div class="card-body">
			<p>Upload a <code>binarycms_backup.zip</code> file to restore Pages, Posts, and Plugins. Existing content with matching slugs will be <strong>replaced</strong>.</p>
			<form action="/admin/plugin/backup_import/import" method="POST" enctype="multipart/form-data" class="d-flex align-items-center gap-3">
				<input type="file" name="backup_file" class="form-control w-auto" accept=".zip" required>
				<button type="submit" class="btn btn-danger">
					<svg xmlns="http://www.w3.org/2000/svg" class="icon" width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none" stroke-linecap="round" stroke-linejoin="round"><path stroke="none" d="M0 0h24v24H0z" fill="none"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" /><path d="M7 9l5 -5l5 5" /><path d="M12 4l0 12" /></svg>
					Upload & Restore
				</button>
			</form>
		</div>
	</div>
	`
}

func (p *BackupImportPlugin) generateBackup() []byte {
	var bd BackupData

	// Fetch Pages
	rows, err := p.db.Query("SELECT id, title, slug, content, status, show_in_menu, menu_order, author_id, required_role, full_width, hide_title, hide_breadcrumbs, meta_title, meta_description, featured_image, created_at, updated_at FROM pages")
	if err == nil {
		for rows.Next() {
			var pg Page
			var created, updated sql.NullTime
			rows.Scan(&pg.ID, &pg.Title, &pg.Slug, &pg.Content, &pg.Status, &pg.ShowInMenu, &pg.MenuOrder, &pg.AuthorID, &pg.RequiredRole, &pg.FullWidth, &pg.HideTitle, &pg.HideBreadcrumbs, &pg.MetaTitle, &pg.MetaDescription, &pg.FeaturedImage, &created, &updated)
			if created.Valid { pg.CreatedAt = created.Time }
			if updated.Valid { pg.UpdatedAt = updated.Time }
			bd.Pages = append(bd.Pages, pg)
		}
		rows.Close()
	}

	// Fetch Posts
	rowsP, err := p.db.Query("SELECT id, title, slug, excerpt, content, status, meta_title, meta_description, featured_image, author_id, created_at, updated_at, published_at FROM posts")
	if err == nil {
		for rowsP.Next() {
			var pst Post
			var created, updated, pub sql.NullTime
			rowsP.Scan(&pst.ID, &pst.Title, &pst.Slug, &pst.Excerpt, &pst.Content, &pst.Status, &pst.MetaTitle, &pst.MetaDescription, &pst.FeaturedImage, &pst.AuthorID, &created, &updated, &pub)
			if created.Valid { pst.CreatedAt = created.Time }
			if updated.Valid { pst.UpdatedAt = updated.Time }
			if pub.Valid { pst.PublishedAt = pub.Time }
			bd.Posts = append(bd.Posts, pst)
		}
		rowsP.Close()
	}

	jsonBytes, _ := json.MarshalIndent(bd, "", "  ")
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	// Write JSON
	f, err := zw.Create("backup.json")
	if err == nil {
		f.Write(jsonBytes)
	}

	// Write Plugins
	files, _ := os.ReadDir("plugins")
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("plugins", file.Name()))
		if err == nil {
			pf, err := zw.Create("plugins/" + file.Name())
			if err == nil {
				pf.Write(data)
			}
		}
	}

	zw.Close()
	return buf.Bytes()
}

func (p *BackupImportPlugin) handleImport(tmpFilePath string) string {
	r, err := zip.OpenReader(tmpFilePath)
	if err != nil {
		return fmt.Sprintf("<div class='alert alert-danger'>Failed to open ZIP file: %v</div>", err)
	}
	defer r.Close()

	var bd BackupData
	for _, f := range r.File {
		if f.Name == "backup.json" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(rc)
			json.Unmarshal(data, &bd)
			rc.Close()
		} else if strings.HasPrefix(f.Name, "plugins/") {
			name := filepath.Base(f.Name)
			if name != "" && name != "." && name != "/" {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(rc)
				rc.Close()
				os.MkdirAll("plugins", 0755)
				os.WriteFile(filepath.Join("plugins", name), data, 0755)
			}
		}
	}

	// Restore pages
	pagesRestored := 0
	for _, page := range bd.Pages {
		var existingID int
		err := p.db.QueryRow("SELECT id FROM pages WHERE slug = ?", page.Slug).Scan(&existingID)
		if err == nil && existingID > 0 {
			p.db.Exec(`UPDATE pages SET title=?, content=?, status=?, show_in_menu=?, menu_order=?, author_id=?, required_role=?, full_width=?, hide_title=?, hide_breadcrumbs=?, meta_title=?, meta_description=?, featured_image=?, updated_at=? WHERE id=?`,
				page.Title, page.Content, page.Status, page.ShowInMenu, page.MenuOrder, page.AuthorID, page.RequiredRole, page.FullWidth, page.HideTitle, page.HideBreadcrumbs, page.MetaTitle, page.MetaDescription, page.FeaturedImage, time.Now(), existingID)
		} else {
			p.db.Exec(`INSERT INTO pages (title, slug, content, status, show_in_menu, menu_order, author_id, required_role, full_width, hide_title, hide_breadcrumbs, meta_title, meta_description, featured_image, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				page.Title, page.Slug, page.Content, page.Status, page.ShowInMenu, page.MenuOrder, page.AuthorID, page.RequiredRole, page.FullWidth, page.HideTitle, page.HideBreadcrumbs, page.MetaTitle, page.MetaDescription, page.FeaturedImage, page.CreatedAt, time.Now())
		}
		pagesRestored++
	}

	// Restore posts
	postsRestored := 0
	for _, post := range bd.Posts {
		var existingID int
		err := p.db.QueryRow("SELECT id FROM posts WHERE slug = ?", post.Slug).Scan(&existingID)
		if err == nil && existingID > 0 {
			p.db.Exec(`UPDATE posts SET title=?, excerpt=?, content=?, status=?, meta_title=?, meta_description=?, featured_image=?, author_id=?, updated_at=?, published_at=? WHERE id=?`,
				post.Title, post.Excerpt, post.Content, post.Status, post.MetaTitle, post.MetaDescription, post.FeaturedImage, post.AuthorID, time.Now(), post.PublishedAt, existingID)
		} else {
			p.db.Exec(`INSERT INTO posts (title, slug, excerpt, content, status, meta_title, meta_description, featured_image, author_id, created_at, updated_at, published_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				post.Title, post.Slug, post.Excerpt, post.Content, post.Status, post.MetaTitle, post.MetaDescription, post.FeaturedImage, post.AuthorID, post.CreatedAt, time.Now(), post.PublishedAt)
		}
		postsRestored++
	}

	return fmt.Sprintf(`
	<div class='alert alert-success'>
		<h4>Import Successful</h4>
		<p>Restored %d Pages and %d Posts.</p>
		<p>Plugin binaries were extracted. <strong>You may need to restart the CMS container</strong> to fully load any newly imported plugins.</p>
		<a href="/admin/plugin/backup_import/ui" class="btn btn-primary">Go Back</a>
	</div>`, pagesRestored, postsRestored)
}

func main() {
	dsn := "data/cms.db"
	if _, err := os.Stat("/app/data/cms.db"); err == nil {
		dsn = "/app/data/cms.db"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatal("failed to open database: ", err)
	}

	plugin.ServePlugin(&BackupImportPlugin{db: db})
}
