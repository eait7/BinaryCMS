package auth

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/theme"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

var store *sessions.CookieStore

// ── Rate Limiting ─────────────────────────────────────────────────────────────

// rateLimitEntry tracks attempt timestamps and lockout state for one IP.
type rateLimitEntry struct {
	mu          sync.Mutex
	attempts    []time.Time
	lockedUntil time.Time
}

var (
	loginMu      sync.Mutex
	loginLimits  = make(map[string]*rateLimitEntry)

	registrationMu     sync.Mutex
	registrationLimits = make(map[string]*rateLimitEntry)
)

// RealClientIP extracts the actual client IP from a request.
// It respects the X-Real-IP header set by Caddy/nginx, stripping any port.
func RealClientIP(r *http.Request) string {
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(strings.Split(xr, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLoginRateLimited checks whether an IP has exceeded the login attempt threshold.
// Allows 10 attempts per 60 seconds; locks out for 10 minutes after threshold breach.
func isLoginRateLimited(ip string) bool {
	loginMu.Lock()
	e, ok := loginLimits[ip]
	if !ok {
		e = &rateLimitEntry{}
		loginLimits[ip] = e
	}
	loginMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	// Still locked out?
	if now.Before(e.lockedUntil) {
		return true
	}

	// Prune attempts outside the 60-second window.
	cutoff := now.Add(-60 * time.Second)
	var recent []time.Time
	for _, t := range e.attempts {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	e.attempts = recent

	if len(recent) >= 10 {
		// Trigger a 10-minute lockout.
		e.lockedUntil = now.Add(10 * time.Minute)
		return true
	}
	return false
}

// recordLoginAttempt records a failed login for an IP.
func recordLoginAttempt(ip string) {
	loginMu.Lock()
	e, ok := loginLimits[ip]
	if !ok {
		e = &rateLimitEntry{}
		loginLimits[ip] = e
	}
	loginMu.Unlock()

	e.mu.Lock()
	e.attempts = append(e.attempts, time.Now())
	e.mu.Unlock()
}

// clearLoginAttempts resets the counter for an IP on successful login.
func clearLoginAttempts(ip string) {
	loginMu.Lock()
	delete(loginLimits, ip)
	loginMu.Unlock()
}

// IsRegistrationRateLimited checks whether an IP has exceeded the registration
// attempt threshold: 10 attempts per hour.
func IsRegistrationRateLimited(ip string) bool {
	registrationMu.Lock()
	e, ok := registrationLimits[ip]
	if !ok {
		e = &rateLimitEntry{}
		registrationLimits[ip] = e
	}
	registrationMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)
	var recent []time.Time
	for _, t := range e.attempts {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	e.attempts = recent

	if len(recent) >= 10 {
		return true
	}
	// Record this attempt.
	e.attempts = append(e.attempts, now)
	return false
}

// ── Session Store ─────────────────────────────────────────────────────────────

func Init() {
	secret := os.Getenv("GOCMS_SESSION_SECRET")
	if secret == "" {
		// Generate a random 32-byte key if none is configured.
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
		MaxAge:   86400 * 7,             // 7 days (was 30 — admin sessions should not be eternal)
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode, // was Lax — Strict prevents CSRF via cross-site navigations
	}
}

// ── Password Utilities ────────────────────────────────────────────────────────

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ── Middleware ────────────────────────────────────────────────────────────────

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

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	ip := RealClientIP(r)
	if isLoginRateLimited(ip) {
		w.Header().Set("Retry-After", "600")
		renderLoginError(w, "Too many login attempts. Please try again in 10 minutes.", http.StatusTooManyRequests)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	// Basic input validation.
	if username == "" || password == "" {
		renderLoginError(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Sanitize username — only allow alphanumeric, underscore, hyphen, dot.
	validUsername := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validUsername.MatchString(username) {
		renderLoginError(w, "Invalid username format", http.StatusBadRequest)
		return
	}

	if u, ok := models.CheckUserLogin(username, password); ok {
		// Successful login — clear brute-force counter.
		clearLoginAttempts(ip)

		session, _ := store.Get(r, "session-name")
		session.Values["authenticated"] = true
		session.Values["user_id"] = u.ID
		session.Save(r, w)

		// Update last login timestamp.
		models.UpdateUserLastLogin(u.ID)

		// Redirect subscribers to frontend, admin/staff to backend.
		if u.Role == "subscriber" {
			http.Redirect(w, r, "/my-account", http.StatusFound)
		} else {
			http.Redirect(w, r, "/admin", http.StatusFound)
		}
		return
	}

	// Record failed attempt.
	recordLoginAttempt(ip)
	renderLoginError(w, "Forbidden - Incorrect Username or Password", http.StatusForbidden)
}

func renderLoginError(w http.ResponseWriter, errMsg string, statusCode int) {
	w.WriteHeader(statusCode)
	t, err := template.ParseFiles(theme.GetBackendPath("login.html"))
	if err != nil {
		http.Error(w, errMsg, statusCode)
		return
	}
	t.Execute(w, map[string]interface{}{"Error": errMsg})
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
