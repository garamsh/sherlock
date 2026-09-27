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
// so what the agent sends and writes can be read back. shell_exec is not
// installed; a call to it fails as a missing tool does in production.
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

// workspace is one episode's tool state: the files the agent wrote and
// the messages it sent, kept across the episode's turns.
type workspace struct {
	descriptors []tool.Descriptor

	mu    sync.Mutex
	files map[string]string
	sent  []string
	// written names the files written since the last take.
	written map[string]string
}

func newWorkspace(descriptors []tool.Descriptor) *workspace {
	return &workspace{descriptors: descriptors, files: map[string]string{}, written: map[string]string{}}
}

func (w *workspace) List() []tool.Descriptor { return w.descriptors }

func cleanPath(p string) string { return strings.TrimPrefix(path.Clean("/"+p), "/") }

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
		if in.Path == nil || *in.Path == "" || in.Content == nil {
			return nil, errors.New(`tool "file_write": path and content are required`)
		}
		p := cleanPath(*in.Path)
		w.files[p], w.written[p] = *in.Content, *in.Content
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
		var names []string
		for p := range w.files {
			names = append(names, p)
		}
		sort.Strings(names)
		entries := make([]map[string]any, 0, len(names))
		for _, n := range names {
			entries = append(entries, map[string]any{"name": n, "dir": false})
		}
		out, _ := json.Marshal(map[string]any{"entries": entries})
		return out, nil
	}
	return nil, fmt.Errorf("tool %q not found", name)
}

// take returns what the agent sent and wrote since the last take.
func (w *workspace) take() (sent []string, written map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sent, written = w.sent, w.written
	w.sent, w.written = nil, map[string]string{}
	return sent, written
}
