package tests

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/tool"
)

// pngBytes stands in for a generated image: the PNG signature and a few
// bytes, enough for the content to be compared and sniffed.
var pngBytes = []byte("\x89PNG\r\n\x1a\nnot really an image")

// imageEndpoint is a local stand-in for an OpenRouter-shaped image
// endpoint. It records every request and answers with reply, or with
// status and errorBody when status is set. No request leaves the host.
type imageEndpoint struct {
	URL string

	mu        sync.Mutex
	requests  []imageRequest
	status    int
	errorBody string
	reply     string
	// onRequest, when set, runs before the answer, under the lock.
	onRequest func(auth string) int
}

type imageRequest struct {
	Path, Auth    string
	Model, Prompt string
}

func newImageEndpoint(t *testing.T) *imageEndpoint {
	t.Helper()
	e := &imageEndpoint{reply: imageReply(pngBytes, "image/png", 0.007)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model, Prompt string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		e.mu.Lock()
		defer e.mu.Unlock()
		auth := r.Header.Get("Authorization")
		e.requests = append(e.requests, imageRequest{Path: r.URL.Path, Auth: auth, Model: body.Model, Prompt: body.Prompt})
		status := e.status
		if e.onRequest != nil {
			status = e.onRequest(auth)
		}
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(e.errorBody))
			return
		}
		_, _ = w.Write([]byte(e.reply))
	}))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/api/v1"
	return e
}

func (e *imageEndpoint) seen() []imageRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]imageRequest(nil), e.requests...)
}

func imageReply(image []byte, mediaType string, cost float64) string {
	out, err := json.Marshal(map[string]any{
		"created": 1,
		"data":    []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(image), "media_type": mediaType}},
		"usage":   map[string]any{"cost": cost},
	})
	if err != nil {
		panic(err)
	}
	return string(out)
}

func installImageTool(t *testing.T) *tool.Registry {
	t.Helper()
	root := t.TempDir()
	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir: root, Pins: tool.ManifestPins{"image_generate": installTool(t, root, "image_generate")}, Required: []string{},
	})
	require.NoError(t, err)
	return reg
}

// imageEnv points the tool at e and the workspace at addr, with no key
// configured; each test names its own source.
func imageEnv(t *testing.T, e *imageEndpoint, addr string) {
	t.Helper()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	t.Setenv("IMAGE_API_BASE", e.URL)
	t.Setenv("IMAGE_MODEL", "test/image-model")
	t.Setenv("IMAGE_API_KEY", "")
	t.Setenv("IMAGE_API_KEY_FILE", "")
}

func writeKeyFile(t *testing.T, path, key string) {
	t.Helper()
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(key+"\n"), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

const drawCat = `{"prompt":"a cat","path":"art/cat.png"}`

// TestScriptTool_ImageGenerate_WritesTheImageIntoTheWorkspace runs the
// tool through the registry against a fake endpoint and a real workspace
// server: the image's bytes land at the path, the output names the path,
// media type and reported cost and nothing else, and the request carries
// the key from the configured source — the file, read at the call, or the
// variable.
func TestScriptTool_ImageGenerate_WritesTheImageIntoTheWorkspace(t *testing.T) {
	reg := installImageTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()

	for _, source := range []string{"IMAGE_API_KEY_FILE", "IMAGE_API_KEY"} {
		t.Run(source, func(t *testing.T) {
			endpoint := newImageEndpoint(t)
			imageEnv(t, endpoint, addr)
			if source == "IMAGE_API_KEY_FILE" {
				keyPath := filepath.Join(t.TempDir(), "key")
				writeKeyFile(t, keyPath, "from-the-source")
				t.Setenv(source, keyPath)
			} else {
				t.Setenv(source, "from-the-source")
			}

			out, err := reg.Call(t.Context(), "image_generate", json.RawMessage(drawCat))
			require.NoError(t, err)
			assert.JSONEq(t, `{"path":"art/cat.png","media_type":"image/png","cost_usd":0.007}`, string(out))
			written, err := fs.Read("art/cat.png")
			require.NoError(t, err)
			assert.Equal(t, pngBytes, written)

			requests := endpoint.seen()
			require.Len(t, requests, 1)
			assert.Equal(t, imageRequest{Path: "/api/v1/images", Auth: "Bearer from-the-source", Model: "test/image-model", Prompt: "a cat"}, requests[0])
		})
	}
}

// TestScriptTool_ImageGenerate_RejectedKeyFileIsReadAgainOnce pins the
// recovery ADR 0029 gives the chat model, applied to this tool's key
// file: a 401 reads the file again and sends the request once more. The
// control is the variable, which cannot change and is sent once.
func TestScriptTool_ImageGenerate_RejectedKeyFileIsReadAgainOnce(t *testing.T) {
	reg := installImageTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()

	endpoint := newImageEndpoint(t)
	imageEnv(t, endpoint, addr)
	keyPath := filepath.Join(t.TempDir(), "key")
	writeKeyFile(t, keyPath, "expired")
	t.Setenv("IMAGE_API_KEY_FILE", keyPath)
	endpoint.errorBody = `{"error":{"code":401,"message":"invalid token"}}`
	endpoint.onRequest = func(auth string) int {
		if auth == "Bearer rotated" {
			return 0
		}
		writeKeyFile(t, keyPath, "rotated")
		return http.StatusUnauthorized
	}
	_, err := reg.Call(t.Context(), "image_generate", json.RawMessage(drawCat))
	require.NoError(t, err, "the re-read key recovers the call")
	requests := endpoint.seen()
	require.Len(t, requests, 2)
	assert.Equal(t, "Bearer expired", requests[0].Auth)
	assert.Equal(t, "Bearer rotated", requests[1].Auth)
	_, err = fs.Read("art/cat.png")
	require.NoError(t, err)

	fixed := newImageEndpoint(t)
	imageEnv(t, fixed, addr)
	t.Setenv("IMAGE_API_KEY", "expired")
	fixed.status = http.StatusUnauthorized
	fixed.errorBody = `{"error":{"code":401,"message":"invalid token"}}`
	_, err = reg.Call(t.Context(), "image_generate", json.RawMessage(drawCat))
	require.ErrorIs(t, err, tool.ErrExecution)
	assert.Len(t, fixed.seen(), 1, "a key from the variable is sent once")
}

// TestScriptTool_ImageGenerate_FailuresNameTheirCauseAndLeaveThePath
// fails the call on an endpoint error, a refused model and an unreachable
// workspace, each named, and checks that the file already at the path is
// what it was. The control is the same path after a call that succeeds,
// which does replace the file.
func TestScriptTool_ImageGenerate_FailuresNameTheirCauseAndLeaveThePath(t *testing.T) {
	reg := installImageTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("art/cat.png", []byte("before")))

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	unreachable := closed.Addr().String()
	require.NoError(t, closed.Close())

	for _, tc := range []struct {
		name, want, workspace string
		status                int
		errorBody, reply      string
	}{
		{name: "endpoint error", want: "endpoint error: HTTP 502: provider down", workspace: addr,
			status: http.StatusBadGateway, errorBody: `{"error":{"code":502,"message":"provider down"}}`},
		{name: "model not served", want: "model refused: test/image-model: HTTP 404: no such model", workspace: addr,
			status: http.StatusNotFound, errorBody: `{"error":{"code":404,"message":"no such model"}}`},
		{name: "model returned no image", want: "model refused: test/image-model returned no image", workspace: addr,
			reply: `{"data":[],"usage":{"cost":0}}`},
		{name: "workspace unreachable", want: "workspace unreachable", workspace: unreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := newImageEndpoint(t)
			imageEnv(t, endpoint, tc.workspace)
			t.Setenv("IMAGE_API_KEY", "k")
			endpoint.status, endpoint.errorBody = tc.status, tc.errorBody
			if tc.reply != "" {
				endpoint.reply = tc.reply
			}

			_, err := reg.Call(t.Context(), "image_generate", json.RawMessage(drawCat))
			require.ErrorIs(t, err, tool.ErrExecution)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "Bearer")
			kept, err := fs.Read("art/cat.png")
			require.NoError(t, err)
			assert.Equal(t, "before", string(kept), "a failed call leaves the path as it was")
		})
	}

	endpoint := newImageEndpoint(t)
	imageEnv(t, endpoint, addr)
	t.Setenv("IMAGE_API_KEY", "k")
	_, err = reg.Call(t.Context(), "image_generate", json.RawMessage(drawCat))
	require.NoError(t, err)
	replaced, err := fs.Read("art/cat.png")
	require.NoError(t, err)
	assert.Equal(t, pngBytes, replaced, "control: a call that succeeds replaces the file")
}

// TestScriptTool_ImageGenerate_Live makes one paid generation against
// the configured endpoint (OpenRouter unless IMAGE_API_BASE says
// otherwise), with the key in IMAGE_API_KEY, and writes the image into a
// temporary workspace. It is opt-in, because it spends money: set
// SHERLOCK_IMAGE_GENERATE_LIVE=1. It prints the model, the media type,
// the image's size and the cost the endpoint reported.
func TestScriptTool_ImageGenerate_Live(t *testing.T) {
	if os.Getenv("SHERLOCK_IMAGE_GENERATE_LIVE") != "1" {
		t.Skip("a live generation spends money; set SHERLOCK_IMAGE_GENERATE_LIVE=1 and IMAGE_API_KEY to run it")
	}
	require.NotEmpty(t, os.Getenv("IMAGE_API_KEY"), "the live call needs IMAGE_API_KEY")
	reg := installImageTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)

	out, err := reg.Call(t.Context(), "image_generate", json.RawMessage(`{"prompt":"a small red circle on a white background","path":"live/circle.png"}`))
	require.NoError(t, err)
	var result struct {
		Path      string   `json:"path"`
		MediaType string   `json:"media_type"`
		CostUSD   *float64 `json:"cost_usd"`
	}
	require.NoError(t, json.Unmarshal(out, &result))
	image, err := fs.Read(result.Path)
	require.NoError(t, err)
	require.NotEmpty(t, image)
	cost := "not reported"
	if result.CostUSD != nil {
		cost = strconv.FormatFloat(*result.CostUSD, 'f', -1, 64)
	}
	model := os.Getenv("IMAGE_MODEL")
	if model == "" {
		model = "the tool's default"
	}
	fmt.Printf("live image_generate: model=%s media_type=%s bytes=%d detected=%s cost_usd=%s\n",
		model, result.MediaType, len(image), http.DetectContentType(image), cost)
}
