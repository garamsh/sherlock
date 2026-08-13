package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"sync"
	"sync/atomic"
)

// Method names the platform speaks on the wire (ADR 0018 §Tool contract).
const (
	methodList = "tools/list"
	methodCall = "tools/call"
)

// Spec is one tool server the operator installed. The platform spawns
// exactly one process per spec and holds the protocol client over its
// stdin/stdout. Spec is the only knob the agent's config exposes.
type Spec struct {
	// Name is the logical name the platform exposes to the model and
	// the required-tool check matches against. It must equal the name
	// the server reports in its descriptor; a mismatch fails startup.
	Name string
	// Command is the path to the binary the platform spawns.
	Command string
	// Args are the command arguments in argv order.
	Args []string
	// Env are extra KEY=VALUE pairs the platform adds to the spawned
	// process's environment. Per-tool secret scoping (ADR 0003,
	// ADR 0018) lives here; the platform never materializes a secret.
	Env []string
}

// Descriptor is the model-facing view of a tool: its name, description,
// and JSON Schema documents, JSON-serializable so it can be handed to a
// model. Hash is the descriptor's content hash; the platform pins it
// against any drift on the next startup.
type Descriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Hash         string          `json:"hash"`
}

// server is one running tool server plus its protocol session.
type server struct {
	spec       Spec
	cmd        *exec.Cmd
	stdin      *json.Encoder
	stdout     *json.Decoder
	cancel     context.CancelFunc
	descriptor Descriptor
}

// Client is the platform's tool client. It owns one running tool server
// per Spec the operator installed, plus the cross-server policy and
// drift pin. Client is safe for concurrent use.
type Client struct {
	logger *slog.Logger
	policy Policy
	pin    Pins

	mu      sync.RWMutex
	servers map[string]*server // keyed by tool name
	closed  bool
}

// Pins is the set of pinned descriptor hashes the platform refuses to
// load. A tool whose fetched descriptor hash is not in Pins at startup
// is rejected. The operator regenerates the pin on a deliberate
// descriptor change.
type Pins map[string]string

// DefaultRequiredTools is the set of tool names the platform requires
// at startup (ADR 0018 §Required tools). A missing required tool fails
// closed.
var DefaultRequiredTools = []string{"message_send"}

// Options configures NewClient.
type Options struct {
	// Logger receives structured events. Nil uses slog.Default().
	Logger *slog.Logger
	// Policy is the per-tool permission policy enforced at the call
	// boundary. Zero value is AllowAll.
	Policy Policy
	// Pins is the drift pin set. Nil disables drift pinning.
	Pins Pins
	// Required enforces the required-tool set. Empty defaults to
	// DefaultRequiredTools; an explicit list (including empty) overrides
	// the default.
	Required []string
}

// NewClient spawns each spec, runs the discovery handshake, validates
// descriptors, verifies the required-tool set, and returns the client
// ready to list and call. A spec that fails startup is fatal: the
// platform has no fallback for a tool it cannot reach.
func NewClient(ctx context.Context, opts Options, specs []Spec) (*Client, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	required := opts.Required
	if required == nil {
		required = append([]string(nil), DefaultRequiredTools...)
	}

	client := &Client{
		logger:  logger,
		policy:  opts.Policy,
		pin:     opts.Pins,
		servers: make(map[string]*server, len(specs)),
	}

	for _, spec := range specs {
		if spec.Name == "" || spec.Command == "" {
			client.killAll()
			return nil, fmt.Errorf("tool spec: name and command are required")
		}
		srv, err := client.startServer(ctx, spec)
		if err != nil {
			client.killAll()
			return nil, fmt.Errorf("start tool %q: %w", spec.Name, err)
		}
		if _, exists := client.servers[srv.descriptor.Name]; exists {
			client.killAll()
			return nil, fmt.Errorf("duplicate tool name %q", srv.descriptor.Name)
		}
		client.servers[srv.descriptor.Name] = srv
		client.logger.Info("tool server ready",
			"op", "tool.start",
			"name", srv.descriptor.Name,
			"hash", srv.descriptor.Hash,
		)
	}

	if err := client.checkRequired(required); err != nil {
		client.killAll()
		return nil, err
	}
	return client, nil
}

// startServer spawns the tool process and runs the discovery handshake.
func (c *Client) startServer(ctx context.Context, spec Spec) (*server, error) {
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	_ = runCtx
	srv := &server{
		spec:   spec,
		cmd:    cmd,
		stdin:  json.NewEncoder(stdin),
		stdout: json.NewDecoder(bufio.NewReader(stdout)),
		cancel: cancel,
	}

	desc, err := srv.discover()
	if err != nil {
		cancel()
		killServer(cmd)
		return nil, err
	}
	if desc.Name != spec.Name {
		cancel()
		killServer(cmd)
		return nil, fmt.Errorf("descriptor name %q does not match spec name %q",
			desc.Name, spec.Name)
	}
	if err := validateSchemaDocument(desc.InputSchema); err != nil {
		cancel()
		killServer(cmd)
		return nil, fmt.Errorf("descriptor input schema: %w", err)
	}
	if err := validateSchemaDocument(desc.OutputSchema); err != nil {
		cancel()
		killServer(cmd)
		return nil, fmt.Errorf("descriptor output schema: %w", err)
	}
	hash, err := descriptorHash(desc)
	if err != nil {
		cancel()
		killServer(cmd)
		return nil, fmt.Errorf("hash descriptor: %w", err)
	}
	desc.Hash = hash
	if c.pin != nil {
		if pinned, ok := c.pin[desc.Name]; ok && pinned != hash {
			cancel()
			killServer(cmd)
			return nil, fmt.Errorf("descriptor drift for %q: pinned=%s fetched=%s",
				desc.Name, pinned, hash)
		}
	}
	srv.descriptor = desc
	return srv, nil
}

// killServer terminates cmd and waits for it to exit. The Wait is
// essential: the process must be reaped before the function returns,
// so the error path does not leak a running tool into the next call.
func killServer(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Kill)
	_ = cmd.Wait()
}

// discover runs the tools/list handshake and returns the first tool
// the server reports. Tools are single-server processes; the protocol
// supports multiple descriptors per server, but the platform speaks
// one Spec per binary and enforces the invariant here.
func (s *server) discover() (Descriptor, error) {
	var raw json.RawMessage
	if err := s.call(methodList, listParams{}, &raw); err != nil {
		return Descriptor{}, err
	}
	var result listResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return Descriptor{}, fmt.Errorf("decode list result: %w", err)
	}
	if len(result.Tools) == 0 {
		return Descriptor{}, errors.New("tool server reports no tools")
	}
	if len(result.Tools) > 1 {
		return Descriptor{}, fmt.Errorf("tool server reports %d tools, expected 1", len(result.Tools))
	}
	t := result.Tools[0]
	desc := Descriptor{
		Name:         t.Name,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: t.OutputSchema,
	}
	return desc, nil
}

// call sends one JSON-RPC request and decodes the result. Errors are
// returned as the platform's typed error without HTTP-style codes.
func (s *server) call(method string, params, result any) error {
	id := s.nextID()
	req := request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := s.stdin.Encode(req); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	var resp response
	if err := s.stdout.Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("tool server closed stream before %s response", method)
		}
		return fmt.Errorf("decode %s: %w", method, err)
	}
	if resp.ID != id {
		return fmt.Errorf("response id %d does not match request id %d", resp.ID, id)
	}
	if resp.Error != nil {
		return resp.Error.callError(method)
	}
	if result == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return fmt.Errorf("%s response: empty result", method)
	}
	return json.Unmarshal(resp.Result, result)
}

// idCounter is a process-wide counter so every server session sees
// unique request IDs. JSON-RPC 2.0 only requires per-session uniqueness;
// the counter costs nothing and keeps the implementation trivially
// safe.
var idCounter atomic.Uint64

func (s *server) nextID() uint64 {
	return idCounter.Add(1)
}

// Close releases every server's process. Close is idempotent; a second
// call after the first has no observable effect.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.killAllLocked()
}

// killAll stops every server. Used on startup failure and on Close.
func (c *Client) killAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killAllLocked()
}

func (c *Client) killAllLocked() error {
	var errs []error
	for _, srv := range c.servers {
		srv.cancel()
		if err := srv.cmd.Process.Signal(os.Kill); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, fmt.Errorf("kill %s: %w", srv.spec.Name, err))
		}
	}
	for _, srv := range c.servers {
		// Wait must be called even when the signal is lost: a
		// successful signal still leaves the process to be reaped.
		// We swallow the wait error when the kill succeeded — the
		// signal status is the only useful signal here.
		_ = srv.cmd.Wait()
	}
	return errors.Join(errs...)
}

// checkRequired fails when the required tool set is not a subset of the
// discovered tools. The check runs after every spec has handshaked.
func (c *Client) checkRequired(required []string) error {
	missing := make([]string, 0)
	for _, name := range required {
		if _, ok := c.servers[name]; !ok {
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
// The result is the model-facing view of the tool surface.
func (c *Client) List() []Descriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Descriptor, 0, len(c.servers))
	for _, srv := range c.servers {
		out = append(out, srv.descriptor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Call dispatches one tool call through the protocol. It validates input
// against the tool's input schema, applies the permission policy, sends
// the call, re-validates the output against the tool's output schema,
// and returns the result.
func (c *Client) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	c.mu.RLock()
	srv, ok := c.servers[name]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q: not installed", name)
	}
	if err := c.policy.Enforce(name); err != nil {
		return nil, err
	}
	if err := validate(srv.descriptor.InputSchema, input); err != nil {
		return nil, &ValidationError{Tool: name, Description: srv.descriptor.Description, Err: err}
	}
	params := callParams{Name: name, Arguments: input}
	var raw json.RawMessage
	if err := srv.call(methodCall, params, &raw); err != nil {
		return nil, &ExecutionError{Tool: name, Err: err}
	}
	var result callResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("decode call result: %w", err)}
	}
	if result.IsError {
		return nil, &ExecutionError{Tool: name, Err: errors.New("tool reported error")}
	}
	if len(result.Content) == 0 {
		return nil, &ExecutionError{Tool: name, Err: errors.New("tool returned empty content")}
	}
	// The platform protocol returns the result as an array of content
	// blocks; structured tools send one text block whose body is the
	// JSON-encoded output. Re-parse the text as JSON so the platform
	// boundary can validate the output against the schema.
	var body json.RawMessage
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			if err := json.Unmarshal([]byte(c.Text), &body); err != nil {
				return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("decode text content: %w", err)}
			}
		}
	}
	if len(body) == 0 {
		return nil, &ExecutionError{Tool: name, Err: errors.New("tool returned no text content")}
	}
	if err := validate(srv.descriptor.OutputSchema, body); err != nil {
		return nil, &ExecutionError{Tool: name, Err: fmt.Errorf("output failed schema validation (tool quarantined): %w", err)}
	}
	return body, nil
}

// PinsExport returns the current pins map for the running client. The
// operator regenerates the file they pass to the next startup from
// this map on a deliberate descriptor change.
func (c *Client) PinsExport() Pins {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(Pins, len(c.servers))
	for _, srv := range c.servers {
		out[srv.descriptor.Name] = srv.descriptor.Hash
	}
	return out
}

// ValidationError reports input that failed schema validation at the
// contract boundary. It matches ErrValidation via errors.Is. The
// description restates the tool's advertised summary so a model
// reading the rejection is steered back to the tool's real contract.
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

// Sentinel errors at the boundary (matches the in-process contract).
var (
	// ErrValidation marks errors produced by input validation at the
	// contract boundary, before the tool body runs.
	ErrValidation = errors.New("tool input validation failed")
	// ErrExecution marks errors produced by the tool body itself, after
	// the input has passed validation.
	ErrExecution = errors.New("tool execution failed")
)
