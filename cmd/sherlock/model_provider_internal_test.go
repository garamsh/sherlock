package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/config"
)

// keyEndpoint answers both protocols' chat routes with a reply each
// accepts, recording the key every request carried. No request leaves
// the host.
type keyEndpoint struct {
	URL string

	mu   sync.Mutex
	keys []string
}

func newKeyEndpoint(t *testing.T) *keyEndpoint {
	t.Helper()
	e := &keyEndpoint{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.keys = append(e.keys, r.Header.Get("Authorization")+r.Header.Get("X-Api-Key"))
		e.mu.Unlock()
		if r.URL.Path == "/v1/messages" {
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/v1"
	return e
}

func (e *keyEndpoint) seen() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.keys...)
}

// rewriteKey replaces the key file the way a platform rotating it does:
// a new file renamed over the old one.
func rewriteKey(t *testing.T, path, key string) {
	t.Helper()
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(key+"\n"), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

// TestNewModelFunc_KeyFileIsReadAgainWithoutARestart runs the shipped
// wiring of model.api-key-file for both protocols (ADR 0029): one client,
// built once, sends the key the file holds at each request, and a file
// that has gone fails the call with an error naming it while the next
// request, once the file is back, succeeds.
func TestNewModelFunc_KeyFileIsReadAgainWithoutARestart(t *testing.T) {
	for _, tc := range []struct {
		provider, first, second string
	}{
		{"openai-compatible", "Bearer first", "Bearer second"},
		{"anthropic-compatible", "first", "second"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			endpoint := newKeyEndpoint(t)
			path := filepath.Join(t.TempDir(), "key")
			rewriteKey(t, path, "first")
			newModel, err := newModelFunc(config.ModelConfig{
				Provider: tc.provider, BaseURL: endpoint.URL, Model: "auto", APIKeyFile: path,
			})
			require.NoError(t, err)
			model := newModel("agent")
			chat := func() error {
				_, err := model.Chat(t.Context(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
				return err
			}

			require.NoError(t, chat())
			rewriteKey(t, path, "second")
			require.NoError(t, chat())
			assert.Equal(t, []string{tc.first, tc.second}, endpoint.seen(), "the rotated key reaches the next request")

			require.NoError(t, os.Remove(path))
			err = chat()
			require.Error(t, err, "a key file that has gone fails the call")
			assert.Contains(t, err.Error(), "model.api-key-file "+path)
			assert.Len(t, endpoint.seen(), 2, "nothing is sent without a key")

			rewriteKey(t, path, "second")
			require.NoError(t, chat(), "the same client recovers once the file is back")
		})
	}
}

// TestNewModelFunc_KeyFileMustReadAtStartup refuses a start whose key
// file is absent or empty, as an unset key variable is refused, and
// names the file. The control is a readable file, which starts.
func TestNewModelFunc_KeyFileMustReadAtStartup(t *testing.T) {
	dir := t.TempDir()
	readable := filepath.Join(dir, "key")
	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(readable, []byte("k"), 0o600))
	require.NoError(t, os.WriteFile(empty, []byte(" \n"), 0o600))
	cfg := config.ModelConfig{Provider: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1", Model: "auto"}

	cfg.APIKeyFile = readable
	_, err := newModelFunc(cfg)
	require.NoError(t, err, "control: a readable key file starts")

	for _, path := range []string{filepath.Join(dir, "absent"), empty} {
		cfg.APIKeyFile = path
		_, err := newModelFunc(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model.api-key-file "+path)
	}
}
