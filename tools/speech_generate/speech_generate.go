// speech_generate turns text into speech over the script-tool runner's
// stdio protocol (ADR 0009 §Decision) and writes the audio into the
// agent's workspace through the workspace contract, as file_write does. It
// calls a speech endpoint of the OpenRouter shape, POST
// {SPEECH_API_BASE}/audio/speech, with the model SPEECH_MODEL names. The
// key comes from the file SPEECH_API_KEY_FILE names, read on every call, or
// from SPEECH_API_KEY. Neither the key nor the audio bytes reach stdout.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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
	defaultModel   = "hexgrad/kokoro-82m"
	defaultVoice   = "af_heart"
)

// requestTimeout bounds one call to the speech endpoint end to end. The
// answer is the finished audio, which takes seconds to a minute or two
// for long text; nothing else bounds the call (security.md, "An outbound
// call a tool makes is bounded by the tool that makes it").
const requestTimeout = 5 * time.Minute

// writeTimeout bounds the workspace Write, a pod-local filesystem call.
const writeTimeout = 10 * time.Second

// maxResponseBytes caps how much of the endpoint's answer is read.
const maxResponseBytes = 64 << 20

// maxErrorBody caps how much of an error answer an error message carries.
const maxErrorBody = 512

var httpClient = &http.Client{Timeout: requestTimeout}

type callInput struct {
	Text   string `json:"text"`
	Path   string `json:"path"`
	Voice  string `json:"voice"`
	Format string `json:"format"`
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
	voice   string
	key     string
	keyFile string
	addr    string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "speech_generate:", err)
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
	if in.Text == "" || in.Path == "" {
		return errors.New("text and path are required")
	}
	if in.Format != "" && in.Format != "mp3" && in.Format != "pcm" {
		return fmt.Errorf("format must be mp3 or pcm, not %q", in.Format)
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
		base:    os.Getenv("SPEECH_API_BASE"),
		model:   os.Getenv("SPEECH_MODEL"),
		voice:   os.Getenv("SPEECH_VOICE"),
		key:     os.Getenv("SPEECH_API_KEY"),
		keyFile: os.Getenv("SPEECH_API_KEY_FILE"),
		addr:    os.Getenv("SHERLOCK_WORKSPACE_ADDR"),
	}
	if cfg.base == "" {
		cfg.base = defaultAPIBase
	}
	if cfg.model == "" {
		cfg.model = defaultModel
		if cfg.voice == "" {
			cfg.voice = defaultVoice
		}
	}
	switch {
	case cfg.addr == "":
		return settings{}, errors.New("SHERLOCK_WORKSPACE_ADDR is required")
	case cfg.key != "" && cfg.keyFile != "":
		return settings{}, errors.New("SPEECH_API_KEY and SPEECH_API_KEY_FILE are both set; set one")
	case cfg.key == "" && cfg.keyFile == "":
		return settings{}, errors.New("SPEECH_API_KEY_FILE or SPEECH_API_KEY is required")
	}
	return cfg, nil
}

// generate asks the endpoint for the speech and writes it to in.Path. The
// workspace writes through a temporary file and a rename, so a call that
// fails leaves whatever was at the path before. The endpoint answers with
// raw audio and reports no cost, so the output's cost is always null.
func generate(ctx context.Context, cfg settings, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	request := map[string]string{"model": cfg.model, "input": in.Text}
	if voice := firstNonEmpty(in.Voice, cfg.voice); voice != "" {
		request["voice"] = voice
	}
	if in.Format != "" {
		request["response_format"] = in.Format
	}
	body, err := json.Marshal(request)
	if err != nil {
		return callOutput{}, err
	}
	audio, mediaType, err := post(ctx, cfg, body)
	if err != nil {
		return callOutput{}, err
	}
	if len(audio) == 0 {
		return callOutput{}, fmt.Errorf("model refused: %s returned no audio", cfg.model)
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if _, err := ws.Write(writeCtx, &workspace.WriteRequest{Path: in.Path, Content: audio}); err != nil {
		return callOutput{}, workspaceError(err)
	}
	return callOutput{Path: in.Path, MediaType: mediaType}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// post sends one speech request and returns the audio and its media type.
// A key file the endpoint answers with 401 is read again and the request
// sent once more, so a key rewritten since the first read recovers the
// call, as the chat model's does (ADR 0029). A key from SPEECH_API_KEY is
// not retried.
func post(ctx context.Context, cfg settings, body []byte) ([]byte, string, error) {
	resp, err := send(ctx, cfg, body)
	if err == nil && resp.StatusCode == http.StatusUnauthorized && cfg.keyFile != "" {
		_ = resp.Body.Close()
		resp, err = send(ctx, cfg, body)
	}
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("endpoint error: read answer: %v", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, "", fmt.Errorf("endpoint error: answer is larger than %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", statusError(cfg.model, resp.StatusCode, raw)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "audio/") {
		// A 200 that is not audio is a generation that failed after the
		// endpoint had sent its status.
		return nil, "", fmt.Errorf("endpoint error: answer is %q, not audio: %s", resp.Header.Get("Content-Type"), truncate(string(raw)))
	}
	return raw, mediaType, nil
}

func send(ctx context.Context, cfg settings, body []byte) (*http.Response, error) {
	key, err := readKey(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(cfg.base, "/")+"/audio/speech", bytes.NewReader(body))
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
// or SPEECH_API_KEY.
func readKey(cfg settings) (string, error) {
	if cfg.keyFile == "" {
		return cfg.key, nil
	}
	raw, err := os.ReadFile(cfg.keyFile)
	if err != nil {
		return "", fmt.Errorf("read SPEECH_API_KEY_FILE: %v", err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("read SPEECH_API_KEY_FILE: %s is empty", cfg.keyFile)
	}
	return key, nil
}

func truncate(s string) string {
	if len(s) > maxErrorBody {
		return s[:maxErrorBody]
	}
	return s
}

// statusError names a non-2xx answer. 403 and 404 are the endpoint
// declining the request it understood — a model it does not serve or will
// not run, or text its policy refuses — so they are the model's refusal;
// every other status is the endpoint's own error.
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
	detail = truncate(detail)
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
