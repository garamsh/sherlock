package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garamsh/gagent/internal/workspace"
)

func publish(in callInput, revision repositoryRevision, ws workspace.WorkspaceClient) (callOutput, error) {
	branch := os.Getenv("GITHUB_BRANCH")
	command := os.Getenv("GITHUB_VALIDATE_COMMAND")
	if !validBranch(branch) || branch == revision.branch {
		return callOutput{}, errors.New("GITHUB_BRANCH must name a non-default branch")
	}
	if command == "" || in.Title == "" || len(in.Files) == 0 || len(in.Files) > 100 {
		return callOutput{}, errors.New("publish requires validation command, title, and 1 to 100 files")
	}
	entries, err := snapshot(in.Files, revision, ws)
	if err != nil {
		return callOutput{}, err
	}
	// Validation runs only in the secret-free workspace, never in the tool process.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	result, err := ws.Exec(ctx, &workspace.ExecRequest{Command: command, NoNetwork: true})
	if err != nil {
		return callOutput{}, errors.New("workspace validation unavailable; nothing published")
	}
	if result.GetExitCode() != 0 || result.GetTimedOut() {
		return callOutput{}, errors.New("workspace validation failed; nothing published")
	}
	after, err := snapshot(in.Files, revision, ws)
	if err != nil {
		return callOutput{}, err
	}
	for i := range entries {
		if *entries[i].Content != *after[i].Content {
			return callOutput{}, errors.New("files changed during validation; nothing published")
		}
	}
	var tree apiResponse
	if err := githubRequest(http.MethodPost, in, "/git/trees", map[string]any{"base_tree": revision.tree, "tree": entries}, &tree); err != nil {
		return callOutput{}, err
	}
	if !objectID.MatchString(tree.SHA) || tree.SHA == revision.tree {
		return callOutput{}, errors.New("GitHub returned an invalid or unchanged tree")
	}
	var commit apiResponse
	if err := githubRequest(http.MethodPost, in, "/git/commits", map[string]any{"message": in.Title, "tree": tree.SHA, "parents": []string{revision.commit}}, &commit); err != nil {
		return callOutput{}, err
	}
	if !objectID.MatchString(commit.SHA) {
		return callOutput{}, errors.New("GitHub returned an invalid commit")
	}
	var ref struct {
		Ref string `json:"ref"`
	}
	// Create only: an existing branch is refused instead of moved or force-pushed.
	if err := githubRequest(http.MethodPost, in, "/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": commit.SHA}, &ref); err != nil {
		return callOutput{}, err
	}
	var pr apiResponse
	if err := githubRequest(http.MethodPost, in, "/pulls", map[string]any{"title": in.Title, "body": in.Body, "head": branch, "base": revision.branch, "draft": true}, &pr); err != nil {
		return callOutput{}, fmt.Errorf("branch created; draft PR failed (inspect the authorized branch before retrying): %v", err)
	}
	if pr.Number <= 0 || pr.HTMLURL == "" {
		return callOutput{}, errors.New("branch created; invalid PR response, inspect repository before retrying")
	}
	return callOutput{Status: "published", URL: pr.HTMLURL, Number: pr.Number, SHA: commit.SHA, Branch: branch, Paths: in.Files}, nil
}

func snapshot(paths []string, revision repositoryRevision, ws workspace.WorkspaceClient) ([]treeEntry, error) {
	entries := make([]treeEntry, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	total := 0
	for _, path := range paths {
		if !validPath(path) || seen[path] {
			return nil, errors.New("publish paths must be distinct relative file paths")
		}
		seen[path] = true
		mode := "100644"
		for _, existing := range revision.entries {
			if existing.Path == path {
				if existing.Type != "blob" || (existing.Mode != "100644" && existing.Mode != "100755") {
					return nil, errors.New("publish can only replace regular files")
				}
				mode = existing.Mode
			}
			if strings.HasPrefix(path, existing.Path+"/") && existing.Type != "tree" {
				return nil, errors.New("publish parent is not a directory")
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := ws.Read(ctx, &workspace.ReadRequest{Path: path})
		cancel()
		if err != nil {
			return nil, errors.New("workspace file read failed")
		}
		content := resp.GetContent()
		total += len(content)
		if len(content) > 1024*1024 || total > 4*1024*1024 || !utf8.Valid(content) {
			return nil, errors.New("publish supports UTF-8 files up to 1 MiB each and 4 MiB total")
		}
		if bytes.Contains(content, []byte(os.Getenv("GH_TOKEN"))) {
			return nil, errors.New("credential in workspace file refused")
		}
		text := string(content)
		entries = append(entries, treeEntry{Path: path, Mode: mode, Type: "blob", Content: &text})
	}
	return entries, nil
}
