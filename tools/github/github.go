// github provides scoped repository work over the stdio tool contract.
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
	"regexp"
	"strings"
	"time"
)

const defaultAPIBase = "https://api.github.com"
const requestTimeout = 30 * time.Second

var httpClient = &http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
	return errors.New("redirect refused")
}}
var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`)
var objectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

type callInput struct {
	Operation string   `json:"operation"`
	Owner     string   `json:"owner"`
	Repo      string   `json:"repo"`
	SHA       string   `json:"sha"`
	Path      string   `json:"path"`
	Files     []string `json:"files"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Number    int      `json:"number"`
	Message   string   `json:"message"`
}

type callOutput struct {
	Status string   `json:"status"`
	URL    string   `json:"url,omitempty"`
	Number int      `json:"number,omitempty"`
	ID     int64    `json:"id,omitempty"`
	SHA    string   `json:"sha,omitempty"`
	Branch string   `json:"branch,omitempty"`
	Paths  []string `json:"paths,omitempty"`
}

type apiResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
	ID      int64  `json:"id"`
	SHA     string `json:"sha"`
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
	dec.DisallowUnknownFields()
	var in callInput
	if err := dec.Decode(&in); err != nil {
		return errors.New("invalid input JSON")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("input must be one JSON object")
	}
	target := in.Owner + "/" + in.Repo
	if !repositoryName.MatchString(target) || target != os.Getenv("GITHUB_REPOSITORY") || in.Repo == "." || in.Repo == ".." {
		return errors.New("repository is not authorized by GITHUB_REPOSITORY")
	}
	var out callOutput
	var err error
	switch in.Operation {
	case "inspect", "import_file", "publish":
		out, err = repositoryWork(in)
	case "open_issue":
		if in.Title == "" {
			return errors.New("title is required for open_issue")
		}
		body := map[string]any{"title": in.Title, "body": in.Body}
		if in.Labels != nil {
			body["labels"] = in.Labels
		}
		var resp apiResponse
		err = githubRequest(http.MethodPost, in, "/issues", body, &resp)
		out = callOutput{Status: "opened", URL: resp.HTMLURL, Number: resp.Number}
	case "comment":
		if in.Number <= 0 || in.Message == "" {
			return errors.New("number and message are required for comment")
		}
		var resp apiResponse
		err = githubRequest(http.MethodPost, in, fmt.Sprintf("/issues/%d/comments", in.Number), map[string]string{"body": in.Message}, &resp)
		out = callOutput{Status: "commented", URL: resp.HTMLURL, ID: resp.ID}
	default:
		return errors.New("unsupported operation")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func githubRequest(method string, in callInput, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("encode GitHub request")
		}
	}
	base := os.Getenv("GITHUB_API_BASE")
	if base == "" {
		base = defaultAPIBase
	}
	req, err := http.NewRequest(method, base+"/repos/"+in.Owner+"/"+in.Repo+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid GitHub API address")
	}
	token := os.Getenv("GH_TOKEN")
	if bytes.Contains(data, []byte(token)) {
		return errors.New("credential in request refused")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("GitHub request failed")
	}
	defer resp.Body.Close()
	// API errors and transport URLs can reflect credentials; never return them.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil || len(data) > 8*1024*1024 {
		return errors.New("GitHub response unreadable or too large")
	}
	if bytes.Contains(data, []byte(token)) {
		return errors.New("credential in response refused")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("invalid GitHub response")
	}
	normalized, err := json.Marshal(out)
	if err != nil || bytes.Contains(normalized, []byte(token)) {
		return errors.New("credential or invalid data in response refused")
	}
	return nil
}

func validBranch(branch string) bool {
	for _, r := range branch {
		if r < 32 || r == 127 {
			return false
		}
	}
	if branch == "" || strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, " ~^:?*[\\\x00\r\n\t") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || branch == "@" {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
