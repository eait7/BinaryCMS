package models

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ez8/gocms/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int
	Username     string
	PasswordHash string
	Name         string
	Email        string
	Bio          string
	Phone        string
	AvatarURL    string
	Role         string
	Status       string // "active", "suspended", "pending"
	LastLogin    string
	CreatedAt    string
	UpdatedAt    string
}

func (u User) Initials() string {
	name := strings.TrimSpace(u.Name)
	if name != "" {
		parts := strings.Fields(name)
		if len(parts) >= 2 && len(parts[0]) > 0 && len(parts[1]) > 0 {
			return strings.ToUpper(string(parts[0][0]) + string(parts[1][0]))
		}
		if len(parts) == 1 && len(parts[0]) > 0 {
			if len(parts[0]) >= 2 {
				return strings.ToUpper(parts[0][0:2])
			}
			return strings.ToUpper(parts[0][0:1])
		}
	}
	uname := strings.TrimSpace(u.Username)
	if len(uname) >= 2 {
		return strings.ToUpper(uname[0:2])
	}
	if len(uname) == 1 {
		return strings.ToUpper(uname)
	}
	return "G" // Default Guest/Unknown
}

// AvatarOrDefault returns the user's avatar URL or a DiceBear fallback.
func (u User) AvatarOrDefault() string {
	if u.AvatarURL != "" {
		return u.AvatarURL
	}
	seed := u.Username
	if seed == "" {
		seed = u.Email
	}
	return fmt.Sprintf("https://api.dicebear.com/9.x/avataaars/svg?seed=%s", seed)
}

// StatusLabel returns a human-readable status.
func (u User) StatusLabel() string {
	switch u.Status {
	case "suspended":
		return "Suspended"
	case "pending":
		return "Pending"
	default:
		return "Active"
	}
}

// CreateDefaultAdmin creates an "admin/admin" user if no users exist.
// The password should be changed immediately in production.
func CreateDefaultAdmin() {
	var count int
	err := db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Println("Error checking users count:", err)
		return
	}

	if count == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), 14)
		_, err := db.DB.Exec("INSERT INTO users (username, password_hash, role, status) VALUES (?, ?, 'admin', 'active')", "admin", string(hash))
		if err != nil {
			log.Println("Error creating default admin user:", err)
		} else {
			log.Println("Created default 'admin' user with password 'admin'. CHANGE THIS IN PRODUCTION!")
		}
	}
}

const userSelectColumns = `id, username, password_hash, COALESCE(name, ''), COALESCE(email, ''),
	COALESCE(bio, ''), COALESCE(phone, ''), COALESCE(avatar_url, ''),
	COALESCE(role, 'subscriber'), COALESCE(status, 'active'),
	COALESCE(last_login, ''), created_at, COALESCE(updated_at, '')`

func scanUserFull(scanner interface{ Scan(...interface{}) error }) (User, error) {
	var u User
	err := scanner.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Name, &u.Email,
		&u.Bio, &u.Phone, &u.AvatarURL,
		&u.Role, &u.Status,
		&u.LastLogin, &u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}

// CheckUserLogin validates credentials and returns the user if successful.
func CheckUserLogin(username, password string) (User, bool) {
	row := db.DB.QueryRow("SELECT "+userSelectColumns+" FROM users WHERE username = ?", username)
	u, err := scanUserFull(row)
	if err != nil {
		return u, false
	}

	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	if err != nil {
		return u, false
	}

	// Check if account is active
	if u.Status == "suspended" {
		return u, false
	}

	return u, true
}

// UpdateUserLastLogin stamps the current time on last_login.
func UpdateUserLastLogin(id int) error {
	_, err := db.DB.Exec("UPDATE users SET last_login = ? WHERE id = ?", time.Now(), id)
	return err
}

func GetAllUsers() ([]User, error) {
	rows, err := db.DB.Query(
		"SELECT id, username, COALESCE(name, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'subscriber'), COALESCE(status, 'active'), COALESCE(last_login, ''), created_at FROM users ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.AvatarURL, &u.Role, &u.Status, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Println("Error scanning user:", err)
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

func GetUsersByRole(role string) ([]User, error) {
	rows, err := db.DB.Query(
		"SELECT id, username, COALESCE(name, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'subscriber'), COALESCE(status, 'active'), COALESCE(last_login, ''), created_at FROM users WHERE COALESCE(role, 'subscriber') = ? ORDER BY created_at DESC",
		role,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.AvatarURL, &u.Role, &u.Status, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Println("Error scanning user:", err)
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

func GetUsersExcludeRole(role string) ([]User, error) {
	rows, err := db.DB.Query(
		"SELECT id, username, COALESCE(name, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'subscriber'), COALESCE(status, 'active'), COALESCE(last_login, ''), created_at FROM users WHERE COALESCE(role, 'subscriber') != ? ORDER BY created_at DESC",
		role,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.AvatarURL, &u.Role, &u.Status, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Println("Error scanning user:", err)
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// SearchUsers returns paginated users with optional filters.
func SearchUsers(query, role, status string, page, perPage int) ([]User, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	where := []string{"1=1"}
	args := []interface{}{}

	if query != "" {
		where = append(where, "(username LIKE ? OR name LIKE ? OR email LIKE ?)")
		q := "%" + query + "%"
		args = append(args, q, q, q)
	}
	if role != "" {
		where = append(where, "COALESCE(role, 'subscriber') = ?")
		args = append(args, role)
	}
	if status != "" {
		where = append(where, "COALESCE(status, 'active') = ?")
		args = append(args, status)
	}

	whereClause := strings.Join(where, " AND ")

	// Count
	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	err := db.DB.QueryRow("SELECT COUNT(*) FROM users WHERE "+whereClause, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch
	args = append(args, perPage, offset)
	rows, err := db.DB.Query(
		"SELECT id, username, COALESCE(name, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'subscriber'), COALESCE(status, 'active'), COALESCE(last_login, ''), created_at FROM users WHERE "+whereClause+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.AvatarURL, &u.Role, &u.Status, &u.LastLogin, &u.CreatedAt); err != nil {
			continue
		}
		users = append(users, u)
	}
	return users, total, nil
}

// CountUsersByRole returns a map of role -> count.
func CountUsersByRole() map[string]int {
	result := make(map[string]int)
	rows, err := db.DB.Query("SELECT COALESCE(role, 'subscriber'), COUNT(*) FROM users GROUP BY COALESCE(role, 'subscriber')")
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		var count int
		rows.Scan(&role, &count)
		result[role] = count
	}
	return result
}

// CountUsersByStatus returns a map of status -> count.
func CountUsersByStatus() map[string]int {
	result := make(map[string]int)
	rows, err := db.DB.Query("SELECT COALESCE(status, 'active'), COUNT(*) FROM users GROUP BY COALESCE(status, 'active')")
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		result[status] = count
	}
	return result
}

func CreateUser(username, password, role string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	if role == "" {
		role = "subscriber"
	}
	_, err = db.DB.Exec("INSERT INTO users (username, password_hash, role, status, created_at) VALUES (?, ?, ?, 'active', ?)", username, string(hash), role, time.Now())
	return err
}

// CreateUserFull creates a user with all profile fields and returns the new ID.
func CreateUserFull(username, password, name, email, role string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return 0, err
	}
	if role == "" {
		role = "subscriber"
	}
	now := time.Now()
	result, err := db.DB.Exec(
		"INSERT INTO users (username, password_hash, name, email, role, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)",
		username, string(hash), name, email, role, now, now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func DeleteUser(id int) error {
	_, err := db.DB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func GetUserByID(id int) (User, error) {
	row := db.DB.QueryRow("SELECT "+userSelectColumns+" FROM users WHERE id = ?", id)
	return scanUserFull(row)
}

// GetUserByEmail looks up a user by email address.
func GetUserByEmail(email string) (User, error) {
	row := db.DB.QueryRow("SELECT "+userSelectColumns+" FROM users WHERE email = ?", email)
	return scanUserFull(row)
}

// GetUserByUsername looks up a user by username.
func GetUserByUsername(username string) (User, error) {
	row := db.DB.QueryRow("SELECT "+userSelectColumns+" FROM users WHERE username = ?", username)
	return scanUserFull(row)
}

func UpdateUserProfile(id int, name, email, bio string) error {
	_, err := db.DB.Exec("UPDATE users SET name = ?, email = ?, bio = ?, updated_at = ? WHERE id = ?", name, email, bio, time.Now(), id)
	return err
}

// UpdateUserFull updates all editable fields for a user.
func UpdateUserFull(u User) error {
	_, err := db.DB.Exec(
		"UPDATE users SET name = ?, email = ?, bio = ?, phone = ?, avatar_url = ?, role = ?, status = ?, updated_at = ? WHERE id = ?",
		u.Name, u.Email, u.Bio, u.Phone, u.AvatarURL, u.Role, u.Status, time.Now(), u.ID,
	)
	return err
}

func UpdateUserRole(id int, role string) error {
	_, err := db.DB.Exec("UPDATE users SET role = ?, updated_at = ? WHERE id = ?", role, time.Now(), id)
	return err
}

func UpdateUserPassword(id int, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 14)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec("UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", string(hash), time.Now(), id)
	return err
}

// SuspendUser sets user status to suspended.
func SuspendUser(id int) error {
	_, err := db.DB.Exec("UPDATE users SET status = 'suspended', updated_at = ? WHERE id = ?", time.Now(), id)
	return err
}

// ActivateUser sets user status to active.
func ActivateUser(id int) error {
	_, err := db.DB.Exec("UPDATE users SET status = 'active', updated_at = ? WHERE id = ?", time.Now(), id)
	return err
}

// =====================
// User Meta (Plugin Extension Table)
// =====================

// SetUserMeta inserts or updates a user meta key-value pair.
func SetUserMeta(userID int, key, value string) error {
	_, err := db.DB.Exec(
		"INSERT INTO user_meta (user_id, meta_key, meta_value) VALUES (?, ?, ?) ON CONFLICT(user_id, meta_key) DO UPDATE SET meta_value = ?",
		userID, key, value, value,
	)
	return err
}

// GetUserMeta retrieves a single meta value for a user.
func GetUserMeta(userID int, key string) string {
	var value string
	err := db.DB.QueryRow("SELECT meta_value FROM user_meta WHERE user_id = ? AND meta_key = ?", userID, key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

// GetAllUserMeta retrieves all meta key-value pairs for a user.
func GetAllUserMeta(userID int) map[string]string {
	result := make(map[string]string)
	rows, err := db.DB.Query("SELECT meta_key, meta_value FROM user_meta WHERE user_id = ?", userID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) == nil {
			result[key] = value
		}
	}
	return result
}

// DeleteUserMeta removes a specific meta entry.
func DeleteUserMeta(userID int, key string) error {
	_, err := db.DB.Exec("DELETE FROM user_meta WHERE user_id = ? AND meta_key = ?", userID, key)
	return err
}

// GetTotalUserCount returns the total number of users.
func GetTotalUserCount() int {
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count
}

// GetUserMetaByKey retrieves a specific meta value, with a sql error for checking existence.
func GetUserMetaByKey(userID int, key string) (string, error) {
	var value string
	err := db.DB.QueryRow("SELECT meta_value FROM user_meta WHERE user_id = ? AND meta_key = ?", userID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", err
	}
	return value, err
}
