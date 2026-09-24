package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
)

type repositoryRevision struct {
	branch  string
	commit  string
	tree    string
	entries []treeEntry
}

type treeEntry struct {
	Path    string  `json:"path"`
	Mode    string  `json:"mode"`
	Type    string  `json:"type"`
	SHA     string  `json:"sha,omitempty"`
	Content *string `json:"content,omitempty"`
}

func repositoryWork(in callInput) (callOutput, error) {
	revision, err := readRevision(in)
	if err != nil {
		return callOutput{}, err
	}
	if in.Operation == "inspect" {
		paths := make([]string, 0, len(revision.entries))
		for _, entry := range revision.entries {
			paths = append(paths, entry.Path)
		}
		return callOutput{Status: "inspected", SHA: revision.commit, Branch: revision.branch, Paths: paths}, nil
	}
	if in.SHA != revision.commit {
		return callOutput{}, errors.New("sha must match the current inspected default revision")
	}
	addr := os.Getenv("GAGENT_WORKSPACE_ADDR")
	if addr == "" {
		return callOutput{}, errors.New("GAGENT_WORKSPACE_ADDR is required")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return callOutput{}, errors.New("workspace connection failed")
	}
	defer conn.Close()
	ws := workspace.NewWorkspaceClient(conn)
	if in.Operation == "import_file" {
		return importFile(in, revision, ws)
	}
	return publish(in, revision, ws)
}

func readRevision(in callInput) (repositoryRevision, error) {
	var metadata struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := githubRequest(http.MethodGet, in, "", nil, &metadata); err != nil {
		return repositoryRevision{}, err
	}
	if !validBranch(metadata.DefaultBranch) {
		return repositoryRevision{}, errors.New("invalid default branch")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := githubRequest(http.MethodGet, in, "/git/ref/heads/"+url.PathEscape(metadata.DefaultBranch), nil, &ref); err != nil {
		return repositoryRevision{}, err
	}
	if !objectID.MatchString(ref.Object.SHA) {
		return repositoryRevision{}, errors.New("invalid commit SHA")
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := githubRequest(http.MethodGet, in, "/git/commits/"+ref.Object.SHA, nil, &commit); err != nil {
		return repositoryRevision{}, err
	}
	if !objectID.MatchString(commit.Tree.SHA) {
		return repositoryRevision{}, errors.New("invalid tree SHA")
	}
	var tree struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := githubRequest(http.MethodGet, in, "/git/trees/"+commit.Tree.SHA+"?recursive=1", nil, &tree); err != nil {
		return repositoryRevision{}, err
	}
	if tree.Truncated {
		return repositoryRevision{}, errors.New("repository tree is truncated")
	}
	for _, entry := range tree.Tree {
		if !validPath(entry.Path) || !objectID.MatchString(entry.SHA) {
			return repositoryRevision{}, errors.New("invalid repository tree entry")
		}
	}
	return repositoryRevision{branch: metadata.DefaultBranch, commit: ref.Object.SHA, tree: commit.Tree.SHA, entries: tree.Tree}, nil
}

func validPath(path string) bool {
	if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func importFile(in callInput, revision repositoryRevision, ws workspace.WorkspaceClient) (callOutput, error) {
	for _, entry := range revision.entries {
		if entry.Path != in.Path {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			return callOutput{}, errors.New("only regular files can be imported")
		}
		var blob struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := githubRequest(http.MethodGet, in, "/git/blobs/"+entry.SHA, nil, &blob); err != nil {
			return callOutput{}, err
		}
		content, err := base64.StdEncoding.DecodeString(blob.Content)
		if err != nil || blob.Encoding != "base64" || len(content) > 1024*1024 || !utf8.Valid(content) {
			return callOutput{}, errors.New("only UTF-8 files up to 1 MiB can be imported")
		}
		if strings.Contains(string(content), os.Getenv("GH_TOKEN")) {
			return callOutput{}, errors.New("credential in file refused")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := ws.Write(ctx, &workspace.WriteRequest{Path: in.Path, Content: content}); err != nil {
			return callOutput{}, errors.New("workspace import failed")
		}
		return callOutput{Status: "imported", SHA: revision.commit, Paths: []string{in.Path}}, nil
	}
	return callOutput{}, errors.New("path is absent from the inspected repository")
}
