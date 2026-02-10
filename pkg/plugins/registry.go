package plugins

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/ocx/backend/internal/protocol"
)

// ConnectorPlugin is the interface any third-party connector must implement.
// Implement this interface to add support for a new AI protocol without
// modifying OCX source code.
//
// Example:
//
//	type MyCustomParser struct{}
//	func (p *MyCustomParser) Name() string { return "my-custom-ai" }
//	func (p *MyCustomParser) Version() string { return "1.0.0" }
//	func (p *MyCustomParser) Protocols() []string { return []string{"custom-ai"} }
//	func (p *MyCustomParser) Priority() int { return 50 }
//	func (p *MyCustomParser) CanHandle(payload []byte) bool { ... }
//	func (p *MyCustomParser) Parse(payload []byte) (*protocol.AIPayload, error) { ... }
type ConnectorPlugin interface {
	// Name returns the plugin's unique identifier
	Name() string

	// Version returns the plugin version
	Version() string

	// Protocols returns the AI protocols this plugin handles
	Protocols() []string

	// Priority determines parse order (lower = tried first)
	Priority() int

	// CanHandle returns true if this plugin can parse the payload
	CanHandle(payload []byte) bool

	// Parse extracts AI payload from raw bytes
	Parse(payload []byte) (*protocol.AIPayload, error)
}

// PluginInfo describes a registered plugin (for API responses)
type PluginInfo struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Protocols []string `json:"protocols"`
	Priority  int      `json:"priority"`
	Active    bool     `json:"active"`
}

// Registry manages connector plugins
type Registry struct {
	mu      sync.RWMutex
	plugins []ConnectorPlugin
	byName  map[string]ConnectorPlugin
	logger  *log.Logger
}

// NewRegistry creates a plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins: make([]ConnectorPlugin, 0),
		byName:  make(map[string]ConnectorPlugin),
		logger:  log.New(log.Writer(), "[PLUGINS] ", log.LstdFlags),
	}
}

// Register adds a plugin to the registry
func (r *Registry) Register(plugin ConnectorPlugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byName[plugin.Name()]; exists {
		return fmt.Errorf("plugin %q already registered", plugin.Name())
	}

	r.plugins = append(r.plugins, plugin)
	r.byName[plugin.Name()] = plugin

	// Re-sort by priority (lower = first)
	sort.Slice(r.plugins, func(i, j int) bool {
		return r.plugins[i].Priority() < r.plugins[j].Priority()
	})

	r.logger.Printf("🔌 Registered plugin: %s v%s (protocols=%v, priority=%d)",
		plugin.Name(), plugin.Version(), plugin.Protocols(), plugin.Priority())
	return nil
}

// Unregister removes a plugin
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.byName, name)
	filtered := make([]ConnectorPlugin, 0)
	for _, p := range r.plugins {
		if p.Name() != name {
			filtered = append(filtered, p)
		}
	}
	r.plugins = filtered
}

// Parse tries all registered plugins in priority order
func (r *Registry) Parse(payload []byte) (*protocol.AIPayload, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, plugin := range r.plugins {
		if plugin.CanHandle(payload) {
			result, err := plugin.Parse(payload)
			if err == nil && result != nil {
				r.logger.Printf("✅ Plugin %s parsed payload (protocol=%s, tool=%s)",
					plugin.Name(), result.Protocol, result.ToolName)
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("no plugin could parse the payload")
}

// List returns info about all registered plugins
func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]PluginInfo, 0, len(r.plugins))
	for _, p := range r.plugins {
		infos = append(infos, PluginInfo{
			Name:      p.Name(),
			Version:   p.Version(),
			Protocols: p.Protocols(),
			Priority:  p.Priority(),
			Active:    true,
		})
	}
	return infos
}

// Get returns a specific plugin by name
func (r *Registry) Get(name string) (ConnectorPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[name]
	return p, ok
}

// Count returns the number of registered plugins
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}
