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
)

// Discover lists the descriptors of the registry served at baseURL (see
// NewHandler), so its tools can join a local registry as remote tools.
func Discover(ctx context.Context, baseURL string) ([]Descriptor, error) {
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
// any other tool.
func NewRemoteTool(desc Descriptor, baseURL string) Tool {
	return &remoteTool{desc: desc, baseURL: strings.TrimSuffix(baseURL, "/")}
}

type remoteTool struct {
	desc    Descriptor
	baseURL string
}

func (t *remoteTool) Name() string                 { return t.desc.Name }
func (t *remoteTool) Description() string          { return t.desc.Description }
func (t *remoteTool) InputSchema() json.RawMessage { return t.desc.InputSchema }
func (t *remoteTool) OutputSchema() json.RawMessage {
	return t.desc.OutputSchema
}

func (t *remoteTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/tools/"+t.desc.Name+"/call", bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("call %q: %w", t.desc.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %q: %w", t.desc.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeWireError(resp)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("call %q: read output: %w", t.desc.Name, err)
	}
	return json.RawMessage(out), nil
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
