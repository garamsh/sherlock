package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DefaultRequiredTools is the set of tool names the platform requires
// at startup (ADR 0009 §Decision, invariant 5). A missing required tool
// fails closed. The package-level var is declared on the legacy
// protocol client — the script-tool runner re-uses the same value.

// DefaultToolsDir is the operator's tools directory when the agent's
// config does not override it.
const DefaultToolsDir = "./tools"

// DefaultRequiredTools is the set of tool names the platform requires
// at startup (ADR 0009 §Decision, invariant 5). A missing required tool
// fails closed. The script-tool runtime owns the invariant; the script
// registry uses it.
var DefaultRequiredTools = []string{"message_send"}

// ValidationError reports input that failed schema validation at the
// contract boundary. It matches ErrValidation via errors.Is. The
// description restates the tool's advertised summary so a model reading
// the rejection is steered back to the tool's real contract.
type ValidationError struct {
	Tool        string
	Description string
	Err         error
}

// Error returns a human-readable description of the validation failure.
func (e *ValidationError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("tool %q: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("tool %q: %v (tool description: %s)", e.Tool, e.Err, e.Description)
}

// Unwrap returns the underlying validation failure.
func (e *ValidationError) Unwrap() error { return e.Err }

// Is reports whether target is ErrValidation.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// ExecutionError reports a failure inside the tool body after the input
// passed validation. It matches ErrExecution via errors.Is.
type ExecutionError struct {
	Tool string
	Err  error
}

// Error returns a human-readable description of the execution failure.
func (e *ExecutionError) Error() string {
	return fmt.Sprintf("tool %q: %v", e.Tool, e.Err)
}

// Unwrap returns the underlying execution failure.
func (e *ExecutionError) Unwrap() error { return e.Err }

// Is reports whether target is ErrExecution.
func (e *ExecutionError) Is(target error) bool { return target == ErrExecution }

// Sentinel errors at the boundary. Validation fires before the tool
// body; execution fires after.
var (
	ErrValidation = errors.New("tool input validation failed")
	ErrExecution  = errors.New("tool execution failed")
)

// Registry is the platform's tool registry. It walks the operator's
// tools directory at startup, loads each manifest, validates the
// schemas, pins the SHA-256 of the canonical manifest bytes, caches the
// sanitizer's view of the input and output schemas, and exposes the
// tool set as descriptors (ADR 0009 §Decision). Call dispatches one
// tool invocation: validate the input against the cached input schema,
// run the stdio runner, validate the output against the cached output
// schema. The cached schemas match the model-facing surface the
// sanitizer produces, so whatever the model sees the runtime accepts
// (issue #319; sanitizer #318).
type Registry struct {
	logger *slog.Logger
	dir    string

	mu    sync.RWMutex
	tools map[string]*toolInstance
	// pins records the manifest hash the registry serves for each
	// tool. The operator can supply a pin set at startup; a tool
	// whose hash drifts from the pin is rejected.
	pins ManifestPins
	// sanitize, when non-nil, reshapes each tool's input and output
	// schemas before caching them. The cached form is what the
	// runtime's validation uses; with nil, the cached form equals
	// the manifest's schema.
	sanitize func(name string, raw json.RawMessage) json.RawMessage
}

type toolInstance struct {
	manifest        Manifest
	sanitizedInput  json.RawMessage
	sanitizedOutput json.RawMessage
	runner          *Runner
	hash            string
}

// ManifestPins is the operator's drift-check set: the SHA-256 of the
// canonical manifest bytes for each tool the operator has accepted. A
// tool whose manifest hash is not in the set is rejected at startup.
// The legacy protocol client uses the same shape but its own type
// alias; the two types are interchangeable at runtime.
type ManifestPins map[string]string

// RegistryOptions configures NewRegistry. The zero value is usable:
// empty Logger falls back to slog.Default(), empty Dir falls back to
// DefaultToolsDir, nil ManifestPins disables drift pinning.
type RegistryOptions struct {
	Logger *slog.Logger
	Dir    string
	Pins   ManifestPins
	// Required is the set of tool names the registry must hold at
	// startup. A nil slice uses DefaultRequiredTools; an explicit
	// slice (including empty) overrides the default.
	Required []string
	// Sanitize optionally rewrites a tool's input and output schemas
	// into the model-facing form the runtime's validation should
	// match. The manifest remains the source of truth; the sanitizer
	// reshapes each schema (root type forced to object, strict-mode
	// keywords dropped, every properties key promoted into required
	// with a nullable type). Nil disables sanitization — the cached
	// schemas equal the manifest's and runtime validation behaves as
	// before this option existed. Issue #319.
	Sanitize func(name string, raw json.RawMessage) json.RawMessage
}

// NewRegistry walks dir, loads every manifest, validates the contract,
// pins the SHA-256 of the canonical manifest bytes, and sets the
// required-tool set. A missing required tool fails closed (ADR 0009
// invariant 5).
func NewRegistry(opts RegistryOptions) (*Registry, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	dir := opts.Dir
	if dir == "" {
		dir = DefaultToolsDir
	}
	required := opts.Required
	if required == nil {
		required = append([]string(nil), DefaultRequiredTools...)
	}

	reg := &Registry{
		logger:   logger,
		dir:      dir,
		tools:    make(map[string]*toolInstance),
		pins:     opts.Pins,
		sanitize: opts.Sanitize,
	}
	if err := reg.loadTools(dir); err != nil {
		return nil, err
	}
	if err := reg.checkRequired(required); err != nil {
		return nil, err
	}
	return reg, nil
}

// loadTools walks dir and registers every entry that contains a
// manifest.json. A directory without a manifest is silently skipped;
// a broken manifest fails startup. Each registered tool caches the
// sanitized form of its input and output schemas (when the registry
// was constructed with a Sanitizer) so runtime validation matches
// the model-facing surface.
func (r *Registry) loadTools(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("tools directory %q does not exist", dir)
		}
		return fmt.Errorf("read tools directory %q: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		toolDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(toolDir, "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %s: %w", manifestPath, err)
		}
		loaded, err := loadManifestDir(toolDir)
		if err != nil {
			return fmt.Errorf("load %s: %w", manifestPath, err)
		}
		if _, exists := r.tools[loaded.manifest.Name]; exists {
			return fmt.Errorf("duplicate tool name %q", loaded.manifest.Name)
		}
		if r.pins != nil {
			pinned, ok := r.pins[loaded.manifest.Name]
			if ok && pinned != loaded.hash {
				return fmt.Errorf("descriptor drift for %q: pinned=%s fetched=%s",
					loaded.manifest.Name, pinned, loaded.hash)
			}
		}
		runner := NewRunner(loaded.manifest, toolDir)
		sanInput, sanOutput := loaded.manifest.InputSchema, loaded.manifest.OutputSchema
		if r.sanitize != nil {
			sanInput = r.sanitize(loaded.manifest.Name, loaded.manifest.InputSchema)
			sanOutput = r.sanitize(loaded.manifest.Name, loaded.manifest.OutputSchema)
		}
		r.tools[loaded.manifest.Name] = &toolInstance{
			manifest:        loaded.manifest,
			sanitizedInput:  sanInput,
			sanitizedOutput: sanOutput,
			runner:          runner,
			hash:            loaded.hash,
		}
		r.logger.Info("tool manifest loaded",
			"op", "tool.load",
			"name", loaded.manifest.Name,
			"hash", loaded.hash,
		)
	}
	return nil
}

// checkRequired fails when the required tool set is not a subset of
// the loaded tools.
func (r *Registry) checkRequired(required []string) error {
	missing := make([]string, 0)
	for _, name := range required {
		if _, ok := r.tools[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("required tools missing: %v", missing)
	}
	return nil
}

// List returns the descriptors of every installed tool, sorted by name.
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, descriptorFromTool(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the descriptor for one tool, or false if the tool is not
// installed. Call sites that need a single descriptor (e.g. the
// session's per-tool description lookup) use it to avoid scanning
// List().
func (r *Registry) Get(name string) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return Descriptor{}, false
	}
	return descriptorFromTool(t), true
}

// PinsExport returns the manifest hash for every loaded tool. The
// operator regenerates the pin set from this map on a deliberate
// manifest change.
func (r *Registry) PinsExport() ManifestPins {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(ManifestPins, len(r.tools))
	for name, t := range r.tools {
		out[name] = t.hash
	}
	return out
}

// Close releases every tool's resources. The stdio runner is stateless;
// Close is a no-op today but is part of the client surface so future
// long-lived tool resources have a shutdown hook.
func (r *Registry) Close() error {
	return nil
}

// Call validates the input against the cached input schema, dispatches
// the stdio runner, and validates the output against the cached output
// schema. The cached schemas match the model-facing surface the
// sanitizer produces, so the model can emit null for the fields the
// sanitizer marks nullable and the runtime accepts it. The output is
// also checked for an error field (ADR 0009 §Decision: a tool may
// report a failure as a JSON object with an "error" key); on success
// the validated output is returned.
func (r *Registry) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q: not installed", name)
	}
	if err := validate(t.sanitizedInput, input); err != nil {
		return nil, &ValidationError{Tool: name, Description: t.manifest.Description, Err: err}
	}
	response, stderr, runErr := t.runner.Run(ctx, input)
	if runErr != nil {
		return nil, &ExecutionError{Tool: name, Err: joinStderr(runErr, stderr)}
	}
	if msg, ok := ReadErrorObject(response); ok {
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("tool reported error: %s", msg)}
	}
	if err := validate(t.sanitizedOutput, response); err != nil {
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("output failed schema validation (tool quarantined): %w", err)}
	}
	return response, nil
}

// descriptorFromTool projects a tool instance into the model-facing
// descriptor. The schemas exposed are the cached sanitized forms when
// the registry was constructed with a sanitizer, otherwise the
// manifest's schemas. The model-facing surface and the runtime's
// validation surface are the same shape; whichever the model sees, the
// runtime accepts.
func descriptorFromTool(t *toolInstance) Descriptor {
	return Descriptor{
		Name:         t.manifest.Name,
		Description:  t.manifest.Description,
		InputSchema:  t.sanitizedInput,
		OutputSchema: t.sanitizedOutput,
		Hash:         t.hash,
	}
}

// joinStderr annotates the runner error with the captured stderr when
// stderr carries any text. The runner returns the exit status as the
// leading error; the stderr text is the tool's own diagnostic for the
// failure (ADR 0009 §Decision: the platform surfaces stderr in the
// tool-failure event).
func joinStderr(runErr error, stderr string) error {
	stderr = strings.TrimRight(stderr, "\r\n")
	if stderr == "" {
		return runErr
	}
	return fmt.Errorf("%w (stderr: %s)", runErr, stderr)
}

// ManifestHashOfFile reads and hashes the manifest at path. The
// integration test uses it to compute the pin set before the registry
// starts; the registry itself recomputes the same hash when it loads
// the manifest.
func ManifestHashOfFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ManifestHash(raw), nil
}
