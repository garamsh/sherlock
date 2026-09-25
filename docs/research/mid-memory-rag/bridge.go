// Command bridge runs the production conversation store, recall, summarizer
// and chat client for the mid-memory study. Model traffic goes to a local
// capture server: it records each production request body and answers with
// the next recorded provider response, so the study harness makes every paid
// call itself and replays it through the shipped decoding.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

type episode struct {
	Sources []struct {
		Role    agent.Role `json:"role"`
		Content string     `json:"content"`
	} `json:"sources"`
	// Request and Response are the recorded provider exchange that
	// synthesized this episode.
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type request struct {
	Mode      string            `json:"mode"`
	DB        string            `json:"db"`
	Corpus    []episode         `json:"corpus"`
	Updates   []string          `json:"updates"`
	Task      string            `json:"task"`
	Terms     []string          `json:"terms"`
	Limit     int               `json:"limit"`
	IDs       []int64           `json:"ids"`
	Messages  []agent.Message   `json:"messages"`
	Tools     []string          `json:"tools"`
	Responses []json.RawMessage `json:"responses"`
	// Direct recalls without a model: the task text is the search input.
	Direct bool `json:"direct"`
}

type output struct {
	Requests []json.RawMessage `json:"requests"`
	Result   any               `json:"result,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// capture answers production requests with recorded responses in order and
// refuses once they run out, which ends the production call with an error.
type capture struct {
	mu        sync.Mutex
	requests  []json.RawMessage
	responses []json.RawMessage
}

func (c *capture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, body)
	if len(c.responses) == 0 {
		http.Error(w, "no recorded response", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(c.responses[0])
	c.responses = c.responses[1:]
}

// replaySummary synthesizes one episode from its recorded response through the
// production summarizer, so RecordTurn applies the shipped validation. The
// batch the store hands it must reproduce the recorded request exactly.
type replaySummary struct{ request, response json.RawMessage }

func (s replaySummary) Summarize(ctx context.Context, updates []string) (string, error) {
	summary, requests, err := summarize(ctx, updates, []json.RawMessage{s.response})
	if len(requests) != 1 || !sameJSON(requests[0], s.request) {
		return "", errors.New("store batch differs from the recorded synthesis request")
	}
	return summary, err
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

func model(c *capture) (agent.Model, func(), error) {
	server := httptest.NewServer(c)
	m, err := openai.New(openai.Config{BaseURL: server.URL, Model: "MiniMax-M2", APIKey: "capture"})
	if err != nil {
		server.Close()
		return nil, nil, err
	}
	return m, server.Close, nil
}

func summarize(ctx context.Context, updates []string, responses []json.RawMessage) (string, []json.RawMessage, error) {
	c := &capture{responses: responses}
	m, stop, err := model(c)
	if err != nil {
		return "", nil, err
	}
	defer stop()
	summary, err := memory.ConversationSummary{Model: m}.Summarize(ctx, updates)
	return summary, c.requests, err
}

func build(ctx context.Context, req request) (any, error) {
	store, err := sqlite.OpenConversation(req.DB)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	for i, ep := range req.Corpus {
		var sources []agent.SourceMessage
		for _, s := range ep.Sources {
			sources = append(sources, agent.SourceMessage{Role: s.Role, Content: s.Content})
		}
		if err := store.RecordTurn(ctx, sources, replaySummary{ep.Request, ep.Response}); err != nil {
			return nil, fmt.Errorf("episode %d: %w", i+1, err)
		}
	}
	pending, err := store.PendingSources(ctx)
	if err != nil {
		return nil, err
	}
	if len(pending) != 0 {
		return nil, errors.New("corpus left pending sources")
	}
	return store.RecentMid(ctx, len(req.Corpus)+1)
}

func descriptors(names []string) ([]tool.Descriptor, error) {
	var out []tool.Descriptor
	for _, name := range names {
		if name != agent.MemoryInspectTool {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		out = append(out, agent.MemoryInspectDescriptor())
	}
	return out, nil
}

func run(ctx context.Context, req request) output {
	var out output
	var result any
	var err error
	switch req.Mode {
	case "synthesize":
		result, out.Requests, err = summarize(ctx, req.Updates, req.Responses)
	case "build":
		result, err = build(ctx, req)
	case "recall", "search", "recent", "source":
		result, out.Requests, err = read(ctx, req)
	case "chat":
		c := &capture{responses: req.Responses}
		var m agent.Model
		var stop func()
		if m, stop, err = model(c); err == nil {
			defer stop()
			var tools []tool.Descriptor
			if tools, err = descriptors(req.Tools); err == nil {
				result, err = m.Chat(ctx, req.Messages, tools)
			}
			out.Requests = c.requests
		}
	case "tokens":
		result = agent.SearchTokens(req.Terms)
	default:
		err = fmt.Errorf("unknown mode %q", req.Mode)
	}
	out.Result = result
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

func read(ctx context.Context, req request) (any, []json.RawMessage, error) {
	store, err := sqlite.OpenConversation(req.DB)
	if err != nil {
		return nil, nil, err
	}
	defer store.Close()
	switch req.Mode {
	case "search":
		result, err := store.Search(ctx, req.Terms, req.Limit)
		return result, nil, err
	case "recent":
		result, err := store.RecentMid(ctx, req.Limit)
		return result, nil, err
	case "source":
		sources := []agent.SourceMessage{}
		for _, id := range req.IDs {
			source, err := store.Source(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			sources = append(sources, source)
		}
		return sources, nil, nil
	}
	recall := memory.ConversationRecall{Store: store}
	if req.Direct {
		result, err := recall.Recall(ctx, req.Task)
		return result, nil, err
	}
	c := &capture{responses: req.Responses}
	m, stop, err := model(c)
	if err != nil {
		return nil, nil, err
	}
	defer stop()
	recall.Model = m
	result, err := recall.Recall(ctx, req.Task)
	return result, c.requests, err
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(run(context.Background(), req)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
