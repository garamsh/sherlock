// image_generate generates one image over the script-tool runner's stdio
// protocol (ADR 0009 §Decision) and writes it into the agent's workspace
// through the workspace contract, as file_write does. It calls an image
// endpoint of the OpenRouter shape, POST {IMAGE_API_BASE}/images, with the
// model IMAGE_MODEL names. The key comes from the file IMAGE_API_KEY_FILE
// names, read on every call, or from IMAGE_API_KEY. Neither the key nor
// the image bytes reach stdout.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/sherlock/internal/workspace"
)

const (
	defaultAPIBase = "https://openrouter.ai/api/v1"
	defaultModel   = "recraft/recraft-v4.1-flash"
)

// requestTimeout bounds one call to the image endpoint end to end. One
// generation is a single request whose answer arrives when the image is
// finished, which takes from seconds to well over a minute depending on
// the model; nothing else bounds the call (security.md, "An outbound call
// a tool makes is bounded by the tool that makes it").
const requestTimeout = 5 * time.Minute

// writeTimeout bounds the workspace Write, a pod-local filesystem call.
const writeTimeout = 10 * time.Second

// maxResponseBytes caps how much of the endpoint's answer is read. A
// base64 image is a third larger than the image itself.
const maxResponseBytes = 64 << 20

// maxErrorBody caps how much of an error answer an error message carries.
const maxErrorBody = 512

var httpClient = &http.Client{Timeout: requestTimeout}

type callInput struct {
	Prompt string `json:"prompt"`
	Path   string `json:"path"`
}

type callOutput struct {
	Path      string   `json:"path"`
	MediaType string   `json:"media_type"`
	CostUSD   *float64 `json:"cost_usd"`
}

// settings is the operator's configuration, read from the environment.
type settings struct {
	base    string
	model   string
	key     string
	keyFile string
	addr    string
}

type imagesResponse struct {
	Data []struct {
		B64JSON   string `json:"b64_json"`
		MediaType string `json:"media_type"`
	} `json:"data"`
	Usage struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	// Error is set on a 200 answer whose generation failed after the
	// endpoint had already sent its status.
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "image_generate:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := readSettings()
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	dec.DisallowUnknownFields()
	var in callInput
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("decode input: %v", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("input must be one JSON object")
	}
	if in.Prompt == "" || in.Path == "" {
		return errors.New("prompt and path are required")
	}
	conn, err := grpc.NewClient(cfg.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("workspace unreachable: dial %s: %v", cfg.addr, err)
	}
	defer conn.Close()
	out, err := generate(context.Background(), cfg, workspace.NewWorkspaceClient(conn), in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func readSettings() (settings, error) {
	cfg := settings{
		base:    os.Getenv("IMAGE_API_BASE"),
		model:   os.Getenv("IMAGE_MODEL"),
		key:     os.Getenv("IMAGE_API_KEY"),
		keyFile: os.Getenv("IMAGE_API_KEY_FILE"),
		addr:    os.Getenv("SHERLOCK_WORKSPACE_ADDR"),
	}
	if cfg.base == "" {
		cfg.base = defaultAPIBase
	}
	if cfg.model == "" {
		cfg.model = defaultModel
	}
	switch {
	case cfg.addr == "":
		return settings{}, errors.New("SHERLOCK_WORKSPACE_ADDR is required")
	case cfg.key != "" && cfg.keyFile != "":
		return settings{}, errors.New("IMAGE_API_KEY and IMAGE_API_KEY_FILE are both set; set one")
	case cfg.key == "" && cfg.keyFile == "":
		return settings{}, errors.New("IMAGE_API_KEY_FILE or IMAGE_API_KEY is required")
	}
	return cfg, nil
}

// generate asks the endpoint for one image and writes it to in.Path. The
// workspace writes through a temporary file and a rename, so a call that
// fails leaves whatever was at the path before.
func generate(ctx context.Context, cfg settings, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]string{"model": cfg.model, "prompt": in.Prompt})
	if err != nil {
		return callOutput{}, err
	}
	answer, err := post(ctx, cfg, body)
	if err != nil {
		return callOutput{}, err
	}
	if answer.Error != nil {
		return callOutput{}, fmt.Errorf("endpoint error: %s", answer.Error.Message)
	}
	if len(answer.Data) == 0 || answer.Data[0].B64JSON == "" {
		return callOutput{}, fmt.Errorf("model refused: %s returned no image", cfg.model)
	}
	image, err := base64.StdEncoding.DecodeString(answer.Data[0].B64JSON)
	if err != nil {
		return callOutput{}, fmt.Errorf("endpoint error: decode image: %v", err)
	}
	mediaType := answer.Data[0].MediaType
	if mediaType == "" {
		mediaType = http.DetectContentType(image)
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if _, err := ws.Write(writeCtx, &workspace.WriteRequest{Path: in.Path, Content: image}); err != nil {
		return callOutput{}, workspaceError(err)
	}
	return callOutput{Path: in.Path, MediaType: mediaType, CostUSD: answer.Usage.Cost}, nil
}

// post sends one generation request. A key file the endpoint answers with
// 401 is read again and the request sent once more, so a key rewritten
// since the first read recovers the call, as the chat model's does (ADR
// 0029). A key from IMAGE_API_KEY is not retried.
func post(ctx context.Context, cfg settings, body []byte) (imagesResponse, error) {
	resp, err := send(ctx, cfg, body)
	if err == nil && resp.StatusCode == http.StatusUnauthorized && cfg.keyFile != "" {
		_ = resp.Body.Close()
		resp, err = send(ctx, cfg, body)
	}
	if err != nil {
		return imagesResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return imagesResponse{}, fmt.Errorf("endpoint error: read answer: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return imagesResponse{}, statusError(cfg.model, resp.StatusCode, raw)
	}
	var answer imagesResponse
	if err := json.Unmarshal(raw, &answer); err != nil {
		return imagesResponse{}, fmt.Errorf("endpoint error: decode answer: %v", err)
	}
	return answer, nil
}

func send(ctx context.Context, cfg settings, body []byte) (*http.Response, error) {
	key, err := readKey(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(cfg.base, "/")+"/images", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("endpoint error: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint error: %v", err)
	}
	return resp, nil
}

// readKey returns the key to send now: the key file's content, trimmed,
// or IMAGE_API_KEY.
func readKey(cfg settings) (string, error) {
	if cfg.keyFile == "" {
		return cfg.key, nil
	}
	raw, err := os.ReadFile(cfg.keyFile)
	if err != nil {
		return "", fmt.Errorf("read IMAGE_API_KEY_FILE: %v", err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("read IMAGE_API_KEY_FILE: %s is empty", cfg.keyFile)
	}
	return key, nil
}

// statusError names a non-2xx answer. 403 and 404 are the endpoint
// declining the request it understood — a model it does not serve or will
// not run, or a prompt its policy refuses — so they are the model's
// refusal; every other status is the endpoint's own error.
func statusError(model string, code int, raw []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	detail := string(raw)
	if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Message != "" {
		detail = envelope.Error.Message
	}
	if len(detail) > maxErrorBody {
		detail = detail[:maxErrorBody]
	}
	if code == http.StatusForbidden || code == http.StatusNotFound {
		return fmt.Errorf("model refused: %s: HTTP %d: %s", model, code, detail)
	}
	return fmt.Errorf("endpoint error: HTTP %d: %s", code, detail)
}

// workspaceError names a failed Write: no answer is the workspace being
// unreachable, and any answer is its refusal.
func workspaceError(err error) error {
	st, _ := status.FromError(err)
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("workspace unreachable: %s", st.Message())
	default:
		return fmt.Errorf("workspace refused the write: %s", st.Message())
	}
}
