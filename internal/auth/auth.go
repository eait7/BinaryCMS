package auth

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/theme"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

var store *sessions.CookieStore

// loginAttempts tracks brute-force attempts per IP
var loginAttempts = make(map[string][]time.Time)

func Init() {
	secret := os.Getenv("GOCMS_SESSION_SECRET")
	if secret == "" {
		// Generate a random 32-byte key if none is configured
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("Failed to generate session secret: %v", err)
		}
		secret = hex.EncodeToString(b)
		log.Println("WARNING: No GOCMS_SESSION_SECRET set — using auto-generated key. Sessions will not persist across restarts.")
	}
	store = sessions.NewCookieStore([]byte(secret))

	isProd := os.Getenv("ENV") == "production"
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	}
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// RequireLogin redirects unauthenticated users to the login page.
func RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session-name")
		auth, ok := session.Values["authenticated"].(bool)
		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminRole blocks subscriber-class users from accessing the admin backend.
func RequireAdminRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session-name")
		auth, ok := session.Values["authenticated"].(bool)
		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		userID, ok := session.Values["user_id"].(int)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		user, err := models.GetUserByID(userID)
		if err != nil || user.Role == "subscriber" {
			http.Redirect(w, r, "/profile", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GetSessionUser(r *http.Request) (models.User, bool) {
	session, _ := store.Get(r, "session-name")
	userID, ok := session.Values["user_id"].(int)
	if !ok {
		return models.User{}, false
	}
	user, err := models.GetUserByID(userID)
	if err != nil {
		return models.User{}, false
	}
	return user, true
}

func LoginHandlerTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles(theme.GetBackendPath("login.html"))
	if err != nil {
		log.Printf("Login template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, nil)
}

// isRateLimited checks if an IP has exceeded login attempts (5 per minute).
func isRateLimited(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	// Prune old entries
	var recent []time.Time
	for _, t := range loginAttempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	loginAttempts[ip] = recent

	return len(recent) >= 5
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if isRateLimited(ip) {
		http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	// Basic input validation
	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Sanitize username - only allow alphanumeric, underscore, hyphen, dot
	validUsername := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validUsername.MatchString(username) {
		http.Error(w, "Invalid username format", http.StatusBadRequest)
		return
	}

	if u, ok := models.CheckUserLogin(username, password); ok {
		session, _ := store.Get(r, "session-name")
		session.Values["authenticated"] = true
		session.Values["user_id"] = u.ID
		session.Save(r, w)

		// Update last login timestamp
		models.UpdateUserLastLogin(u.ID)

		// Redirect subscribers to frontend, admin/staff to backend
		if u.Role == "subscriber" {
			http.Redirect(w, r, "/my-account", http.StatusFound)
		} else {
			http.Redirect(w, r, "/admin", http.StatusFound)
		}
		return
	}

	// Record failed attempt
	loginAttempts[ip] = append(loginAttempts[ip], time.Now())
	http.Error(w, "Forbidden - Incorrect Username or Password", http.StatusForbidden)
}

// SetSession creates an authenticated session for a user (used by registration auto-login).
func SetSession(w http.ResponseWriter, r *http.Request, userID int) {
	session, _ := store.Get(r, "session-name")
	session.Values["authenticated"] = true
	session.Values["user_id"] = userID
	session.Save(r, w)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session-name")
	session.Values["authenticated"] = false
	session.Values["user_id"] = nil
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}
