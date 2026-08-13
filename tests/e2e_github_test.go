package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndToEnd_GitHubBinarySpawnsProtocolServer spawns the github
// binary directly with a fake GH_TOKEN and an httptest server
// pretending to be api.github.com. The protocol round-trip is asserted
// for every operation: tools/list returns the descriptor, and
// tools/call reaches the fake server with the right method, path,
// and bearer token. No real network. Issue #291.
func TestEndToEnd_GitHubBinarySpawnsProtocolServer(t *testing.T) {
	binary := buildGagentBinary(t)
	githubBin := filepath.Join(filepath.Dir(binary), "github")

	// canned tracks every GitHub API call the binary makes; the test
	// asserts the right method/path/headers/body land at the fake
	// server.
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
			fmt.Fprint(w, `{"ref":"refs/heads/main","object":{"url":"https://api.github.com/objects/abc"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/issues":
			fmt.Fprint(w, `{"html_url":"https://github.com/example/widget/issues/42","number":42}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/pulls":
			fmt.Fprint(w, `{"html_url":"https://github.com/example/widget/pull/7","number":7}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/widget/issues/9/comments":
			fmt.Fprint(w, `{"html_url":"https://github.com/example/widget/issues/9#issuecomment-123","id":123}`)
		default:
			http.Error(w, `{"message":"unexpected request"}`, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	// Spawn the binary with the fake token, the fake server as its
	// API base, and stdio piped over a pipe pair.
	cmd := exec.Command(githubBin)
	cmd.Env = append(os.Environ(),
		"GH_TOKEN=fake-test-token",
		"GITHUB_API_BASE="+server.URL,
	)
	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stdoutW
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdoutW.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	require.NoError(t, stdinR.Close())

	enc := json.NewEncoder(stdinW)
	dec := json.NewDecoder(bufio.NewReader(stdoutR))

	// tools/list returns the descriptor with the embedded schemas.
	listReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	}
	require.NoError(t, enc.Encode(listReq))
	var listResp struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
				Output      json.RawMessage `json:"outputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, dec.Decode(&listResp))
	require.Len(t, listResp.Result.Tools, 1)
	assert.Equal(t, "github", listResp.Result.Tools[0].Name)
	assert.NotEmpty(t, listResp.Result.Tools[0].InputSchema)
	assert.NotEmpty(t, listResp.Result.Tools[0].Output)

	// Drive each operation through tools/call and assert the
	// round-trip produces the right output and the fake server got
	// the right request.
	cases := []struct {
		name     string
		input    map[string]any
		path     string
		wantKey  string
		wantPath string
	}{
		{
			name: "push",
			input: map[string]any{
				"operation": "push",
				"owner":     "example",
				"repo":      "widget",
				"branch":    "main",
				"sha":       "deadbeef",
			},
			path:     "/repos/example/widget/git/refs/heads/main",
			wantKey:  "status",
			wantPath: "/repos/example/widget/git/refs/heads/main",
		},
		{
			name: "open_issue",
			input: map[string]any{
				"operation": "open_issue",
				"owner":     "example",
				"repo":      "widget",
				"title":     "found a bug",
				"body":      "details",
			},
			path:     "/repos/example/widget/issues",
			wantKey:  "number",
			wantPath: "/repos/example/widget/issues",
		},
		{
			name: "open_pull_request",
			input: map[string]any{
				"operation": "open_pull_request",
				"owner":     "example",
				"repo":      "widget",
				"head":      "feature",
				"base":      "main",
				"title":     "add a feature",
				"body":      "details",
			},
			path:     "/repos/example/widget/pulls",
			wantKey:  "number",
			wantPath: "/repos/example/widget/pulls",
		},
		{
			name: "comment",
			input: map[string]any{
				"operation": "comment",
				"owner":     "example",
				"repo":      "widget",
				"number":    9,
				"message":   "looks good",
			},
			path:     "/repos/example/widget/issues/9/comments",
			wantKey:  "id",
			wantPath: "/repos/example/widget/issues/9/comments",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := len(calls) + 100
			req := map[string]any{
				"jsonrpc": "2.0", "id": id, "method": "tools/call",
				"params": map[string]any{
					"name":      "github",
					"arguments": tc.input,
				},
			}
			require.NoError(t, enc.Encode(req))
			var callResp struct {
				Result struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			require.NoError(t, dec.Decode(&callResp))
			require.Len(t, callResp.Result.Content, 1)
			require.Equal(t, "text", callResp.Result.Content[0].Type)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(callResp.Result.Content[0].Text), &body))
			assert.Contains(t, body, tc.wantKey)
			// Per-operation status assertions: the binary emits
			// the verb in the status field; the round-trip test
			// only needs to confirm each operation carries
			// status + the operation-specific key.
			switch tc.name {
			case "push":
				assert.Equal(t, "pushed", body["status"])
			case "open_issue", "open_pull_request":
				assert.Equal(t, "opened", body["status"])
			case "comment":
				assert.Equal(t, "commented", body["status"])
			}

			// The fake server received the right request.
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
				"the binary forwards the GH_TOKEN as a bearer header")
		})
	}
}

// TestEndToEnd_GitHubBinaryRejectsStartupWithoutToken asserts the
// binary fails closed when GH_TOKEN is not in its environment.
func TestEndToEnd_GitHubBinaryRejectsStartupWithoutToken(t *testing.T) {
	binary := buildGagentBinary(t)
	githubBin := filepath.Join(filepath.Dir(binary), "github")

	cmd := exec.Command(githubBin)
	// Explicitly strip GH_TOKEN even if the test process inherits it.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "github binary must reject startup without GH_TOKEN")
	assert.Contains(t, string(out), "GH_TOKEN",
		"the rejection names the missing variable")
}

// githubCall records one GitHub API request received by the fake
// server. The test asserts the right method, path, and bearer header.
type githubCall struct {
	Method string
	Path   string
	Auth   string
	Body   string
}
