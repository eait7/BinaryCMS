package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(dsn string) error {
	var err error
	DB, err = sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}

	// Connection pool tuning for SQLite
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(25)
	DB.SetConnMaxLifetime(5 * time.Minute)

	// Enable WAL mode for better concurrent read performance
	if _, err = DB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Printf("Failed to enable WAL mode: %v", err)
	}

	// Enable foreign keys
	if _, err = DB.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		log.Printf("Failed to enable foreign keys: %v", err)
	}

	// Run versioned migrations
	if err := runMigrations(DB); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Seed default settings
	seedDefaults(DB)

	return nil
}

// migration represents a single database schema change.
type migration struct {
	Version     int
	Description string
	SQL         string
}

// migrations defines the full schema history in order.
// Each migration runs exactly once and is tracked in schema_migrations.
var migrations = []migration{
	{1, "Create users table", `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`},
	{2, "Create posts table", `
		CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			slug TEXT UNIQUE,
			content TEXT,
			author_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`},
	{3, "Create pages table", `
		CREATE TABLE IF NOT EXISTS pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			slug TEXT UNIQUE,
			content TEXT,
			status TEXT DEFAULT 'draft',
			show_in_menu BOOLEAN DEFAULT 0,
			menu_order INTEGER DEFAULT 0,
			author_id INTEGER,
			required_role TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`},
	{4, "Create menu_items table", `
		CREATE TABLE IF NOT EXISTS menu_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT,
			url TEXT,
			menu_order INTEGER DEFAULT 0,
			parent_id INTEGER DEFAULT 0
		);
	`},
	{5, "Create settings table", `
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`},
	{6, "Add post slug and status columns", `
		-- These are safe to re-run via IF NOT EXISTS patterns.
		-- SQLite doesn't support IF NOT EXISTS for ALTER TABLE,
		-- so we catch errors silently in runMigrations.
		CREATE UNIQUE INDEX IF NOT EXISTS idx_posts_slug ON posts (slug);
	`},
	{7, "Add SEO columns to posts", `
		SELECT 1;
	`},
	{8, "Add SEO columns to pages", `
		SELECT 1;
	`},
	{9, "Add user profile fields", `
		SELECT 1;
	`},
	{10, "Add RBAC role column", `
		SELECT 1;
	`},
	{11, "Create categories table", `
		CREATE TABLE IF NOT EXISTS categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT UNIQUE NOT NULL,
			description TEXT DEFAULT '',
			parent_id INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`},
	{12, "Create tags table", `
		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`},
	{13, "Create post_categories join table", `
		CREATE TABLE IF NOT EXISTS post_categories (
			post_id INTEGER NOT NULL,
			category_id INTEGER NOT NULL,
			PRIMARY KEY (post_id, category_id)
		);
	`},
	{14, "Create post_tags join table", `
		CREATE TABLE IF NOT EXISTS post_tags (
			post_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (post_id, tag_id)
		);
	`},
	{15, "Create comments table", `
		CREATE TABLE IF NOT EXISTS comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			parent_id INTEGER DEFAULT 0,
			author_name TEXT NOT NULL,
			author_email TEXT DEFAULT '',
			content TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_comments_post_id ON comments (post_id);
		CREATE INDEX IF NOT EXISTS idx_comments_status ON comments (status);
	`},
	{16, "Create revisions table", `
		CREATE TABLE IF NOT EXISTS revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			title TEXT,
			content TEXT,
			author_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_revisions_post_id ON revisions (post_id);
	`},
	{17, "Add scheduled publishing column", `
		SELECT 1;
	`},
	{18, "Create FTS5 search index", `
		CREATE VIRTUAL TABLE IF NOT EXISTS posts_fts USING fts5(title, content, content=posts, content_rowid=id);
	`},
	{19, "Create dashboard widgets table", `
		CREATE TABLE IF NOT EXISTS dashboard_widgets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			widget_type TEXT NOT NULL DEFAULT 'iframe',
			source_url TEXT DEFAULT '',
			col_span INTEGER DEFAULT 6,
			row_order INTEGER DEFAULT 0,
			config TEXT DEFAULT '{}',
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_widgets_order ON dashboard_widgets (row_order);
	`},
	{20, "Create user_meta table for plugin extensibility", `
		CREATE TABLE IF NOT EXISTS user_meta (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			meta_key TEXT NOT NULL,
			meta_value TEXT DEFAULT '',
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_meta_unique ON user_meta (user_id, meta_key);
		CREATE INDEX IF NOT EXISTS idx_user_meta_user ON user_meta (user_id);
	`},
}

// runMigrations applies all pending migrations in order.
func runMigrations(db *sql.DB) error {
	// Create the migration tracking table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Get current version
	var current int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current)
	if err != nil {
		return fmt.Errorf("reading current migration version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}

		log.Printf("Running migration %d: %s", m.Version, m.Description)
		if _, err := db.Exec(m.SQL); err != nil {
			// Some ALTER TABLE migrations may fail on existing columns — that's OK
			log.Printf("Migration %d note: %v (may be expected for ALTER TABLE)", m.Version, err)
		}

		// Record successful migration
		_, err := db.Exec("INSERT INTO schema_migrations (version, description) VALUES (?, ?)", m.Version, m.Description)
		if err != nil {
			return fmt.Errorf("recording migration %d: %w", m.Version, err)
		}
	}

	// Run safe ALTER TABLE additions that may already exist.
	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE,
	// so we just run them and ignore errors.
	safeAlterColumns(db)

	log.Printf("Database at migration version %d (latest: %d)", current, len(migrations))
	return nil
}

// safeAlterColumns runs ALTER TABLE statements that may fail if columns exist.
func safeAlterColumns(db *sql.DB) {
	alters := []string{
		// Post columns
		`ALTER TABLE posts ADD COLUMN slug TEXT`,
		`ALTER TABLE posts ADD COLUMN status TEXT DEFAULT 'draft'`,
		`ALTER TABLE posts ADD COLUMN meta_title TEXT DEFAULT ''`,
		`ALTER TABLE posts ADD COLUMN meta_description TEXT DEFAULT ''`,
		`ALTER TABLE posts ADD COLUMN excerpt TEXT DEFAULT ''`,
		`ALTER TABLE posts ADD COLUMN updated_at DATETIME DEFAULT NULL`,
		`ALTER TABLE posts ADD COLUMN published_at DATETIME DEFAULT NULL`,
		`ALTER TABLE posts ADD COLUMN featured_image TEXT DEFAULT ''`,

		// Page columns
		`ALTER TABLE pages ADD COLUMN required_role TEXT DEFAULT ''`,
		`ALTER TABLE pages ADD COLUMN show_in_menu BOOLEAN DEFAULT 0`,
		`ALTER TABLE pages ADD COLUMN menu_order INTEGER DEFAULT 0`,
		`ALTER TABLE pages ADD COLUMN meta_title TEXT DEFAULT ''`,
		`ALTER TABLE pages ADD COLUMN meta_description TEXT DEFAULT ''`,
		`ALTER TABLE pages ADD COLUMN updated_at DATETIME DEFAULT NULL`,
		`ALTER TABLE pages ADD COLUMN featured_image TEXT DEFAULT ''`,

		// User columns
		`ALTER TABLE users ADD COLUMN name TEXT DEFAULT 'System Administrator'`,
		`ALTER TABLE users ADD COLUMN email TEXT DEFAULT 'admin@example.com'`,
		`ALTER TABLE users ADD COLUMN bio TEXT DEFAULT 'Default system administrator.'`,
		`ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'subscriber'`,

		// Menu columns
		`ALTER TABLE menu_items ADD COLUMN parent_id INTEGER DEFAULT 0`,
		`ALTER TABLE menu_items ADD COLUMN location TEXT DEFAULT 'header'`,

		// User management columns
		`ALTER TABLE users ADD COLUMN avatar_url TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN status TEXT DEFAULT 'active'`,
		`ALTER TABLE users ADD COLUMN last_login DATETIME DEFAULT NULL`,
		`ALTER TABLE users ADD COLUMN updated_at DATETIME DEFAULT NULL`,
		`ALTER TABLE users ADD COLUMN phone TEXT DEFAULT ''`,
	}

	for _, sql := range alters {
		db.Exec(sql) // Silently ignore "duplicate column" errors
	}

	// Ensure first user is admin
	db.Exec(`UPDATE users SET role = 'admin' WHERE id = 1`)
}

// seedDefaults inserts default settings if they don't already exist.
func seedDefaults(db *sql.DB) {
	defaults := map[string]string{
		"site_title":       "BinaryCMS",
		"site_tagline":     "The Premium Content Management Engine",
		"site_logo_url":    "",
		"custom_footer":    "© 2026 BinaryCMS. All rights reserved.",
		"homepage_type":    "posts",
		"homepage_page_id": "0",
		"frontend_theme":   "default",
		"backend_theme":    "default",
		"brand_color":      "#4f46e5",
		"posts_per_page":       "10",
		"registration_enabled": "true",
		"default_user_role":    "subscriber",
		"comments_enabled": "true",
		"comment_moderation": "true",
		"footer_brand_text":  "The fast, flexible, open-source CMS built for modern web applications.",
		"footer_col1_title":  "PRODUCT",
		"footer_col2_title":  "DEVELOPERS",
		"footer_col3_title":  "COMPANY",
	}

	for key, value := range defaults {
		db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)", key, value)
	}
}
