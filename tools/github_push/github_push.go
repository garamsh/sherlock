// github_push creates a branch on GitHub at an existing commit over the script-tool runner's stdio
// protocol (ADR 0009 §Decision): one JSON object on stdin (the validated
// input), one JSON object on stdout (the validated output), non-zero exit
// on failure. The tool reads GH_TOKEN from its environment (the
// platform's per-tool env contract), calls the GitHub REST API with
// stdlib net/http, and returns a structured summary. The token is never
// returned to the model.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const defaultAPIBase = "https://api.github.com"

// requestTimeout bounds one GitHub API call end to end. The call is a
// single small REST write over a remote round trip, so a call the
// endpoint answers at all returns far inside it. Nothing else bounds
// one: no operator setting reaches this call, and an endpoint that
// accepts the connection and then answers nothing is by definition
// enforcing no limit of its own.
const requestTimeout = 30 * time.Second

// httpClient carries requestTimeout on every call. http.DefaultClient
// sets no Timeout, so a call made on it waits until the process exits
// (issue #739).
var httpClient = &http.Client{Timeout: requestTimeout}

type callInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

type callOutput struct {
	Status string `json:"status"`
	URL    string `json:"url"`
}

type errorBody struct {
	Message string `json:"message"`
}

type apiResponse struct {
	Object *struct {
		URL string `json:"url"`
	} `json:"object"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "github_push:", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("GH_TOKEN") == "" {
		return errors.New("GH_TOKEN is required")
	}
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	var in callInput
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	if in.Owner == "" || in.Repo == "" {
		return errors.New("owner and repo are required")
	}
	if in.Branch == "" || in.SHA == "" {
		return errors.New("branch and sha are required")
	}
	out, err := call(in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func call(in callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]string{
		"ref": "refs/heads/" + in.Branch,
		"sha": in.SHA,
	})
	if err != nil {
		return callOutput{}, err
	}
	resp, err := githubRequest(http.MethodPost, in.Owner, in.Repo,
		"/git/refs/heads/"+in.Branch, body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "pushed", URL: resp.Object.URL}, nil
}

func apiBase() string {
	if v := os.Getenv("GITHUB_API_BASE"); v != "" {
		return v
	}
	return defaultAPIBase
}

func githubRequest(method, owner, repo, path string, body []byte) (*apiResponse, error) {
	url := apiBase() + "/repos/" + owner + "/" + repo + path
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
		var er errorBody
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
