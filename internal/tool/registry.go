package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	// ErrDuplicateTool is returned when a tool name is registered twice.
	ErrDuplicateTool = errors.New("tool already registered")
	// ErrToolNotFound is returned when no tool is registered under a name.
	ErrToolNotFound = errors.New("tool not found")
	// ErrInvalidTool is returned for tools that cannot be registered
	// (nil tool or empty name).
	ErrInvalidTool = errors.New("invalid tool")
)

// Descriptor is the model-facing view of a registered tool: its name,
// description, and JSON Schema documents, JSON-serializable so it can be
// handed to a model.
type Descriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

// Registry holds the registered tools. The zero value is not usable; use
// NewRegistry. Registry is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Registering a nil tool, a tool with an empty name,
// or a name that is already taken is an error.
func (r *Registry) Register(t Tool) error {
	if t == nil || t.Name() == "" {
		return fmt.Errorf("register: %w", ErrInvalidTool)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("register %q: %w", t.Name(), ErrDuplicateTool)
	}
	r.tools[t.Name()] = t
	return nil
}

// Lookup returns the tool registered under name, or ErrToolNotFound.
func (r *Registry) Lookup(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("lookup %q: %w", name, ErrToolNotFound)
	}
	return t, nil
}

// List returns the descriptors of all registered tools, sorted by name.
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, Descriptor{
			Name:         t.Name(),
			Description:  t.Description(),
			InputSchema:  t.InputSchema(),
			OutputSchema: t.OutputSchema(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
