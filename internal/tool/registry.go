package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DefaultToolsDir is the operator's tools directory when the agent's
// config does not override it.
const DefaultToolsDir = "./tools"

// DefaultRequiredTools is the set of tool names the platform requires
// at startup (ADR 0009 §Decision, invariant 5). A missing required tool
// fails closed.
var DefaultRequiredTools = []string{"message_send"}

// Registry is the platform's tool registry. It walks the operator's
// tools directory at startup, loads each manifest, validates the
// schemas, hashes the canonical manifest bytes against the operator's
// pin set, and exposes the tool set as descriptors (ADR 0009
// §Decision). Call dispatches one tool invocation: validate the input
// against the manifest's input schema, run the stdio runner, validate
// the output against the manifest's output schema (ADR 0009
// invariants 3 and 4). The manifest is the contract on both gates; the
// model-facing projection of a schema belongs to the model provider,
// not to this boundary.
type Registry struct {
	logger *slog.Logger
	dir    string

	mu    sync.RWMutex
	tools map[string]*toolInstance
	// pins is the operator's accepted tool set. A nil set leaves
	// drift pinning off; a non-nil set is enforced in full.
	pins ManifestPins
}

type toolInstance struct {
	manifest Manifest
	runner   *Runner
	hash     string
}

// RegistryOptions configures NewRegistry. The zero value is usable:
// empty Logger falls back to slog.Default(), empty Dir falls back to
// DefaultToolsDir.
type RegistryOptions struct {
	Logger *slog.Logger
	Dir    string
	// Pins is the operator's accepted tool set (see ManifestPins). A
	// nil set leaves drift pinning off and NewRegistry warns, because
	// the operator then has no record to check a mutated manifest
	// against.
	Pins ManifestPins
	// Required is the set of tool names the registry must hold at
	// startup. A nil slice uses DefaultRequiredTools; an explicit
	// slice (including empty) overrides the default.
	Required []string
}

// NewRegistry walks dir, loads every manifest, validates the contract,
// checks each manifest hash against the operator's pin set, and sets
// the required-tool set. A missing required tool fails closed (ADR 0009
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
		logger: logger,
		dir:    dir,
		tools:  make(map[string]*toolInstance),
		pins:   opts.Pins,
	}
	if err := reg.loadTools(dir); err != nil {
		return nil, err
	}
	logger.Info("tool registry loaded",
		"op", "tool.load",
		"tools_dir", dir,
		"tools_dir_resolved", resolvedToolsDir(dir),
		"tools", len(reg.tools),
	)
	if opts.Pins == nil {
		// The hash of every loaded manifest is on the lines above
		// this one, which is where the operator copies the pin set
		// from.
		logger.Warn("tool drift pinning disabled: no pin set configured",
			"op", "tool.load",
			"tools_dir", dir,
		)
	}
	if err := reg.checkRequired(required); err != nil {
		return nil, err
	}
	return reg, nil
}

// loadTools walks dir and registers every entry that contains a
// manifest.json. A directory without a manifest is silently skipped;
// a broken manifest fails startup. When the operator supplied a pin
// set, every manifest is checked against it before the tool is
// registered.
func (r *Registry) loadTools(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("tools directory %q does not exist (resolved to %s)", dir, resolvedToolsDir(dir))
		}
		return fmt.Errorf("read tools directory %q (resolved to %s): %w", dir, resolvedToolsDir(dir), err)
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
		if err := r.checkPin(loaded.manifest.Name, loaded.hash); err != nil {
			return err
		}
		r.tools[loaded.manifest.Name] = &toolInstance{
			manifest: loaded.manifest,
			runner:   NewRunner(loaded.manifest, toolDir),
			hash:     loaded.hash,
		}
		r.logger.Info("tool manifest loaded",
			"op", "tool.load",
			"name", loaded.manifest.Name,
			"hash", loaded.hash,
		)
	}
	return nil
}

// resolvedToolsDir is the absolute path dir names, so a message says
// where the registry looked and not only what was configured: a relative
// dir resolves against the process working directory. Where that
// directory is unreadable there is nothing to resolve against, and the
// marker says so rather than repeating dir.
func resolvedToolsDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Sprintf("<unresolved: %v>", err)
	}
	return abs
}

// checkPin holds the loaded manifest against the operator's accepted
// tool set (ADR 0009 invariant 2). A nil set is the operator declining
// to pin. A non-nil set is the whole accepted set, so a tool it does
// not name is refused for the same reason a drifted one is: neither
// hash is one the operator accepted. Both errors carry the hash the
// operator pins to accept what is on disk.
func (r *Registry) checkPin(name, hash string) error {
	if r.pins == nil {
		return nil
	}
	pinned, ok := r.pins[name]
	if !ok {
		return fmt.Errorf("unpinned tool %q: not named by the pin set; pin it as %s or remove the tool", name, hash)
	}
	if pinned != hash {
		return fmt.Errorf("descriptor drift for %q: pinned=%s fetched=%s", name, pinned, hash)
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

// Call validates the input against the manifest's input schema,
// dispatches the stdio runner, and validates the output against the
// manifest's output schema (ADR 0009 invariants 3 and 4). The manifest
// is the contract on both gates. The input is read through
// absentOptionalNulls first, so a null the model emits for a property
// the manifest leaves optional reads as absence rather than as a type
// violation (issue #319); the tool is handed that same validated
// document. The output is also checked for an error field (ADR 0009
// §Decision: a tool may report a failure as a JSON object with an
// "error" key); on success the validated output is returned.
func (r *Registry) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q: not installed", name)
	}
	input = absentOptionalNulls(t.manifest.InputSchema, input)
	if err := validate(t.manifest.InputSchema, input); err != nil {
		return nil, &ValidationError{Tool: name, Description: t.manifest.Description, Err: err}
	}
	response, stderr, runErr := t.runner.Run(ctx, input)
	if runErr != nil {
		return nil, &ExecutionError{Tool: name, Err: joinStderr(runErr, stderr)}
	}
	if msg, ok := ReadErrorObject(response); ok {
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("tool reported error: %s", msg)}
	}
	if err := validate(t.manifest.OutputSchema, response); err != nil {
		// The call fails and the output is dropped; the tool stays
		// installed and callable. What the boundary gives is that a
		// non-conforming output never reaches the model, not that a
		// tool which emits one is taken out of service.
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("output failed schema validation, so the call failed and the output was dropped: %w", err)}
	}
	return response, nil
}

// descriptorFromTool projects a tool instance into the model-facing
// descriptor: the manifest's own schemas, without the runner's command,
// env, or on-disk path (ADR 0009 §Decision). A provider whose API
// constrains the schema shape projects it further at its own boundary
// (internal/agent/openai) — that projection is the provider's, and it
// never becomes the contract this registry enforces.
func descriptorFromTool(t *toolInstance) Descriptor {
	return Descriptor{
		Name:         t.manifest.Name,
		Description:  t.manifest.Description,
		InputSchema:  t.manifest.InputSchema,
		OutputSchema: t.manifest.OutputSchema,
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
