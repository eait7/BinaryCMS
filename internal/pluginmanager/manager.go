package pluginmanager

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ez8/gocms/pkg/plugin"
	hplugin "github.com/hashicorp/go-plugin"
)

type Manager struct {
	mu      sync.RWMutex
	clients []*hplugin.Client
	plugins []plugin.CMSPlugin
}

func New() *Manager {
	return &Manager{}
}

// LoadPlugins discovers and connects to all plugin binaries in the given directory.
func (m *Manager) LoadPlugins(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), ".disabled") || strings.HasSuffix(file.Name(), ".deleted") {
			continue
		}
		pluginPath := filepath.Join(dir, file.Name())

		client := hplugin.NewClient(&hplugin.ClientConfig{
			HandshakeConfig: plugin.HandshakeConfig,
			Plugins:         plugin.PluginMap,
			Cmd:             exec.Command(pluginPath),
		})
		m.clients = append(m.clients, client)

		rpcClient, err := client.Client()
		if err != nil {
			log.Printf("Error connecting to plugin %s: %s", pluginPath, err)
			continue
		}

		raw, err := rpcClient.Dispense("cms_plugin")
		if err != nil {
			log.Printf("Error dispensing plugin %s: %s", pluginPath, err)
			continue
		}

		cmsPlugin := raw.(plugin.CMSPlugin)
		m.plugins = append(m.plugins, cmsPlugin)
		log.Printf("Loaded plugin: %s", cmsPlugin.PluginName())
	}

	// Dump active menus for external tools
	var allActive []plugin.MenuItem
	for _, p := range m.plugins {
		pluginMenus := p.HookAdminMenu()
		if len(pluginMenus) > 0 {
			allActive = append(allActive, pluginMenus...)
		}
	}
	if b, err := json.MarshalIndent(allActive, "", "  "); err == nil {
		menuPath := "backend_menus_available.json"
		if _, err := os.Stat("/app/data"); err == nil {
			menuPath = "/app/data/backend_menus_available.json"
		} else if _, err := os.Stat("data"); err == nil {
			menuPath = "data/backend_menus_available.json"
		}
		os.WriteFile(menuPath, b, 0644)
	}

	return nil
}

func (m *Manager) GetAdminMenus() []plugin.MenuItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var menus []plugin.MenuItem
	for _, p := range m.plugins {
		pluginMenus := p.HookAdminMenu()
		if len(pluginMenus) > 0 {
			menus = append(menus, pluginMenus...)
		}
	}
	return menus
}

// RenderAdminRoute asks all plugins to handle a route. First valid response wins.
func (m *Manager) RenderAdminRoute(route string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		html := p.HookAdminRoute(route)
		if html != "" && html != "Plugin Route Error" && html != "Not implemented" && html != "Invalid route." {
			return html
		}
	}
	return "Page not found."
}

// RenderFrontendRoute lets plugins claim a public frontend URL path.
// The first plugin that returns a non-empty string wins; the core then
// writes that HTML directly to the response without any DB page lookup.
func (m *Manager) RenderFrontendRoute(route string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		html := p.HookFrontendRoute(route)
		if html != "" {
			return html
		}
	}
	return ""
}


func (m *Manager) GetDashboardWidgets() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var widgets []string
	for _, p := range m.plugins {
		w := p.HookDashboardWidget()
		if w != "" {
			widgets = append(widgets, w)
		}
	}
	return widgets
}

func (m *Manager) GetAdminTopRightWidgets() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	for _, p := range m.plugins {
		w := p.HookAdminTopRightWidget()
		if w != "" {
			sb.WriteString(w)
		}
	}
	return sb.String()
}

func (m *Manager) GetPluginsList() []plugin.CMSPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plugins
}

// GetUserProfileTabs collects HTML tabs from all plugins for the admin user detail page.
func (m *Manager) GetUserProfileTabs(userID int) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tabs []string
	for _, p := range m.plugins {
		html := p.HookUserProfileTab(userID)
		if html != "" {
			tabs = append(tabs, html)
		}
	}
	return tabs
}

// GetUserAccountCards collects HTML cards from all plugins for the frontend /my-account page.
func (m *Manager) GetUserAccountCards(userID int) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var cards []string
	for _, p := range m.plugins {
		html := p.HookUserAccountCard(userID)
		if html != "" {
			cards = append(cards, html)
		}
	}
	return cards
}

// RunUserRegisteredHook notifies all plugins that a new user was registered.
func (m *Manager) RunUserRegisteredHook(userID int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		p.HookUserRegistered(userID)
	}
}

func (m *Manager) RunHook(hookName string, args ...interface{}) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch hookName {
	case "BeforeFrontPageRender":
		content := args[0].(string)
		for _, p := range m.plugins {
			content = p.HookBeforeFrontPageRender(content)
		}
		return content
	}
	return nil
}

// Cleanup kills all running plugin processes.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		go func(c *hplugin.Client) {
			c.Kill()
		}(client)
	}
	m.clients = nil
	m.plugins = nil
}

func (m *Manager) ReloadPlugins(dir string) error {
	m.Cleanup()
	return m.LoadPlugins(dir)
}
