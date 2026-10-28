package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/garamsh/sherlock/internal/tool"
)

// studyTools are the tools each episode offers the agent: the shipped
// manifests of message_send and the three file tools, served from memory
// so the workspace an episode starts with is the family's and what the
// agent sends can be read back. shell_exec is not installed; a call to it
// fails as a missing tool does in production, in both arms alike.
var studyTools = []string{"file_list", "file_read", "file_write", "message_send"}

func loadDescriptors(dir string) ([]tool.Descriptor, error) {
	var out []tool.Descriptor
	for _, name := range studyTools {
		data, err := os.ReadFile(filepath.Join(dir, name, "manifest.json"))
		if err != nil {
			return nil, err
		}
		var d tool.Descriptor
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("%s manifest: %w", name, err)
		}
		out = append(out, d)
	}
	return out, nil
}

// workspace is one episode's files and outbound messages, kept across the
// episode's turns.
type workspace struct {
	descriptors []tool.Descriptor

	mu    sync.Mutex
	files map[string]string
	sent  []string
}

func newWorkspace(descriptors []tool.Descriptor, files map[string]string) *workspace {
	w := &workspace{descriptors: descriptors, files: map[string]string{}}
	for p, content := range files {
		w.files[cleanPath(p)] = content
	}
	return w
}

func (w *workspace) List() []tool.Descriptor { return w.descriptors }

// cleanPath is a workspace path relative to its root; "." and "/" name the
// root itself.
func cleanPath(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}

func (w *workspace) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Path    *string `json:"path"`
		Content *string `json:"content"`
		Channel string  `json:"channel"`
		Target  string  `json:"target"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("tool %q: invalid input: %v", name, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch name {
	case "message_send":
		if in.Content == nil || *in.Content == "" || in.Channel == "" || in.Target == "" {
			return nil, errors.New(`tool "message_send": channel, target and content are required`)
		}
		w.sent = append(w.sent, *in.Content)
		return json.RawMessage(`{"status":"queued"}`), nil
	case "file_write":
		if in.Path == nil || cleanPath(*in.Path) == "" || in.Content == nil {
			return nil, errors.New(`tool "file_write": path and content are required`)
		}
		p := cleanPath(*in.Path)
		if w.isDir(p) {
			return nil, fmt.Errorf(`tool "file_write": %s: is a directory`, *in.Path)
		}
		w.files[p] = *in.Content
		return json.RawMessage(fmt.Sprintf(`{"bytes_written":%d}`, len(*in.Content))), nil
	case "file_read":
		if in.Path == nil {
			return nil, errors.New(`tool "file_read": path is required`)
		}
		content, ok := w.files[cleanPath(*in.Path)]
		if !ok {
			return nil, fmt.Errorf(`tool "file_read": %s: no such file`, *in.Path)
		}
		out, _ := json.Marshal(map[string]string{"content": content})
		return out, nil
	case "file_list":
		if in.Path == nil {
			return nil, errors.New(`tool "file_list": path is required`)
		}
		dir := cleanPath(*in.Path)
		if _, isFile := w.files[dir]; isFile {
			return nil, fmt.Errorf(`tool "file_list": %s: not a directory`, *in.Path)
		}
		if dir != "" && !w.isDir(dir) {
			return nil, fmt.Errorf(`tool "file_list": %s: no such directory`, *in.Path)
		}
		out, _ := json.Marshal(map[string]any{"entries": w.entries(dir)})
		return out, nil
	}
	return nil, fmt.Errorf("tool %q not found", name)
}

func (w *workspace) isDir(dir string) bool {
	for p := range w.files {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// entries are a directory's immediate children, files and directories, by
// name.
func (w *workspace) entries(dir string) []map[string]any {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	seen := map[string]bool{}
	for p := range w.files {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		seen[name] = seen[name] || nested
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n, "dir": seen[n]})
	}
	return out
}

// takeSent returns what the agent sent since the last take.
func (w *workspace) takeSent() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	sent := w.sent
	w.sent = nil
	return sent
}
