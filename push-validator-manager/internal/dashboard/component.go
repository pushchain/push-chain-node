package dashboard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cespare/xxhash/v2"
)

// Component interface - all dashboard panels implement this
type Component interface {
	// Bubbletea lifecycle
	Init() tea.Cmd
	Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd)
	View(width, height int) string

	// Metadata
	ID() string
	Title() string
	MinWidth() int  // Minimum width required
	MinHeight() int // Minimum height required
}

// BaseComponent provides common functionality for all components
// Includes hash-based caching to prevent unnecessary re-renders
type BaseComponent struct {
	id    string
	title string
	minW  int
	minH  int

	// Performance optimization - cache rendered output
	lastHash uint64
	cached   string
}

// ID returns component identifier
func (c *BaseComponent) ID() string {
	return c.id
}

// Title returns component title
func (c *BaseComponent) Title() string {
	return c.title
}

// MinWidth returns minimum width required
func (c *BaseComponent) MinWidth() int {
	return c.minW
}

// MinHeight returns minimum height required
func (c *BaseComponent) MinHeight() int {
	return c.minH
}

// Init performs initialization (default: no-op)
func (c *BaseComponent) Init() tea.Cmd {
	return nil
}

// CheckCache checks if content changed using xxhash
// Returns true if cache hit (content unchanged)
func (c *BaseComponent) CheckCache(content string) bool {
	h64 := xxhash.Sum64String(content)
	if h64 == c.lastHash && c.cached != "" {
		return true // Cache hit
	}
	c.lastHash = h64
	return false
}

// cacheKey generates a hash key including content and dimensions
func (c *BaseComponent) cacheKey(content string, w, h int) uint64 {
	// Include dimensions in hash to invalidate cache on resize
	return xxhash.Sum64String(fmt.Sprintf("%dx%d|%s", w, h, content))
}

// CheckCacheWithSize checks if content or size changed using xxhash
// Returns true if cache hit (content and dimensions unchanged)
func (c *BaseComponent) CheckCacheWithSize(content string, w, h int) bool {
	h64 := c.cacheKey(content, w, h)
	if h64 == c.lastHash && c.cached != "" {
		return true // Cache hit
	}
	c.lastHash = h64
	return false
}

// UpdateCache stores rendered output in cache
func (c *BaseComponent) UpdateCache(rendered string) {
	c.cached = rendered
}

// GetCached returns cached output
func (c *BaseComponent) GetCached() string {
	return c.cached
}

// ComponentRegistry manages collection of dashboard components
// Maintains deterministic registration order for consistent rendering
type ComponentRegistry struct {
	order      []string             // Ordered list of component IDs
	components map[string]Component // ID -> Component lookup
}

// NewComponentRegistry creates a new registry
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{
		order:      make([]string, 0),
		components: make(map[string]Component),
	}
}

// Register adds a component to the registry
func (r *ComponentRegistry) Register(comp Component) {
	id := comp.ID()
	if _, exists := r.components[id]; !exists {
		r.order = append(r.order, id)
	}
	r.components[id] = comp
}

// Get retrieves a component by ID
func (r *ComponentRegistry) Get(id string) Component {
	return r.components[id]
}

// All returns all registered components in registration order
func (r *ComponentRegistry) All() []Component {
	comps := make([]Component, 0, len(r.order))
	for _, id := range r.order {
		comps = append(comps, r.components[id])
	}
	return comps
}

// UpdateAll updates all components with new data in registration order
func (r *ComponentRegistry) UpdateAll(msg tea.Msg, data DashboardData) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(r.order))
	for _, id := range r.order {
		comp := r.components[id]
		updated, cmd := comp.Update(msg, data)
		r.components[id] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}
