package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ez8/gocms/internal/auth"
	"github.com/ez8/gocms/internal/corelock"
	"github.com/ez8/gocms/internal/db"
	"github.com/ez8/gocms/internal/handlers"
	"github.com/ez8/gocms/internal/models"
	"github.com/ez8/gocms/internal/pluginmanager"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	GitCommit = "development"
	BuildTime = "unknown"
)

func main() {
	// =====================
	// Core Lock CLI Commands (must be first — before any init)
	// =====================
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--lock-core":
			fmt.Println("🔒 Generating core.lock manifest...")
			os.MkdirAll("data", 0755)
			if err := corelock.GenerateLock(GitCommit); err != nil {
				log.Fatalf("Failed to generate core lock: %v", err)
			}
			os.Exit(0)
		case "--verify-core":
			fmt.Println("🔍 Verifying core integrity...")
			violations, err := corelock.VerifyLock()
			if err != nil {
				log.Fatalf("Verification error: %v", err)
			}
			if len(violations) == 0 {
				fmt.Println("✅ Core integrity verified — all files match.")
				os.Exit(0)
			}
			fmt.Printf("❌ CORE INTEGRITY VIOLATED — %d file(s) affected:\n", len(violations))
			for _, v := range violations {
				fmt.Printf("   ⚠ [%s] %s\n", v.Reason, v.File)
			}
			os.Exit(1)
		}
	}

	// =====================
	// Core Lock Startup Check
	// =====================
	// DISABLED: Core lock enforcement is disabled during active development.
	// To re-enable, uncomment the block below. The --lock-core and --verify-core
	// CLI commands still work for manual integrity checks.
	//
	// if corelock.HasLockFile() {
	// 	if os.Getenv("GOCMS_CORE_UNLOCK") == "true" {
	// 		log.Println("⚠️  CORE LOCK BYPASSED — running in UNLOCK mode (GOCMS_CORE_UNLOCK=true)")
	// 	} else {
	// 		violations, err := corelock.VerifyLock()
	// 		if err != nil {
	// 			log.Fatalf("❌ Core lock verification failed: %v", err)
	// 		}
	// 		if len(violations) > 0 {
	// 			log.Printf("❌ CORE INTEGRITY VIOLATED — %d file(s) have been tampered with:", len(violations))
	// 			for _, v := range violations {
	// 				log.Printf("   ⚠ [%s] %s", v.Reason, v.File)
	// 			}
	// 			log.Fatal("🛑 Server startup BLOCKED. Set GOCMS_CORE_UNLOCK=true to bypass, or run --lock-core to re-lock after authorized changes.")
	// 		}
	// 		log.Println("✅ Core integrity verified — all files match core.lock")
	// 	}
	// }

	// Pass build info to the updater
	handlers.GitCommit = GitCommit
	handlers.BuildTime = BuildTime

	// Ensure data directory exists
	os.MkdirAll("data", 0755)


	// Determine Database Path based on environment
	dbPath := "cms.db"
	if _, err := os.Stat("/app/data"); err == nil {
		dbPath = "/app/data/cms.db"
	}
	if envDbPath := os.Getenv("DB_PATH"); envDbPath != "" {
		dbPath = envDbPath
	}

	err := db.Init(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Create default admin user if none exists
	models.CreateDefaultAdmin()

	// Create plugin licenses table for marketplace
	models.CreatePluginLicensesTable()

	// Initialize Auth (session store)
	auth.Init()

	// Initialize Plugins
	pm := pluginmanager.New()
	err = pm.LoadPlugins("plugins")
	if err != nil {
		log.Printf("Warning: failed to load some plugins: %v", err)
	}
	defer pm.Cleanup()

	// Start scheduled post publisher (checks every minute)
	go startScheduler()

	// Router setup
	r := chi.NewRouter()

	// Core middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))

	// Security headers
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY") // was SAMEORIGIN — admin panels should never be framed
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com https://unpkg.com; "+
					"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://rsms.me https://fonts.googleapis.com https://unpkg.com https://maxcdn.bootstrapcdn.com; "+
					"font-src 'self' https://cdn.jsdelivr.net https://rsms.me https://fonts.gstatic.com https://maxcdn.bootstrapcdn.com data:; "+
					"img-src * data: blob:; "+
					"media-src * data: blob:; "+
					"frame-src *; "+
					"connect-src *; "+
					"frame-ancestors 'none';",
			)
			next.ServeHTTP(w, r)
		})
	})

	// Static file serving caching architecture
	staticCacheHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if os.Getenv("ENV") == "production" {
				w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 Year
			} else {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}
			h.ServeHTTP(w, r)
		})
	}
	r.Handle("/static/*", http.StripPrefix("/static/", staticCacheHandler(http.FileServer(http.Dir("static")))))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", staticCacheHandler(http.FileServer(http.Dir("uploads")))))

	// =====================
	// Admin Routes
	// =====================
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.RequireAdminRole)

		r.Get("/", handleAdminDashboard(pm))

		// Posts
		r.Get("/posts", handleListPosts(pm))
		r.Get("/posts/new", handleNewPost(pm))
		r.Post("/posts/new", handleNewPost(pm))
		r.Get("/posts/edit/{id}", handleEditPost(pm))
		r.Post("/posts/edit/{id}", handleEditPost(pm))
		r.Post("/posts/delete/{id}", handleDeletePost(pm))

		// Pages
		r.Get("/pages", handleListPages(pm))
		r.Get("/pages/new", handleNewPage(pm))
		r.Post("/pages/new", handleNewPage(pm))
		r.Get("/pages/edit/{id}", handleEditPage(pm))
		r.Post("/pages/edit/{id}", handleEditPage(pm))
		r.Post("/pages/delete/{id}", handleDeletePage(pm))

		// Categories
		r.Get("/categories", handleListCategories(pm))
		r.Post("/categories/new", handleCreateCategory(pm))
		r.Get("/categories/edit/{id}", handleEditCategory(pm))
		r.Post("/categories/edit/{id}", handleEditCategory(pm))
		r.Post("/categories/delete/{id}", handleDeleteCategory(pm))

		// Tags
		r.Get("/tags", handleListTags(pm))
		r.Post("/tags/new", handleCreateTag(pm))
		r.Post("/tags/delete/{id}", handleDeleteTag(pm))

		// Comments
		r.Get("/comments", handleListComments(pm))
		r.Post("/comments/{action}/{id}", handleCommentAction(pm))

		// Search
		r.Get("/search", handleAdminSearch(pm))

		// Settings
		r.Get("/settings", handleSettings(pm))
		r.Post("/settings", handleSettings(pm))

		// Themes
		r.Get("/themes", handleThemes(pm))
		r.Post("/themes", handleThemes(pm))
		r.Post("/themes/upload", handleUploadTheme(pm))
		r.Post("/themes/delete/{type}/{name}", handleDeleteTheme(pm))
		r.Get("/themes/export", handleExportTheme(pm))

		// Media
		r.Get("/media", handleMediaLibrary(pm))
		r.Get("/media/json", handleMediaJSON())
		r.Post("/media/upload", handleMediaUpload(pm))
		r.Post("/media/delete/{filename}", handleMediaDelete(pm))

		// Menus (frontend)
		r.Get("/menus", handleListMenus(pm))
		r.Post("/menus/add_page", handleAddMenuPage(pm))
		r.Post("/menus/add_link", handleAddMenuLink(pm))
		r.Post("/menus/reorder", handleReorderMenus(pm))
		r.Post("/menus/delete/{id}", handleDeleteMenu(pm))
		r.Post("/menus/edit/{id}", handleEditMenuItem(pm))

		// Admin Menu Arrangement (navbar drag-and-drop)
		r.Get("/arrange-menus", handleAdminMenuArrange(pm))
		r.Post("/arrange-menus/save", handleSaveAdminMenuArrangement(pm))
		r.Post("/arrange-menus/reset", handleResetAdminMenuArrangement(pm))

		// Users (unified management)
		r.Get("/users", handleListUsers(pm))
		r.Get("/users/new", handleNewUser(pm))
		r.Post("/users/new", handleNewUser(pm))
		r.Get("/users/edit/{id}", handleEditUser(pm))
		r.Post("/users/edit/{id}", handleEditUser(pm))
		r.Post("/users/delete/{id}", handleDeleteUser(pm))
		r.Post("/users/toggle-status/{id}", handleToggleUserStatus(pm))
		r.Get("/users/developer-guide", handleDevGuide(pm))

		// Core Updater
		r.Get("/api/updater/check", handlers.CheckUpdate)
		r.Post("/api/updater/install", handlers.InstallUpdate)

		// Backwards-compatible redirect
		r.Get("/subscribers", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/users?role=subscriber", http.StatusMovedPermanently)
		})

		// Plugins
		r.Get("/plugins", handleListPlugins(pm))
		r.Post("/plugins/{action}/{filename}", handlePluginState(pm))
		r.Post("/plugins/upload", handleUploadPlugin(pm))

		// Plugin Store — API endpoints (merged under /plugins)
		r.Post("/plugins/store/install", handleMarketplaceInstall(pm))
		r.Post("/plugins/store/activate", handleMarketplaceActivate(pm))
		r.Get("/api/plugins/store/status", handleMarketplaceStatus(pm))
		// Permanent redirect: /admin/marketplace → /admin/plugins (backward compat)
		r.Get("/marketplace", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/plugins", http.StatusMovedPermanently)
		})

		// Dashboard Widgets
		r.Get("/widgets", handleWidgetManagement(pm))
		r.Get("/widgets/new", handleNewWidget(pm))
		r.Post("/widgets/new", handleNewWidget(pm))
		r.Get("/widgets/edit/{id}", handleEditWidget(pm))
		r.Post("/widgets/edit/{id}", handleEditWidget(pm))
		r.Post("/widgets/delete/{id}", handleDeleteWidget(pm))
		r.Post("/widgets/reorder", handleReorderWidgets(pm))

		// System Resources API (builtin widget data)
		r.Get("/api/system-resources", handleSystemResourcesAPI())
		r.Get("/widget/system-resources", handleSystemResourcesWidget())

		// Dashboard Layout API (block order persistence)
		r.Get("/api/dashboard-layout", handleDashboardLayoutAPI())
		r.Post("/api/dashboard-layout", handleDashboardLayoutAPI())

		// Profile
		r.Get("/profile", handleAdminProfile(pm))
		r.Post("/profile", handleAdminProfile(pm))

		// Plugin admin routes (catch-all)
		r.Get("/plugin/*", handlePluginAdminRoute(pm))
		r.Post("/plugin/*", handlePluginAdminRoute(pm))

		// Server restart (graceful)
		r.Get("/restart", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`
				<html>
				<head><meta http-equiv="refresh" content="3;url=/admin"></head>
				<body>
					<div style="font-family: sans-serif; text-align: center; margin-top: 50px;">
						<h2>Server is restarting...</h2>
						<p>You will be redirected automatically...</p>
					</div>
				</body>
				</html>
			`))

			go func() {
				time.Sleep(1 * time.Second)
				log.Println("Restart invoked from admin panel")
				exe, err := os.Executable()
				if err != nil {
					log.Printf("Cannot get executable path: %v", err)
					os.Exit(1)
				}
				err = syscall.Exec(exe, os.Args, os.Environ())
				if err != nil {
					log.Printf("Exec failed: %v", err)
					os.Exit(1)
				}
			}()
		})
	})

	// =====================
	// API Routes
	// =====================
	r.Route("/api", func(r chi.Router) {
		r.Get("/dynamic-favicon.svg", handleDynamicFaviconSVG())
		r.Get("/posts", handleAPIPosts())
		r.Get("/post/{slug}", handleAPIPost())
		r.Get("/pages", handleAPIPages())
		r.Get("/page/{slug}", handleAPIPage())
		r.Get("/search", handleAPISearch())

		// Public Plugin API routes (catch-all)
		r.Get("/plugin/*", handlePluginPublicRoute(pm))
		r.Post("/plugin/*", handlePluginPublicRoute(pm))
	})

	// =====================
	// Auth Routes
	// =====================
	r.Get("/login", auth.LoginHandlerTemplate)
	r.Post("/login", auth.LoginHandler)
	r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
		auth.LogoutHandler(w, r)
	})

	// Registration
	r.Get("/register", handleFrontendRegister(pm))
	r.Post("/register", handleFrontendRegister(pm))

	// =====================
	// Public Authenticated Routes
	// =====================
	r.Route("/profile", func(r chi.Router) {
		r.Use(auth.RequireLogin)
		r.Get("/", handleFrontendProfile(pm))
		r.Post("/", handleFrontendProfile(pm))
	})

	r.Route("/my-account", func(r chi.Router) {
		r.Use(auth.RequireLogin)
		r.Get("/", handleFrontendMyAccount(pm))
		r.Post("/update", handleFrontendUpdateProfile(pm))
		r.Post("/password", handleFrontendChangePassword(pm))
	})

	// =====================
	// Public Frontend Routes
	// =====================
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		handleFrontendIndex(pm).ServeHTTP(w, r)
	})

	r.Get("/post/{slug}", handleFrontendPost(pm))
	r.Post("/post/{slug}/comment", handleFrontendCommentSubmit(pm))
	r.Get("/category/{slug}", handleFrontendCategory(pm))
	r.Get("/tag/{slug}", handleFrontendTag(pm))
	r.Get("/search", handleFrontendSearch(pm))

	// Fallback: static pages (plugins can intercept via HookFrontendRoute)
	r.Get("/{slug}", handleFrontendPage(pm))

	// =====================
	// Server Setup
	// =====================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutdown signal received, draining connections...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Graceful shutdown failed: %v", err)
		}
		log.Println("Server stopped gracefully")
	}()

	log.Printf("GoCMS server starting on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// startScheduler runs a background ticker that publishes scheduled posts.
func startScheduler() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		count, err := models.PublishScheduledPosts()
		if err != nil {
			log.Printf("Scheduler error: %v", err)
			continue
		}
		if count > 0 {
			log.Printf("Scheduler: Published %d scheduled post(s)", count)
		}
	}
}
