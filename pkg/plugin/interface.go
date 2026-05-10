package plugin

import (
	"log"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// MenuItem represents a sidebar navigation entry in the admin panel.
type MenuItem struct {
	Label    string
	URL      string
	Icon     string     // Tabler icon name (e.g., "home", "settings")
	Children []MenuItem // Nested sub-menu items
}

// CMSPlugin is the interface that all GoCMS plugins must implement.
type CMSPlugin interface {
	HookBeforeFrontPageRender(content string) string
	PluginName() string
	HookAdminMenu() []MenuItem
	HookAdminRoute(route string) string
	HookDashboardWidget() string
	HookAdminTopRightWidget() string

	// HookFrontendRoute lets a plugin serve a public frontend URL.
	// Return the complete HTML response to claim the route, or "" to pass through.
	HookFrontendRoute(route string) string

	// User management hooks (v2)
	HookUserProfileTab(userID int) string
	HookUserAccountCard(userID int) string
	HookUserRegistered(userID int) string
}

// --- RPC Client Implementation ---

type CMSPluginRPC struct{ client *rpc.Client }

func (g *CMSPluginRPC) HookBeforeFrontPageRender(content string) string {
	var resp string
	err := g.client.Call("Plugin.HookBeforeFrontPageRender", content, &resp)
	if err != nil {
		return content
	}
	return resp
}

func (g *CMSPluginRPC) PluginName() string {
	var resp string
	err := g.client.Call("Plugin.PluginName", new(interface{}), &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookAdminMenu() []MenuItem {
	var resp []MenuItem
	err := g.client.Call("Plugin.HookAdminMenu", new(interface{}), &resp)
	if err != nil {
		return nil
	}
	return resp
}

func (g *CMSPluginRPC) HookAdminRoute(route string) string {
	var resp string
	err := g.client.Call("Plugin.HookAdminRoute", route, &resp)
	if err != nil {
		return "Plugin Route Error"
	}
	return resp
}

func (g *CMSPluginRPC) HookDashboardWidget() string {
	var resp string
	err := g.client.Call("Plugin.HookDashboardWidget", new(interface{}), &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookAdminTopRightWidget() string {
	var resp string
	err := g.client.Call("Plugin.HookAdminTopRightWidget", new(interface{}), &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookFrontendRoute(route string) string {
	var resp string
	err := g.client.Call("Plugin.HookFrontendRoute", route, &resp)
	if err != nil {
		log.Printf("[plugin_rpc] HookFrontendRoute error: %v", err)
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookUserProfileTab(userID int) string {
	var resp string
	err := g.client.Call("Plugin.HookUserProfileTab", userID, &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookUserAccountCard(userID int) string {
	var resp string
	err := g.client.Call("Plugin.HookUserAccountCard", userID, &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CMSPluginRPC) HookUserRegistered(userID int) string {
	var resp string
	err := g.client.Call("Plugin.HookUserRegistered", userID, &resp)
	if err != nil {
		return ""
	}
	return resp
}

// --- RPC Server Implementation ---

type CMSPluginRPCServer struct {
	Impl CMSPlugin
}

func (s *CMSPluginRPCServer) HookBeforeFrontPageRender(args string, resp *string) error {
	*resp = s.Impl.HookBeforeFrontPageRender(args)
	return nil
}

func (s *CMSPluginRPCServer) PluginName(args interface{}, resp *string) error {
	*resp = s.Impl.PluginName()
	return nil
}

func (s *CMSPluginRPCServer) HookAdminMenu(args interface{}, resp *[]MenuItem) error {
	*resp = s.Impl.HookAdminMenu()
	return nil
}

func (s *CMSPluginRPCServer) HookAdminRoute(args string, resp *string) error {
	*resp = s.Impl.HookAdminRoute(args)
	return nil
}

func (s *CMSPluginRPCServer) HookDashboardWidget(args interface{}, resp *string) error {
	*resp = s.Impl.HookDashboardWidget()
	return nil
}

func (s *CMSPluginRPCServer) HookAdminTopRightWidget(args interface{}, resp *string) error {
	*resp = s.Impl.HookAdminTopRightWidget()
	return nil
}

func (s *CMSPluginRPCServer) HookFrontendRoute(args string, resp *string) error {
	*resp = s.Impl.HookFrontendRoute(args)
	return nil
}

func (s *CMSPluginRPCServer) HookUserProfileTab(userID int, resp *string) error {
	*resp = s.Impl.HookUserProfileTab(userID)
	return nil
}

func (s *CMSPluginRPCServer) HookUserAccountCard(userID int, resp *string) error {
	*resp = s.Impl.HookUserAccountCard(userID)
	return nil
}

func (s *CMSPluginRPCServer) HookUserRegistered(userID int, resp *string) error {
	*resp = s.Impl.HookUserRegistered(userID)
	return nil
}

// --- Plugin Definition ---

type CMSPluginDef struct {
	Impl CMSPlugin
}

func (p *CMSPluginDef) Server(*plugin.MuxBroker) (interface{}, error) {
	return &CMSPluginRPCServer{Impl: p.Impl}, nil
}

func (CMSPluginDef) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &CMSPluginRPC{client: c}, nil
}

// HandshakeConfig is shared between plugin host and client.
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GOCMS_PLUGIN",
	MagicCookieValue: "hello",
}

// PluginMap defines the available plugin types.
var PluginMap = map[string]plugin.Plugin{
	"cms_plugin": &CMSPluginDef{},
}

// ServePlugin is a convenience function for plugin developers.
func ServePlugin(impl CMSPlugin) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"cms_plugin": &CMSPluginDef{Impl: impl},
		},
	})
}
