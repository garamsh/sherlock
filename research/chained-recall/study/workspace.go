package study

// The tool workspace is agent-context/harness/tools.go at 03cbdff: the
// shipped manifests of the file tools and message_send, served from memory
// so what the agent sends and writes can be read back.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
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
	// associate answers memory_associate when the arm's memory is an
	// Associator; nil offers no such call.
	associate Associator

	mu    sync.Mutex
	files map[string]string
	sent  []string
	// written names the files written since the last take.
	written map[string]string
}

func newWorkspace(descriptors []tool.Descriptor) *workspace {
	return &workspace{descriptors: descriptors, files: map[string]string{}, written: map[string]string{}}
}

func (w *workspace) List() []tool.Descriptor {
	if w.associate == nil {
		return w.descriptors
	}
	return append(slices.Clone(w.descriptors), AssociateDescriptor())
}

// AssociateTool is L-T's call. It is served here, beside the file tools,
// because the study cannot add a reserved call to the session; the
// session's per-tool budget bounds it as it bounds memory_search.
const AssociateTool = "memory_associate"

// AssociateLimit bounds one call, as maxSearchResults bounds memory_search
// (internal/agent/search.go).
const AssociateLimit = 5

// AssociateDescriptor is memory_associate as the model sees it, written
// after memory_search's.
func AssociateDescriptor() tool.Descriptor {
	return tool.Descriptor{Name: AssociateTool, Description: "Find mid episodes linked to a name through the named entities their summaries share, following the links past the first episode. Put the person, place, organization or project in the query. Results are episodes associated through a shared name, not matched to the question; they are summaries with source intervals, not verified text; read exact wording with memory_inspect.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1}},"required":["query"],"additionalProperties":false}`)}
}

// associateHit is one memory_associate result, shaped as memory_search's.
type associateHit struct {
	ID          int64  `json:"mid"`
	Summary     string `json:"summary"`
	SourceFirst int64  `json:"source_first"`
	SourceLast  int64  `json:"source_last"`
}

func (w *workspace) callAssociate(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, errors.New("query must not be empty")
	}
	items, err := w.associate.Associate(ctx, query, AssociateLimit)
	if err != nil {
		return nil, err
	}
	out := make([]associateHit, 0, len(items))
	for _, it := range items {
		out = append(out, associateHit{ID: it.ID, Summary: strings.TrimSpace(it.Text), SourceFirst: it.SourceFirst, SourceLast: it.SourceLast})
	}
	return json.Marshal(out)
}

func cleanPath(p string) string { return strings.TrimPrefix(path.Clean("/"+p), "/") }

func (w *workspace) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if name == AssociateTool && w.associate != nil {
		return w.callAssociate(ctx, input)
	}
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
