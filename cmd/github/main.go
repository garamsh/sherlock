// github is the platform's github tool binary (ADR 0018 §Decision, ADR 0009
// §Decision). It serves the platform protocol over stdin/stdout:
// tools/list advertises the descriptor (input and output JSON Schema),
// tools/call routes the operation (push, open_issue, open_pull_request,
// comment) through GitHub's REST API at api.github.com and returns the
// structured summary.
//
// The binary reads GH_TOKEN from its own process environment
// (ADR 0003, ADR 0014): shell_exec children do not see the token
// (ADR 0008 scrubs the env), and the binary returns summaries — never
// the token. The agent binary never imports this package in its
// address space; it spawns the binary via the operator's tool list
// (ADR 0009 §Decision). The token is operator-supplied per the
// per-tool env contract; the platform never materializes a secret.
package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// schemaFile holds the tool's input and output JSON Schema document.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors the on-disk shape of schema.json.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("github: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// descriptorName is the tool name the binary advertises.
const descriptorName = "github"

// descriptorDescription is the one-sentence summary the model sees.
const descriptorDescription = "Push branches and manage issues, pull requests, and comments on GitHub via the REST API."

// defaultAPIBase is the GitHub REST API root. GITHUB_API_BASE overrides
// it for the e2e test's httptest server.
const defaultAPIBase = "https://api.github.com"

// apiBaseURL returns the configured GitHub API base URL. The override
// is test-only; production runs use api.github.com.
func apiBaseURL() string {
	if v := os.Getenv("GITHUB_API_BASE"); v != "" {
		return v
	}
	return defaultAPIBase
}

// httpClient is the GitHub API client. It is package-level so the
// caller can substitute a custom transport in the e2e test if needed;
// production code uses the default transport.
var httpClient = &http.Client{}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "github:", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("GH_TOKEN") == "" {
		return fmt.Errorf("GH_TOKEN is required")
	}

	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	mu := &sync.Mutex{}
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      uint64          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return nil
		}
		switch req.Method {
		case "tools/list":
			mu.Lock()
			handleList(enc, req.ID)
			mu.Unlock()
		case "tools/call":
			mu.Lock()
			handleCall(enc, req.ID, req.Params)
			mu.Unlock()
		default:
			mu.Lock()
			writeError(enc, req.ID, -32601, "method not found", req.Method)
			mu.Unlock()
		}
	}
}

func handleList(enc *json.Encoder, id uint64) {
	raw, _ := json.Marshal(map[string]any{
		"tools": []map[string]any{{
			"name":         descriptorName,
			"description":  descriptorDescription,
			"inputSchema":  schema.Input,
			"outputSchema": schema.Output,
		}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
}

// callInput mirrors the schema's input shape. The schema's allOf
// branches require the per-operation fields, so the binary validates
// them after parsing.
type callInput struct {
	Operation string   `json:"operation"`
	Owner     string   `json:"owner"`
	Repo      string   `json:"repo"`
	Branch    string   `json:"branch"`
	SHA       string   `json:"sha"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Head      string   `json:"head"`
	Base      string   `json:"base"`
	Number    int      `json:"number"`
	Message   string   `json:"message"`
}

// callOutput is the structured result the tool returns. Fields are
// optional because each operation populates a different subset.
type callOutput struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Number int    `json:"number,omitempty"`
	ID     int64  `json:"id,omitempty"`
}

// errorResponse is the JSON body GitHub returns on a non-2xx response.
// message is enough for the agent to surface what went wrong; the
// tool never returns the token, the request URL, or other secrets.
type errorResponse struct {
	Message string `json:"message"`
}

func handleCall(enc *json.Encoder, id uint64, params json.RawMessage) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		writeError(enc, id, -32600, "invalid params", err.Error())
		return
	}
	if p.Name != descriptorName {
		writeError(enc, id, -32602, "unknown tool", p.Name)
		return
	}
	var args callInput
	if err := json.Unmarshal(p.Arguments, &args); err != nil {
		writeError(enc, id, -32602, "invalid arguments", err.Error())
		return
	}
	if args.Owner == "" || args.Repo == "" {
		writeError(enc, id, -32602, "invalid arguments", "owner and repo are required")
		return
	}

	var (
		out callOutput
		err error
	)
	switch args.Operation {
	case "push":
		if args.Branch == "" || args.SHA == "" {
			writeError(enc, id, -32602, "invalid arguments", "branch and sha are required for push")
			return
		}
		out, err = doPush(args)
	case "open_issue":
		if args.Title == "" {
			writeError(enc, id, -32602, "invalid arguments", "title is required for open_issue")
			return
		}
		out, err = doOpenIssue(args)
	case "open_pull_request":
		if args.Head == "" || args.Base == "" || args.Title == "" {
			writeError(enc, id, -32602, "invalid arguments", "head, base, and title are required for open_pull_request")
			return
		}
		out, err = doOpenPullRequest(args)
	case "comment":
		if args.Number <= 0 || args.Message == "" {
			writeError(enc, id, -32602, "invalid arguments", "number and message are required for comment")
			return
		}
		out, err = doComment(args)
	default:
		writeError(enc, id, -32602, "invalid arguments", "unknown operation "+args.Operation)
		return
	}
	if err != nil {
		writeError(enc, id, -32603, "github api failed", err.Error())
		return
	}
	bodyBytes, _ := json.Marshal(out)
	text := string(bodyBytes)
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
}

// doPush updates the remote branch ref to the local SHA. The endpoint
// is POST /repos/{owner}/{repo}/git/refs/heads/{branch} when the branch
// does not yet exist; for the common "push the local branch over its
// remote tracking ref" case the agent passes the local SHA after
// committing via shell_exec.
func doPush(args callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]string{
		"ref": "refs/heads/" + args.Branch,
		"sha": args.SHA,
	})
	if err != nil {
		return callOutput{}, err
	}
	out, err := githubRequest(http.MethodPost, args.Owner, args.Repo,
		"/git/refs/heads/"+args.Branch, body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "pushed", URL: out.Object.URL}, nil
}

// doOpenIssue creates an issue.
func doOpenIssue(args callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]any{
		"title":  args.Title,
		"body":   args.Body,
		"labels": args.Labels,
	})
	if err != nil {
		return callOutput{}, err
	}
	out, err := githubRequest(http.MethodPost, args.Owner, args.Repo, "/issues", body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "opened", URL: out.HTMLURL, Number: out.Number}, nil
}

// doOpenPullRequest creates a pull request.
func doOpenPullRequest(args callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]any{
		"title": args.Title,
		"body":  args.Body,
		"head":  args.Head,
		"base":  args.Base,
	})
	if err != nil {
		return callOutput{}, err
	}
	out, err := githubRequest(http.MethodPost, args.Owner, args.Repo, "/pulls", body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "opened", URL: out.HTMLURL, Number: out.Number}, nil
}

// doComment creates an issue or pull-request comment.
func doComment(args callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]any{
		"body": args.Message,
	})
	if err != nil {
		return callOutput{}, err
	}
	out, err := githubRequest(http.MethodPost, args.Owner, args.Repo,
		fmt.Sprintf("/issues/%d/comments", args.Number), body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "commented", URL: out.HTMLURL, ID: out.ID}, nil
}

// apiResponse is the subset of the GitHub response shape the tool reads.
// GitHub returns more fields per endpoint; the tool only needs the URL,
// number, and id to summarize the result for the agent.
type apiResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
	ID      int64  `json:"id"`
	Object  *struct {
		URL string `json:"url"`
	} `json:"object"`
}

// githubRequest sends one authenticated request to the GitHub REST
// API. It uses the GH_TOKEN environment variable for bearer auth; the
// token never appears in the response or in the returned error.
func githubRequest(method, owner, repo, path string, body []byte) (*apiResponse, error) {
	url := apiBaseURL() + "/repos/" + owner + "/" + repo + path
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GH_TOKEN"))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var er errorResponse
		_ = json.Unmarshal(respBody, &er)
		msg := er.Message
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s: %s", path, msg)
	}
	if len(respBody) == 0 {
		return &apiResponse{}, nil
	}
	var out apiResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", path, err)
	}
	return &out, nil
}

func writeError(enc *json.Encoder, id uint64, code int, msg, data string) {
	raw, _ := json.Marshal(map[string]any{
		"code":    code,
		"message": msg,
		"data":    data,
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Error   json.RawMessage `json:"error"`
	}{"2.0", id, raw})
}
