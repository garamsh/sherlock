package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultCallTimeout bounds one contract request — a Discover listing or
// a remote tool Call — so a server that accepts the connection but never
// responds cannot stall a turn indefinitely (the client-side counterpart
// of the shell executor's exec-timeout).
const DefaultCallTimeout = 30 * time.Second

// Discover lists the descriptors of the registry served at baseURL (see
// NewHandler), so its tools can join a local registry as remote tools.
func Discover(ctx context.Context, baseURL string) ([]Descriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(baseURL, "/")+"/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeWireError(resp)
	}
	var descs []Descriptor
	if err := json.NewDecoder(resp.Body).Decode(&descs); err != nil {
		return nil, fmt.Errorf("discover: decode descriptors: %w", err)
	}
	return descs, nil
}

// NewRemoteTool returns a Tool backed by the registry served at baseURL
// (see NewHandler): Call forwards the input over the wire and reconstructs
// the server's typed errors, so a remote tool joins a local registry like
// any other tool. Every call carries a bounded deadline —
// DefaultCallTimeout unless WithCallTimeout overrides it.
func NewRemoteTool(desc Descriptor, baseURL string, opts ...RemoteOption) Tool {
	t := &remoteTool{
		desc:    desc,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		timeout: DefaultCallTimeout,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RemoteOption customizes a remote tool.
type RemoteOption func(*remoteTool)

// WithCallTimeout overrides DefaultCallTimeout for the tool's calls. A
// non-positive value keeps the default.
func WithCallTimeout(d time.Duration) RemoteOption {
	return func(t *remoteTool) {
		if d > 0 {
			t.timeout = d
		}
	}
}

type remoteTool struct {
	desc    Descriptor
	baseURL string
	timeout time.Duration
}

func (t *remoteTool) Name() string                 { return t.desc.Name }
func (t *remoteTool) Description() string          { return t.desc.Description }
func (t *remoteTool) InputSchema() json.RawMessage { return t.desc.InputSchema }
func (t *remoteTool) OutputSchema() json.RawMessage {
	return t.desc.OutputSchema
}

func (t *remoteTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/tools/"+t.desc.Name+"/call", bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("call %q: %w", t.desc.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, t.callError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeWireError(resp)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, t.callError(fmt.Errorf("read output: %w", err))
	}
	return json.RawMessage(out), nil
}

// callError wraps a transport failure: an expired per-call deadline is a
// typed execution failure the model can read and recover from, never a
// validation failure; anything else stays an ordinary wrapped error.
func (t *remoteTool) callError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &ExecutionError{
			Tool: t.desc.Name,
			Err:  fmt.Errorf("call timed out after %s", t.timeout),
		}
	}
	return fmt.Errorf("call %q: %w", t.desc.Name, err)
}

// decodeWireError reconstructs the typed error a wireError envelope carries,
// preserving the error kind across the wire.
func decodeWireError(resp *http.Response) error {
	var we wireError
	if err := json.NewDecoder(resp.Body).Decode(&we); err != nil {
		return fmt.Errorf("remote call: status %d, undecodable error body", resp.StatusCode)
	}
	switch we.Kind {
	case wireValidation:
		return &ValidationError{Tool: we.Tool, Description: we.Description, Err: errors.New(we.Message)}
	case wireExecution:
		return &ExecutionError{Tool: we.Tool, Err: errors.New(we.Message)}
	case wireNotFound:
		return fmt.Errorf("%s: %w", we.Message, ErrToolNotFound)
	default:
		return fmt.Errorf("remote call: %s: %s", we.Kind, we.Message)
	}
}
