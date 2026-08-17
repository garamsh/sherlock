package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// TestScriptTool_GitHub_Integration drops the github tool into a
// tmpdir, points GITHUB_API_BASE at an httptest server pretending
// to be api.github.com, and asserts the four operations drive the
// right HTTP path with the right bearer token.
func TestScriptTool_GitHub_Integration(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "github")

	var (
		mu    sync.Mutex
		calls []githubCall
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, githubCall{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Body:   string(body),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/git/refs/heads/main":
			_, _ = w.Write([]byte(`{"ref":"refs/heads/main","object":{"url":"https://api.github.com/objects/abc"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/issues":
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/widget/issues/42","number":42}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/pulls":
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/widget/pull/7","number":7}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/issues/9/comments":
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/widget/issues/9#issuecomment-123","id":123}`))
		default:
			http.Error(w, `{"message":"unexpected request"}`, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("GH_TOKEN", "fake-test-token")
	t.Setenv("GITHUB_API_BASE", server.URL)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"github": hash},
		Required: []string{"github"},
	})
	require.NoError(t, err)

	cases := []struct {
		name     string
		input    string
		wantPath string
		wantKV   map[string]any
	}{
		{
			name:     "push",
			input:    `{"operation":"push","owner":"example","repo":"widget","branch":"main","sha":"deadbeef"}`,
			wantPath: "/repos/example/widget/git/refs/heads/main",
			wantKV:   map[string]any{"status": "pushed"},
		},
		{
			name:     "open_issue",
			input:    `{"operation":"open_issue","owner":"example","repo":"widget","title":"found a bug","body":"details"}`,
			wantPath: "/repos/example/widget/issues",
			wantKV:   map[string]any{"status": "opened", "number": float64(42)},
		},
		{
			name:     "open_pull_request",
			input:    `{"operation":"open_pull_request","owner":"example","repo":"widget","head":"feature","base":"main","title":"add a feature"}`,
			wantPath: "/repos/example/widget/pulls",
			wantKV:   map[string]any{"status": "opened", "number": float64(7)},
		},
		{
			name:     "comment",
			input:    `{"operation":"comment","owner":"example","repo":"widget","number":9,"message":"looks good"}`,
			wantPath: "/repos/example/widget/issues/9/comments",
			wantKV:   map[string]any{"status": "commented", "id": float64(123)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := reg.Call(context.Background(), "github", json.RawMessage(tc.input))
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(out, &body))
			for k, v := range tc.wantKV {
				assert.EqualValues(t, v, body[k])
			}

			require.Eventually(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(calls) > 0 && calls[len(calls)-1].Path == tc.wantPath
			}, 2*time.Second, 10*time.Millisecond)
			mu.Lock()
			last := calls[len(calls)-1]
			mu.Unlock()
			assert.Equal(t, http.MethodPost, last.Method)
			assert.Equal(t, tc.wantPath, last.Path)
			assert.Equal(t, "Bearer fake-test-token", last.Auth,
				"the script forwards GH_TOKEN as a bearer header")
		})
	}
}

// TestScriptTool_GitHub_MissingToken covers the error path: the
// script exits non-zero when GH_TOKEN is unset, and the registry
// surfaces the failure as an execution error.
func TestScriptTool_GitHub_MissingToken(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "github")
	t.Setenv("GH_TOKEN", "")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"github": hash},
		Required: []string{"github"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "github",
		json.RawMessage(`{"operation":"push","owner":"example","repo":"widget","branch":"main","sha":"deadbeef"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
}

// TestScriptTool_GitHub_PushRequiresBranchAndSha covers the runtime
// check that replaced the previous top-level oneOf (issue #313): the
// strict-mode subset does not accept oneOf, so the conditional
// requirements per operation moved into the script's run(). The
// registry's schema validation no longer rejects the missing
// branch/sha; the runtime returns an execution error before the
// GitHub request is issued.
func TestScriptTool_GitHub_PushRequiresBranchAndSha(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "github")
	t.Setenv("GH_TOKEN", "fake-test-token")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"github": hash},
		Required: []string{"github"},
	})
	require.NoError(t, err)

	// Missing branch and sha: the runtime check in run() returns
	// an error before the GitHub request is made.
	_, err = reg.Call(context.Background(), "github",
		json.RawMessage(`{"operation":"push","owner":"example","repo":"widget"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
}

// TestScriptTool_GitHub_InputRejected pins the boundary: an unknown
// operation is rejected before the script runs.
func TestScriptTool_GitHub_InputRejected(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "github")
	t.Setenv("GH_TOKEN", "fake-test-token")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"github": hash},
		Required: []string{"github"},
	})
	require.NoError(t, err)

	// Missing owner / repo: those remain unconditional in the
	// schema's required list, so the registry still rejects before
	// the script runs.
	_, err = reg.Call(context.Background(), "github",
		json.RawMessage(`{"operation":"push"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// githubCall records one GitHub API request received by the fake
// server. The integration test asserts the right method, path, and
// bearer header.
type githubCall struct {
	Method string
	Path   string
	Auth   string
	Body   string
}

// TestScriptTool_GitHub_NullBodyAccepted pins the boundary fix from
// issue #319: the sanitizer promotes `body` into the required list
// with a nullable type (strict-mode rejects optional; required+nullable
// is the shape it accepts). The runtime's schema validation must
// therefore accept `body: null` for open_issue instead of rejecting
// the model's null at the boundary.
func TestScriptTool_GitHub_NullBodyAccepted(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "github")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/issues":
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/widget/issues/43","number":43}`))
		default:
			http.Error(w, `{"message":"unexpected request"}`, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("GH_TOKEN", "fake-test-token")
	t.Setenv("GITHUB_API_BASE", server.URL)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"github": hash},
		Required: []string{"github"},
	})
	require.NoError(t, err)

	// Strict-mode requires every property (nullable) for every
	// operation; null fields the open_issue call doesn't need
	// appear alongside the null body the test specifically
	// exercises.
	out, err := reg.Call(context.Background(), "github", json.RawMessage(
		`{"operation":"open_issue","owner":"example","repo":"widget","title":"a bug",`+
			`"body":null,"branch":null,"sha":null,"labels":null,`+
			`"head":null,"base":null,"number":null,"message":null}`))
	require.NoError(t, err, "null body must pass schema validation and reach the GitHub request")
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "opened", body["status"])
	assert.EqualValues(t, 43, body["number"])
}
