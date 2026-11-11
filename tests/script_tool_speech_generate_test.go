package tests

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/tool"
)

// mp3Bytes stands in for generated speech: an ID3 tag and a few bytes,
// enough for the content to be compared.
var mp3Bytes = []byte("ID3\x04\x00not really audio")

// speechEndpoint is a local stand-in for an OpenRouter-shaped speech
// endpoint. It records every request and answers with audio, or with
// status and errorBody when status is set. No request leaves the host.
type speechEndpoint struct {
	URL string

	mu          sync.Mutex
	requests    []speechRequest
	status      int
	errorBody   string
	audio       []byte
	contentType string
	// onRequest, when set, runs before the answer, under the lock.
	onRequest func(auth string) int
}

type speechRequest struct {
	Path, Auth                  string
	Model, Input, Voice, Format string
}

func newSpeechEndpoint(t *testing.T) *speechEndpoint {
	t.Helper()
	e := &speechEndpoint{audio: mp3Bytes, contentType: "audio/mpeg"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model          string `json:"model"`
			Input          string `json:"input"`
			Voice          string `json:"voice"`
			ResponseFormat string `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		e.mu.Lock()
		defer e.mu.Unlock()
		auth := r.Header.Get("Authorization")
		e.requests = append(e.requests, speechRequest{Path: r.URL.Path, Auth: auth,
			Model: body.Model, Input: body.Input, Voice: body.Voice, Format: body.ResponseFormat})
		status := e.status
		if e.onRequest != nil {
			status = e.onRequest(auth)
		}
		if status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(e.errorBody))
			return
		}
		w.Header().Set("Content-Type", e.contentType)
		_, _ = w.Write(e.audio)
	}))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/api/v1"
	return e
}

func (e *speechEndpoint) seen() []speechRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]speechRequest(nil), e.requests...)
}

func installSpeechTool(t *testing.T) *tool.Registry {
	t.Helper()
	root := t.TempDir()
	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir: root, Pins: tool.ManifestPins{"speech_generate": installTool(t, root, "speech_generate")}, Required: []string{},
	})
	require.NoError(t, err)
	return reg
}

// speechEnv points the tool at e and the workspace at addr, with no key
// and no voice configured; each test names its own source.
func speechEnv(t *testing.T, e *speechEndpoint, addr string) {
	t.Helper()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	t.Setenv("SPEECH_API_BASE", e.URL)
	t.Setenv("SPEECH_MODEL", "test/speech-model")
	t.Setenv("SPEECH_VOICE", "")
	t.Setenv("SPEECH_API_KEY", "")
	t.Setenv("SPEECH_API_KEY_FILE", "")
}

const sayHello = `{"text":"hello there","path":"audio/hello.mp3"}`

// TestScriptTool_SpeechGenerate_WritesTheAudioIntoTheWorkspace runs the
// tool through the registry against a fake endpoint and a real workspace
// server: the audio's bytes land at the path, the output names the path
// and media type with a null cost and nothing else, and the request
// carries the key from the configured source — the file, read at the
// call, or the variable.
func TestScriptTool_SpeechGenerate_WritesTheAudioIntoTheWorkspace(t *testing.T) {
	reg := installSpeechTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()

	for _, source := range []string{"SPEECH_API_KEY_FILE", "SPEECH_API_KEY"} {
		t.Run(source, func(t *testing.T) {
			endpoint := newSpeechEndpoint(t)
			speechEnv(t, endpoint, addr)
			if source == "SPEECH_API_KEY_FILE" {
				keyPath := filepath.Join(t.TempDir(), "key")
				writeKeyFile(t, keyPath, "from-the-source")
				t.Setenv(source, keyPath)
			} else {
				t.Setenv(source, "from-the-source")
			}

			out, err := reg.Call(t.Context(), "speech_generate", json.RawMessage(sayHello))
			require.NoError(t, err)
			assert.JSONEq(t, `{"path":"audio/hello.mp3","media_type":"audio/mpeg","cost_usd":null}`, string(out))
			written, err := fs.Read("audio/hello.mp3")
			require.NoError(t, err)
			assert.Equal(t, mp3Bytes, written)

			requests := endpoint.seen()
			require.Len(t, requests, 1)
			assert.Equal(t, speechRequest{Path: "/api/v1/audio/speech", Auth: "Bearer from-the-source",
				Model: "test/speech-model", Input: "hello there"}, requests[0])
		})
	}
}

// TestScriptTool_SpeechGenerate_VoiceAndFormatReachTheRequest checks that
// the tool's voice and format inputs are the request's, that the voice
// input wins over the operator's SPEECH_VOICE, and that a format the
// manifest does not list is refused before any request. The control is
// the call with neither input, which sends neither field.
func TestScriptTool_SpeechGenerate_VoiceAndFormatReachTheRequest(t *testing.T) {
	reg := installSpeechTool(t)
	addr, _, _, stop := startFakeWorkspace(t)
	defer stop()
	endpoint := newSpeechEndpoint(t)
	speechEnv(t, endpoint, addr)
	t.Setenv("SPEECH_API_KEY", "k")
	t.Setenv("SPEECH_VOICE", "operator-voice")
	endpoint.contentType = "audio/pcm"

	out, err := reg.Call(t.Context(), "speech_generate",
		json.RawMessage(`{"text":"hi","path":"a.pcm","voice":"nova","format":"pcm"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"path":"a.pcm","media_type":"audio/pcm","cost_usd":null}`, string(out))
	_, err = reg.Call(t.Context(), "speech_generate", json.RawMessage(`{"text":"hi","path":"b.pcm"}`))
	require.NoError(t, err)
	requests := endpoint.seen()
	require.Len(t, requests, 2)
	assert.Equal(t, "nova", requests[0].Voice)
	assert.Equal(t, "pcm", requests[0].Format)
	assert.Equal(t, "operator-voice", requests[1].Voice, "control: the operator's voice applies without the input")
	assert.Empty(t, requests[1].Format, "control: no format is sent unless asked for")

	_, err = reg.Call(t.Context(), "speech_generate", json.RawMessage(`{"text":"hi","path":"c.wav","format":"wav"}`))
	require.Error(t, err)
	assert.Len(t, endpoint.seen(), 2, "a format outside the manifest never reaches the endpoint")
}

// TestScriptTool_SpeechGenerate_RejectedKeyFileIsReadAgainOnce pins the
// recovery ADR 0029 gives the chat model, applied to this tool's key
// file: a 401 reads the file again and sends the request once more. The
// control is the variable, which cannot change and is sent once.
func TestScriptTool_SpeechGenerate_RejectedKeyFileIsReadAgainOnce(t *testing.T) {
	reg := installSpeechTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()

	endpoint := newSpeechEndpoint(t)
	speechEnv(t, endpoint, addr)
	keyPath := filepath.Join(t.TempDir(), "key")
	writeKeyFile(t, keyPath, "expired")
	t.Setenv("SPEECH_API_KEY_FILE", keyPath)
	endpoint.errorBody = `{"error":{"code":401,"message":"invalid token"}}`
	endpoint.onRequest = func(auth string) int {
		if auth == "Bearer rotated" {
			return 0
		}
		writeKeyFile(t, keyPath, "rotated")
		return http.StatusUnauthorized
	}
	_, err := reg.Call(t.Context(), "speech_generate", json.RawMessage(sayHello))
	require.NoError(t, err, "the re-read key recovers the call")
	requests := endpoint.seen()
	require.Len(t, requests, 2)
	assert.Equal(t, "Bearer expired", requests[0].Auth)
	assert.Equal(t, "Bearer rotated", requests[1].Auth)
	_, err = fs.Read("audio/hello.mp3")
	require.NoError(t, err)

	fixed := newSpeechEndpoint(t)
	speechEnv(t, fixed, addr)
	t.Setenv("SPEECH_API_KEY", "expired")
	fixed.status = http.StatusUnauthorized
	fixed.errorBody = `{"error":{"code":401,"message":"invalid token"}}`
	_, err = reg.Call(t.Context(), "speech_generate", json.RawMessage(sayHello))
	require.ErrorIs(t, err, tool.ErrExecution)
	assert.Len(t, fixed.seen(), 1, "a key from the variable is sent once")
}

// TestScriptTool_SpeechGenerate_FailuresNameTheirCauseAndLeaveThePath
// fails the call on an endpoint error, a refused model, an answer that is
// not audio and an unreachable workspace, each named, and checks that the
// file already at the path is what it was. The control is the same path
// after a call that succeeds, which does replace the file.
func TestScriptTool_SpeechGenerate_FailuresNameTheirCauseAndLeaveThePath(t *testing.T) {
	reg := installSpeechTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("audio/hello.mp3", []byte("before")))

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	unreachable := closed.Addr().String()
	require.NoError(t, closed.Close())

	for _, tc := range []struct {
		name, want, workspace string
		status                int
		errorBody             string
		audio                 []byte
		contentType           string
	}{
		{name: "endpoint error", want: "endpoint error: HTTP 502: provider down", workspace: addr,
			status: http.StatusBadGateway, errorBody: `{"error":{"code":502,"message":"provider down"}}`},
		{name: "model not served", want: "model refused: test/speech-model: HTTP 404: no such model", workspace: addr,
			status: http.StatusNotFound, errorBody: `{"error":{"code":404,"message":"no such model"}}`},
		{name: "model returned no audio", want: "model refused: test/speech-model returned no audio", workspace: addr,
			audio: []byte{}, contentType: "audio/mpeg"},
		{name: "answer is not audio", want: `endpoint error: answer is "application/json", not audio: {"error"`, workspace: addr,
			audio: []byte(`{"error":{"message":"provider failed"}}`), contentType: "application/json"},
		{name: "workspace unreachable", want: "workspace unreachable", workspace: unreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := newSpeechEndpoint(t)
			speechEnv(t, endpoint, tc.workspace)
			t.Setenv("SPEECH_API_KEY", "k")
			endpoint.status, endpoint.errorBody = tc.status, tc.errorBody
			if tc.contentType != "" {
				endpoint.audio, endpoint.contentType = tc.audio, tc.contentType
			}

			_, err := reg.Call(t.Context(), "speech_generate", json.RawMessage(sayHello))
			require.ErrorIs(t, err, tool.ErrExecution)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "Bearer")
			kept, err := fs.Read("audio/hello.mp3")
			require.NoError(t, err)
			assert.Equal(t, "before", string(kept), "a failed call leaves the path as it was")
		})
	}

	endpoint := newSpeechEndpoint(t)
	speechEnv(t, endpoint, addr)
	t.Setenv("SPEECH_API_KEY", "k")
	_, err = reg.Call(t.Context(), "speech_generate", json.RawMessage(sayHello))
	require.NoError(t, err)
	replaced, err := fs.Read("audio/hello.mp3")
	require.NoError(t, err)
	assert.Equal(t, mp3Bytes, replaced, "control: a call that succeeds replaces the file")
}

// TestScriptTool_SpeechGenerate_Live makes one paid synthesis against the
// configured endpoint (OpenRouter unless SPEECH_API_BASE says otherwise),
// with the key in SPEECH_API_KEY, and writes the audio into a temporary
// workspace. It is opt-in, because it spends money: set
// SHERLOCK_SPEECH_GENERATE_LIVE=1. It prints the model, the media type and
// the audio's size.
func TestScriptTool_SpeechGenerate_Live(t *testing.T) {
	if os.Getenv("SHERLOCK_SPEECH_GENERATE_LIVE") != "1" {
		t.Skip("a live synthesis spends money; set SHERLOCK_SPEECH_GENERATE_LIVE=1 and SPEECH_API_KEY to run it")
	}
	require.NotEmpty(t, os.Getenv("SPEECH_API_KEY"), "the live call needs SPEECH_API_KEY")
	reg := installSpeechTool(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)

	out, err := reg.Call(t.Context(), "speech_generate", json.RawMessage(`{"text":"Hello from the speech tool.","path":"live/hello.mp3","format":"mp3"}`))
	require.NoError(t, err)
	var result struct {
		Path      string   `json:"path"`
		MediaType string   `json:"media_type"`
		CostUSD   *float64 `json:"cost_usd"`
	}
	require.NoError(t, json.Unmarshal(out, &result))
	audio, err := fs.Read(result.Path)
	require.NoError(t, err)
	require.NotEmpty(t, audio)
	model := os.Getenv("SPEECH_MODEL")
	if model == "" {
		model = "the tool's default"
	}
	fmt.Printf("live speech_generate: model=%s media_type=%s bytes=%d cost_reported=%t\n",
		model, result.MediaType, len(audio), result.CostUSD != nil)
}
