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

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/tool"
)

// githubTools are the four tools the GitHub capability ships as, one per
// operation, each taking only required inputs (issue #831).
var githubTools = []string{"github_push", "github_open_issue", "github_open_pull_request", "github_comment"}

// newGitHubRegistry installs the named tools into a tmpdir and returns a
// registry that pins and requires each of them.
func newGitHubRegistry(t *testing.T, names ...string) *tool.Registry {
	t.Helper()
	root := t.TempDir()
	pins := tool.ManifestPins{}
	for _, name := range names {
		pins[name] = installTool(t, root, name)
	}
	reg, err := tool.NewRegistry(tool.RegistryOptions{Dir: root, Pins: pins, Required: names})
	require.NoError(t, err)
	return reg
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

// startFakeGitHub serves the four endpoints the GitHub tools call and
// records every request it receives.
func startFakeGitHub(t *testing.T) (string, func() []githubCall) {
	t.Helper()
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
	return server.URL, func() []githubCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]githubCall(nil), calls...)
	}
}

// completeGitHubInputs is one complete input per GitHub tool, each
// addressed at an endpoint startFakeGitHub answers.
var completeGitHubInputs = map[string]string{
	"github_push":              `{"owner":"example","repo":"widget","branch":"main","sha":"deadbeef"}`,
	"github_open_issue":        `{"owner":"example","repo":"widget","title":"found a bug","body":"details","labels":[]}`,
	"github_open_pull_request": `{"owner":"example","repo":"widget","head":"feature","base":"main","title":"add a feature","body":""}`,
	"github_comment":           `{"owner":"example","repo":"widget","number":9,"message":"looks good"}`,
}

// TestScriptTool_GitHub_Integration drops the four GitHub tools into a
// tmpdir, points GITHUB_API_BASE at an httptest server pretending to be
// api.github.com, and asserts each drives the right HTTP path with the
// right bearer token.
func TestScriptTool_GitHub_Integration(t *testing.T) {
	apiBase, calls := startFakeGitHub(t)
	t.Setenv("GH_TOKEN", "fake-test-token")
	t.Setenv("GITHUB_API_BASE", apiBase)
	reg := newGitHubRegistry(t, githubTools...)

	cases := []struct {
		tool     string
		wantPath string
		want     string
	}{
		{"github_push", "/repos/example/widget/git/refs/heads/main",
			`{"status":"pushed","url":"https://api.github.com/objects/abc"}`},
		{"github_open_issue", "/repos/example/widget/issues",
			`{"status":"opened","url":"https://github.com/example/widget/issues/42","number":42}`},
		{"github_open_pull_request", "/repos/example/widget/pulls",
			`{"status":"opened","url":"https://github.com/example/widget/pull/7","number":7}`},
		{"github_comment", "/repos/example/widget/issues/9/comments",
			`{"status":"commented","url":"https://github.com/example/widget/issues/9#issuecomment-123","id":123}`},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			out, err := reg.Call(context.Background(), tc.tool, json.RawMessage(completeGitHubInputs[tc.tool]))
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(out))

			recorded := calls()
			require.NotEmpty(t, recorded)
			last := recorded[len(recorded)-1]
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
	t.Setenv("GH_TOKEN", "")
	reg := newGitHubRegistry(t, "github_push")

	_, err := reg.Call(context.Background(), "github_push", json.RawMessage(completeGitHubInputs["github_push"]))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
}

// TestScriptTool_GitHub_BoundaryRejectsIncompleteInput pins each tool's
// own required inputs at the registry boundary, before the script runs
// (issue #831). The control is the same tool's complete input, which the
// same registry accepts, so the refusal is the missing or out-of-range
// input and not the tool or its wiring.
func TestScriptTool_GitHub_BoundaryRejectsIncompleteInput(t *testing.T) {
	apiBase, _ := startFakeGitHub(t)
	t.Setenv("GH_TOKEN", "fake-test-token")
	t.Setenv("GITHUB_API_BASE", apiBase)
	reg := newGitHubRegistry(t, githubTools...)

	for _, tc := range []struct {
		name       string
		tool       string
		incomplete string
	}{
		{"push without branch and sha", "github_push", `{"owner":"example","repo":"widget"}`},
		{"open_issue with a null title", "github_open_issue", `{"owner":"example","repo":"widget","title":null,"body":"details","labels":[]}`},
		{"open_issue with a null body", "github_open_issue", `{"owner":"example","repo":"widget","title":"found a bug","body":null,"labels":[]}`},
		{"open_issue with an unrelated number", "github_open_issue", `{"owner":"example","repo":"widget","title":"found a bug","body":"details","labels":[],"number":0}`},
		{"open_pull_request without base", "github_open_pull_request", `{"owner":"example","repo":"widget","head":"feature","title":"add a feature","body":""}`},
		{"comment on number 0", "github_comment", `{"owner":"example","repo":"widget","number":0,"message":"looks good"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.Call(context.Background(), tc.tool, json.RawMessage(completeGitHubInputs[tc.tool]))
			require.NoError(t, err, "control: the complete input is accepted")

			_, err = reg.Call(context.Background(), tc.tool, json.RawMessage(tc.incomplete))
			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}

// githubRequestTimeout restates the bound the GitHub tools set on one API
// call. The tool is package main and cannot be imported, so the value
// is repeated here; changing one without the other fails the test
// below.
const githubRequestTimeout = 30 * time.Second

// startStalledGitHub serves the GitHub API base on an ephemeral
// loopback address and answers nothing: the handler blocks until the
// test tears the server down. It is the endpoint a bound exists for
// (issue #739) — one that took the connection and stopped answering —
// and not one that refuses or closes, which fails fast without a bound
// and so proves nothing about it.
//
// The handler is released on stopped and never on the request's own
// context: net/http cancels that context the moment the client gives
// up, so a handler waiting on it would answer at exactly the bound
// under test, and a peer under test for never answering must not
// answer. A peer that closes is refused in milliseconds with no bound
// at all, which is what the elapsed assertion below rejects.
func startStalledGitHub(t *testing.T) string {
	t.Helper()
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-stopped
	}))
	t.Cleanup(func() {
		close(stopped)
		server.Close()
	})
	return server.URL
}

// TestScriptTool_GitHub_StalledEndpointRefused covers issue #739 for
// each GitHub tool: an endpoint that accepts the connection and then
// answers nothing is refused within the tool's own bound instead of
// parking the caller. The answering endpoint in the same test is the
// control — it takes the same tool, input and wiring — so what the
// second half measures is the stall and not an endpoint that refused or
// closed. The four stalled calls run concurrently so the test waits the
// bound out once.
func TestScriptTool_GitHub_StalledEndpointRefused(t *testing.T) {
	answering, _ := startFakeGitHub(t)
	stalled := startStalledGitHub(t)
	t.Setenv("GH_TOKEN", "fake-test-token")
	reg := newGitHubRegistry(t, githubTools...)

	t.Setenv("GITHUB_API_BASE", answering)
	for _, name := range githubTools {
		_, err := reg.Call(context.Background(), name, json.RawMessage(completeGitHubInputs[name]))
		require.NoError(t, err, "control: an endpoint that answers accepts this exact %s call", name)
	}

	t.Setenv("GITHUB_API_BASE", stalled)
	type result struct {
		err     error
		elapsed time.Duration
	}
	results := make([]result, len(githubTools))
	var wg sync.WaitGroup
	for i, name := range githubTools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			_, err := reg.Call(context.Background(), name, json.RawMessage(completeGitHubInputs[name]))
			results[i] = result{err: err, elapsed: time.Since(start)}
		}()
	}
	wg.Wait()

	for i, name := range githubTools {
		got := results[i]
		require.Error(t, got.err, name)
		assert.ErrorIs(t, got.err, tool.ErrExecution, name)
		assert.GreaterOrEqual(t, got.elapsed, githubRequestTimeout,
			"%s waits its bound out: an endpoint that refused or closed would have failed fast", name)
		assert.Less(t, got.elapsed, githubRequestTimeout+10*time.Second,
			"%s then refuses, rather than parking the caller", name)
	}
}

// openIssueTool is the tool a model calls to open an issue.
const openIssueTool = "github_open_issue"

// recordedValues are the inputs the live calls in issue #831 were asked
// to send for one issue.
var recordedValues = map[string]any{
	"owner": "example",
	"repo":  "widget",
	"title": "found a bug",
	"body":  "details",
}

// minimaxFill builds tool-call arguments from a strict-mode parameters
// document the way a MiniMax model was recorded filling them (issue
// #831). onlyRequiredValues is MiniMax-M2.5-highspeed: null for every
// property whose type admits null, the asked-for value for the rest.
// Otherwise it is MiniMax-M3: the asked-for value where there is one,
// and a zero value — `number: 0`, an empty string — for every property
// the task gave no value for.
func minimaxFill(t *testing.T, parameters json.RawMessage, onlyRequiredValues bool) string {
	t.Helper()
	var schema struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(parameters, &schema))
	args := map[string]any{}
	for name, prop := range schema.Properties {
		types := []string{}
		switch typ := prop.Type.(type) {
		case string:
			types = append(types, typ)
		case []any:
			for _, v := range typ {
				types = append(types, v.(string))
			}
		}
		nullable := len(types) > 1 && types[1] == "null"
		value, asked := recordedValues[name]
		switch {
		case onlyRequiredValues && nullable:
			args[name] = nil
		case asked:
			args[name] = value
		case types[0] == "integer":
			args[name] = 0
		case types[0] == "array":
			args[name] = []any{}
		default:
			args[name] = ""
		}
	}
	out, err := json.Marshal(args)
	require.NoError(t, err)
	return string(out)
}

// TestScriptTool_GitHub_OpenIssueSurvivesRecordedMiniMaxFills covers
// issue #831 through the provider and tool boundary together: the
// shipped manifest goes through the OpenAI-compatible provider's
// strict-mode projection to a chat endpoint that fills the arguments
// as each MiniMax model was recorded doing, and the call it returns
// goes through the registry to a GitHub API stand-in. Both fills must
// open the issue with the title and body the task asked for.
func TestScriptTool_GitHub_OpenIssueSurvivesRecordedMiniMaxFills(t *testing.T) {
	for _, tc := range []struct {
		name               string
		onlyRequiredValues bool
	}{
		{"MiniMax-M2.5 nulls every nullable property", true},
		{"MiniMax-M3 fills unrelated properties with zero values", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var issueBody map[string]any
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/repos/example/widget/issues" {
					http.Error(w, `{"message":"unexpected request"}`, http.StatusInternalServerError)
					return
				}
				_ = json.NewDecoder(r.Body).Decode(&issueBody)
				_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/widget/issues/45","number":45}`))
			}))
			t.Cleanup(github.Close)
			t.Setenv("GH_TOKEN", "fake-test-token")
			t.Setenv("GITHUB_API_BASE", github.URL)

			var advertised bool
			chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools []struct {
						Function struct {
							Name       string          `json:"name"`
							Parameters json.RawMessage `json:"parameters"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				for _, wire := range req.Tools {
					if wire.Function.Name != openIssueTool {
						continue
					}
					advertised = true
					call := map[string]any{"id": "call_1", "type": "function", "function": map[string]any{
						"name": openIssueTool, "arguments": minimaxFill(t, wire.Function.Parameters, tc.onlyRequiredValues),
					}}
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
						"message": map[string]any{"content": "", "tool_calls": []any{call}}, "finish_reason": "tool_calls",
					}}})
					return
				}
				http.Error(w, "open-issue tool not advertised", http.StatusBadRequest)
			}))
			t.Cleanup(chat.Close)

			reg := newGitHubRegistry(t, githubTools...)
			model, err := openai.New(openai.Config{BaseURL: chat.URL, Model: "MiniMax-M2.5-highspeed", APIKey: "fake-test-key"})
			require.NoError(t, err)

			resp, err := model.Chat(context.Background(), []agent.Message{{
				Role:    agent.RoleUser,
				Content: "Open an issue on example/widget titled \"found a bug\" with body \"details\".",
			}}, reg.List())
			require.NoError(t, err)
			require.True(t, advertised, "the provider advertised the open-issue tool")
			require.Len(t, resp.ToolCalls, 1)

			out, err := reg.Call(context.Background(), resp.ToolCalls[0].Name, resp.ToolCalls[0].Input)
			require.NoError(t, err, "the recorded fill passes the boundary and opens the issue (arguments: %s)", resp.ToolCalls[0].Input)
			assert.JSONEq(t, `{"status":"opened","url":"https://github.com/example/widget/issues/45","number":45}`, string(out))
			assert.Equal(t, "found a bug", issueBody["title"])
			assert.Equal(t, "details", issueBody["body"])
		})
	}
}
