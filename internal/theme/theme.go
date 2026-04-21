package theme

import (
	"html/template"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/ez8/gocms/internal/models"
)

// TemplateCache stores parsed templates to avoid re-parsing on every request.
var cache = &templateCache{
	templates: make(map[string]*template.Template),
}

type templateCache struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

// Get returns a cached template or parses and caches it.
func (tc *templateCache) Get(path string) (*template.Template, error) {
	tc.mu.RLock()
	if t, ok := tc.templates[path]; ok {
		tc.mu.RUnlock()
		return t, nil
	}
	tc.mu.RUnlock()

	t, err := template.ParseFiles(path)
	if err != nil {
		return nil, err
	}

	tc.mu.Lock()
	tc.templates[path] = t
	tc.mu.Unlock()

	return t, nil
}

// Invalidate clears the template cache (call on theme change).
func InvalidateCache() {
	cache.mu.Lock()
	cache.templates = make(map[string]*template.Template)
	cache.mu.Unlock()
	log.Println("Template cache invalidated")
}

// GetCachedTemplate returns a cached, parsed template ready for execution.
func GetCachedTemplate(path string) (*template.Template, error) {
	return cache.Get(path)
}

// GetFrontendPath resolves the template path for the active frontend theme,
// falling back to "default" if the file doesn't exist in the active theme.
func GetFrontendPath(templateName string) string {
	themeName := models.GetSetting("frontend_theme")
	if themeName == "" {
		themeName = "default"
	}

	path := filepath.Join("themes", "frontend", themeName, templateName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("Template %s not found in theme %s, falling back to default", templateName, themeName)
		return filepath.Join("themes", "frontend", "default", templateName)
	}
	return path
}

// GetBackendPath resolves the template path for the active backend theme,
// falling back to "default" if the file doesn't exist in the active theme.
func GetBackendPath(templateName string) string {
	themeName := models.GetSetting("backend_theme")
	if themeName == "" {
		themeName = "default"
	}

	path := filepath.Join("themes", "backend", themeName, templateName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("Template %s not found in backend theme %s, falling back to default", templateName, themeName)
		return filepath.Join("themes", "backend", "default", templateName)
	}
	return path
}

// ExtractFrontendCSS parses the active frontend theme_index.html and returns a list of stylesheet URLs
func ExtractFrontendCSS() []string {
	path := GetFrontendPath("theme_index.html")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(b)
	
	var cssPaths []string
	
	// Basic regex to find <link rel="stylesheet" href="...">
	re := regexp.MustCompile(`<link[^>]+rel=["']stylesheet["'][^>]+href=["']([^"']+)["'][^>]*>|<link[^>]+href=["']([^"']+)["'][^>]+rel=["']stylesheet["'][^>]*>`)
	matches := re.FindAllStringSubmatch(content, -1)
	
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" {
			cssPaths = append(cssPaths, m[1])
		} else if len(m) > 2 && m[2] != "" {
			cssPaths = append(cssPaths, m[2])
		}
	}
	
	return cssPaths
}
