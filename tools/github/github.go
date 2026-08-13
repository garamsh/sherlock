// github is the platform's github tool. It implements the
// script-tool runner's stdio protocol (ADR 0009 §Decision): one JSON
// object on stdin (the validated input), one JSON object on stdout
// (the validated output), non-zero exit on failure. The tool reads
// GH_TOKEN from its environment (the platform's per-tool env
// contract), calls the GitHub REST API with stdlib net/http, and
// returns a structured summary. The token is never returned to the
// model.
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
)

const defaultAPIBase = "https://api.github.com"

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

type callOutput struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Number int    `json:"number,omitempty"`
	ID     int64  `json:"id,omitempty"`
}

type errorBody struct {
	Message string `json:"message"`
}

type apiResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
	ID      int64  `json:"id"`
	Object  *struct {
		URL string `json:"url"`
	} `json:"object"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "github:", err)
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
	var (
		out callOutput
		err error
	)
	switch in.Operation {
	case "push":
		if in.Branch == "" || in.SHA == "" {
			return errors.New("branch and sha are required for push")
		}
		out, err = doPush(in)
	case "open_issue":
		if in.Title == "" {
			return errors.New("title is required for open_issue")
		}
		out, err = doOpenIssue(in)
	case "open_pull_request":
		if in.Head == "" || in.Base == "" || in.Title == "" {
			return errors.New("head, base, and title are required for open_pull_request")
		}
		out, err = doOpenPullRequest(in)
	case "comment":
		if in.Number <= 0 || in.Message == "" {
			return errors.New("number and message are required for comment")
		}
		out, err = doComment(in)
	default:
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func apiBase() string {
	if v := os.Getenv("GITHUB_API_BASE"); v != "" {
		return v
	}
	return defaultAPIBase
}

func doPush(in callInput) (callOutput, error) {
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

func doOpenIssue(in callInput) (callOutput, error) {
	body := map[string]any{
		"title": in.Title,
		"body":  in.Body,
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	marshaled, err := json.Marshal(body)
	if err != nil {
		return callOutput{}, err
	}
	resp, err := githubRequest(http.MethodPost, in.Owner, in.Repo, "/issues", marshaled)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "opened", URL: resp.HTMLURL, Number: resp.Number}, nil
}

func doOpenPullRequest(in callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]any{
		"title": in.Title,
		"body":  in.Body,
		"head":  in.Head,
		"base":  in.Base,
	})
	if err != nil {
		return callOutput{}, err
	}
	resp, err := githubRequest(http.MethodPost, in.Owner, in.Repo, "/pulls", body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "opened", URL: resp.HTMLURL, Number: resp.Number}, nil
}

func doComment(in callInput) (callOutput, error) {
	body, err := json.Marshal(map[string]any{"body": in.Message})
	if err != nil {
		return callOutput{}, err
	}
	resp, err := githubRequest(http.MethodPost, in.Owner, in.Repo,
		fmt.Sprintf("/issues/%d/comments", in.Number), body)
	if err != nil {
		return callOutput{}, err
	}
	return callOutput{Status: "commented", URL: resp.HTMLURL, ID: resp.ID}, nil
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
	resp, err := http.DefaultClient.Do(req)
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
