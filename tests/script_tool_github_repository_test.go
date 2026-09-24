package tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/server"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

const repositoryBase = "1111111111111111111111111111111111111111"
const repositoryTree = "2222222222222222222222222222222222222222"
const repositoryBlob = "3333333333333333333333333333333333333333"
const publishedTree = "4444444444444444444444444444444444444444"
const publishedCommit = "5555555555555555555555555555555555555555"
const repositoryBefore = "package calc\n\nfunc Add(a, b int) int { return a - b }\n"
const repositoryAfter = "package calc\n\nfunc Add(a, b int) int { return a + b }\n"
const repositoryToken = "fake-github-repository-credential"

// repositoryAPI substitutes GitHub's Git database and PR endpoints, not the tool.
type repositoryAPI struct {
	t              *testing.T
	mu             sync.Mutex
	writes         []string
	contents       string
	rejectPR       bool
	branchExists   bool
	echoSecret     bool
	truncated      bool
	secretBlob     bool
	secretResponse bool
}

func (f *repositoryAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(f.t, "Bearer "+repositoryToken, r.Header.Get("Authorization"))
	assert.NotContains(f.t, r.URL.String(), repositoryToken)
	body, err := io.ReadAll(r.Body)
	assert.NoError(f.t, err)
	assert.NotContains(f.t, string(body), repositoryToken)
	w.Header().Set("Content-Type", "application/json")
	if f.secretResponse {
		fmt.Fprintf(w, `{"default_branch":"\u0066ake-github-repository-credential"}`)
		return
	}
	if f.echoSecret {
		http.Error(w, repositoryToken, http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	}
	var result any
	switch r.Method + " " + r.URL.Path {
	case "GET /repos/example/widget":
		result = map[string]any{"default_branch": "main"}
	case "GET /repos/example/widget/git/ref/heads/main":
		result = map[string]any{"object": map[string]string{"sha": repositoryBase}}
	case "GET /repos/example/widget/git/commits/" + repositoryBase:
		result = map[string]any{"tree": map[string]string{"sha": repositoryTree}}
	case "GET /repos/example/widget/git/trees/" + repositoryTree:
		assert.Equal(f.t, "1", r.URL.Query().Get("recursive"))
		result = map[string]any{"truncated": f.truncated, "tree": []map[string]string{{"path": "calc.go", "mode": "100644", "type": "blob", "sha": repositoryBlob}}}
	case "GET /repos/example/widget/git/blobs/" + repositoryBlob:
		content := repositoryBefore
		if f.secretBlob {
			content = repositoryToken
		}
		result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}
	case "POST /repos/example/widget/git/trees":
		var tree struct {
			Base string                                       `json:"base_tree"`
			Tree []struct{ Path, Mode, Type, Content string } `json:"tree"`
		}
		assert.NoError(f.t, json.Unmarshal(body, &tree))
		assert.Equal(f.t, repositoryTree, tree.Base)
		if assert.Len(f.t, tree.Tree, 1) {
			assert.Equal(f.t, "calc.go", tree.Tree[0].Path)
			assert.Equal(f.t, "100644", tree.Tree[0].Mode)
			assert.Equal(f.t, "blob", tree.Tree[0].Type)
			f.contents = tree.Tree[0].Content
		}
		result = map[string]string{"sha": publishedTree}
	case "POST /repos/example/widget/git/commits":
		var commit struct {
			Tree    string
			Parents []string
			Message string
		}
		assert.NoError(f.t, json.Unmarshal(body, &commit))
		assert.Equal(f.t, publishedTree, commit.Tree)
		assert.Equal(f.t, []string{repositoryBase}, commit.Parents)
		assert.Equal(f.t, "fix addition", commit.Message)
		result = map[string]string{"sha": publishedCommit}
	case "POST /repos/example/widget/git/refs":
		if f.branchExists {
			http.Error(w, `{"message":"Reference already exists"}`, http.StatusUnprocessableEntity)
			return
		}
		f.branchExists = true
		assert.JSONEq(f.t, `{"ref":"refs/heads/qa/addition","sha":"`+publishedCommit+`"}`, string(body))
		result = map[string]string{"ref": "refs/heads/qa/addition"}
	case "POST /repos/example/widget/pulls":
		if f.rejectPR {
			http.Error(w, `{"message":"refused"}`, http.StatusForbidden)
			return
		}
		assert.JSONEq(f.t, `{"title":"fix addition","body":"","head":"qa/addition","base":"main","draft":true}`, string(body))
		result = map[string]any{"html_url": "https://github.com/example/widget/pull/17", "number": 17}
	default:
		f.t.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(result)
}

// repositoryValidator checks the actual workspace edit at the execution seam.
// Kernel isolation is exercised separately by internal/workspace/shell tests.
type repositoryValidator struct {
	t        *testing.T
	root     string
	mu       sync.Mutex
	requests []shell.Request
	timeout  bool
	mutate   bool
}

func (v *repositoryValidator) Run(_ context.Context, req shell.Request) (shell.Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.requests = append(v.requests, req)
	assert.True(v.t, req.NoNetwork)
	assert.False(v.t, req.Trusted)
	assert.Empty(v.t, req.Env)
	assert.Empty(v.t, req.Args)
	assert.Equal(v.t, "validate-addition", req.Command)
	content, err := os.ReadFile(filepath.Join(v.root, "calc.go"))
	if err != nil {
		return shell.Result{}, err
	}
	if v.mutate {
		err = os.WriteFile(filepath.Join(v.root, "calc.go"), []byte(repositoryBefore), 0600)
		assert.NoError(v.t, err)
	}
	if string(content) != repositoryAfter {
		return shell.Result{ExitCode: 1}, nil
	}
	return shell.Result{TimedOut: v.timeout}, nil
}

func TestScriptTool_GitHub_RepositoryWorkflow(t *testing.T) {
	root := t.TempDir()
	pins := tool.ManifestPins{}
	for _, name := range []string{"github", "file_read", "file_write", "file_list"} {
		pins[name] = installTool(t, root, name)
	}
	reg, err := tool.NewRegistry(tool.RegistryOptions{Dir: root, Pins: pins, Required: []string{"github"}})
	require.NoError(t, err)
	work := t.TempDir()
	fs, err := files.Open(work)
	require.NoError(t, err)
	validator := &repositoryValidator{t: t, root: work}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	rpc := grpc.NewServer()
	workspace.RegisterWorkspaceServer(rpc, &server.Server{FS: fs, Executor: validator})
	go func() { _ = rpc.Serve(listener) }()
	t.Cleanup(rpc.Stop)
	api := &repositoryAPI{t: t}
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)
	t.Setenv("GH_TOKEN", repositoryToken)
	t.Setenv("GITHUB_API_BASE", httpServer.URL)
	t.Setenv("GITHUB_REPOSITORY", "example/widget")
	t.Setenv("GITHUB_BRANCH", "qa/addition")
	t.Setenv("GITHUB_VALIDATE_COMMAND", "validate-addition")
	t.Setenv("GAGENT_WORKSPACE_ADDR", listener.Addr().String())

	call := func(name string, input any) (json.RawMessage, error) {
		data, err := json.Marshal(input)
		require.NoError(t, err)
		out, err := reg.Call(context.Background(), name, data)
		assert.NotContains(t, string(out), repositoryToken)
		if err != nil {
			assert.NotContains(t, err.Error(), repositoryToken)
		}
		return out, err
	}
	githubInput := func(operation string) map[string]any {
		return map[string]any{"operation": operation, "owner": "example", "repo": "widget"}
	}
	out, err := call("github", githubInput("inspect"))
	require.NoError(t, err)
	assert.Contains(t, string(out), repositoryBase)
	assert.Contains(t, string(out), "calc.go")
	input := githubInput("import_file")
	input["sha"] = repositoryBase
	input["path"] = "calc.go"
	_, err = call("github", input)
	require.NoError(t, err)
	out, err = call("file_list", map[string]string{"path": "."})
	require.NoError(t, err)
	assert.Contains(t, string(out), "calc.go")
	out, err = call("file_read", map[string]string{"path": "calc.go"})
	require.NoError(t, err)
	var read struct{ Content string }
	require.NoError(t, json.Unmarshal(out, &read))
	assert.Equal(t, repositoryBefore, read.Content)
	api.mu.Lock()
	api.secretBlob = true
	api.mu.Unlock()
	_, err = call("github", input)
	require.ErrorContains(t, err, "credential in file refused")
	contentAfterRefusal, err := os.ReadFile(filepath.Join(work, "calc.go"))
	require.NoError(t, err)
	assert.Equal(t, repositoryBefore, string(contentAfterRefusal))
	api.mu.Lock()
	api.secretBlob = false
	api.secretResponse = true
	api.mu.Unlock()
	_, err = call("github", githubInput("inspect"))
	require.ErrorContains(t, err, "credential or invalid data in response refused")
	api.mu.Lock()
	api.secretResponse = false
	api.mu.Unlock()
	publishInput := githubInput("publish")
	publishInput["sha"] = repositoryBase
	publishInput["files"] = []string{"calc.go"}
	publishInput["title"] = "fix addition"
	// The same publish that fails validation below succeeds after a substantive edit.
	_, err = call("github", publishInput)
	require.ErrorContains(t, err, "validation failed")
	api.mu.Lock()
	assert.Empty(t, api.writes)
	api.mu.Unlock()
	_, err = call("file_write", map[string]string{"path": "calc.go", "content": repositoryAfter})
	require.NoError(t, err)
	out, err = call("github", publishInput)
	require.NoError(t, err)
	assert.Contains(t, string(out), "https://github.com/example/widget/pull/17")
	assert.Contains(t, string(out), publishedCommit)
	api.mu.Lock()
	assert.Equal(t, repositoryAfter, api.contents)
	assert.Equal(t, []string{"POST /repos/example/widget/git/trees", "POST /repos/example/widget/git/commits", "POST /repos/example/widget/git/refs", "POST /repos/example/widget/pulls"}, api.writes)
	api.mu.Unlock()

	_, err = call("github", publishInput)
	require.ErrorContains(t, err, "status 422")
	api.mu.Lock()
	assert.Equal(t, "POST /repos/example/widget/git/refs", api.writes[len(api.writes)-1])
	api.mu.Unlock()

	cases := []struct {
		name   string
		change func(*testing.T, map[string]any)
		want   string
	}{
		{"wrong repository", func(_ *testing.T, in map[string]any) { in["repo"] = "gagent" }, "not authorized"},
		{"repository URL injection", func(_ *testing.T, in map[string]any) { in["repo"] = "widget/../../gagent" }, "not authorized"},
		{"stale revision", func(_ *testing.T, in map[string]any) { in["sha"] = publishedCommit }, "current inspected"},
		{"default branch", func(t *testing.T, _ map[string]any) { t.Setenv("GITHUB_BRANCH", "main") }, "non-default"},
		{"invalid branch", func(t *testing.T, _ map[string]any) { t.Setenv("GITHUB_BRANCH", "../main") }, "non-default"},
		{"missing scope", func(t *testing.T, _ map[string]any) { t.Setenv("GITHUB_REPOSITORY", "") }, "not authorized"},
		{"missing validator", func(t *testing.T, _ map[string]any) { t.Setenv("GITHUB_VALIDATE_COMMAND", "") }, "requires validation"},
		{"traversal", func(_ *testing.T, in map[string]any) { in["files"] = []string{"../calc.go"} }, "relative file"},
		{"git metadata", func(_ *testing.T, in map[string]any) { in["files"] = []string{".git/config"} }, "relative file"},
		{"duplicate files", func(_ *testing.T, in map[string]any) { in["files"] = []string{"calc.go", "calc.go"} }, "distinct"},
		{"validation timeout", func(t *testing.T, _ map[string]any) {
			validator.mu.Lock()
			validator.timeout = true
			validator.mu.Unlock()
		}, "validation failed"},
		{"validation changes files", func(t *testing.T, _ map[string]any) {
			validator.mu.Lock()
			validator.mutate = true
			validator.mu.Unlock()
		}, "changed during validation"},
		{"truncated tree", func(t *testing.T, _ map[string]any) { api.mu.Lock(); api.truncated = true; api.mu.Unlock() }, "truncated"},
		{"reflected API credential", func(t *testing.T, _ map[string]any) { api.mu.Lock(); api.echoSecret = true; api.mu.Unlock() }, "status 403"},
		{"workspace credential", func(t *testing.T, _ map[string]any) {
			_, e := call("file_write", map[string]string{"path": "calc.go", "content": repositoryToken})
			require.NoError(t, e)
		}, "credential in workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each refusal starts from the accepted publication inputs and workspace.
			in := githubInput("publish")
			in["sha"] = repositoryBase
			in["files"] = []string{"calc.go"}
			in["title"] = "fix addition"
			tc.change(t, in)
			api.mu.Lock()
			api.writes = nil
			api.mu.Unlock()
			_, err := call("github", in)
			require.ErrorContains(t, err, tc.want)
			api.mu.Lock()
			assert.Empty(t, api.writes)
			api.truncated = false
			api.echoSecret = false
			api.mu.Unlock()
			validator.mu.Lock()
			validator.timeout = false
			validator.mutate = false
			validator.mu.Unlock()
			t.Cleanup(func() {
				_, e := call("file_write", map[string]string{"path": "calc.go", "content": repositoryAfter})
				require.NoError(t, e)
			})
		})
	}
	api.mu.Lock()
	api.rejectPR = true
	api.branchExists = false
	api.mu.Unlock()
	_, err = call("github", publishInput)
	require.ErrorContains(t, err, "branch created; draft PR failed")
	validator.mu.Lock()
	assert.NotEmpty(t, validator.requests)
	validator.mu.Unlock()
	content, err := os.ReadFile(filepath.Join(work, "calc.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(content), repositoryToken)
	entries, err := os.ReadDir(work)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "calc.go", entries[0].Name())
}

func TestScriptTool_GitHub_RejectsRedirect(t *testing.T) {
	root := t.TempDir()
	pin := installTool(t, root, "github")
	reg, err := tool.NewRegistry(tool.RegistryOptions{Dir: root, Pins: tool.ManifestPins{"github": pin}, Required: []string{"github"}})
	require.NoError(t, err)
	var mu sync.Mutex
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reached = true
		mu.Unlock()
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	t.Setenv("GH_TOKEN", repositoryToken)
	t.Setenv("GITHUB_REPOSITORY", "example/widget")
	t.Setenv("GITHUB_API_BASE", redirect.URL)
	_, err = reg.Call(context.Background(), "github", json.RawMessage(`{"operation":"inspect","owner":"example","repo":"widget"}`))
	require.ErrorContains(t, err, "GitHub request failed")
	mu.Lock()
	assert.False(t, reached)
	mu.Unlock()
	// Control: the configured destination is contacted when it does not redirect.
	t.Setenv("GITHUB_API_BASE", target.URL)
	_, err = reg.Call(context.Background(), "github", json.RawMessage(`{"operation":"inspect","owner":"example","repo":"widget"}`))
	require.ErrorContains(t, err, "invalid default branch")
	mu.Lock()
	assert.True(t, reached)
	mu.Unlock()
	assert.False(t, strings.Contains(err.Error(), repositoryToken))
}
