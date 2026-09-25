package main

import (
	"context"
	"strings"
	"sync"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// exchange is one chat call as the session made it: what the model was
// shown and what it answered.
type exchange struct {
	Tools    []string
	Messages []agent.Message
	Response agent.Response
	Err      error
}

// recorder stands between the session and the production client and keeps
// every exchange, so the study reads the context each call actually had.
type recorder struct {
	inner agent.Model

	mu        sync.Mutex
	exchanges []exchange
}

func (r *recorder) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	resp, err := r.inner.Chat(ctx, messages, tools)
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	r.mu.Lock()
	r.exchanges = append(r.exchanges, exchange{Tools: names, Messages: append([]agent.Message(nil), messages...), Response: resp, Err: err})
	r.mu.Unlock()
	return resp, err
}

func (r *recorder) take() []exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.exchanges
	r.exchanges = nil
	return out
}

// kind names an exchange by the one reserved tool it offers, as the proxy
// does.
func (e exchange) kind() string {
	if len(e.Tools) == 1 {
		switch e.Tools[0] {
		case "memory_summary":
			return "synthesis"
		case "memory_understand":
			return "understand"
		}
	}
	return "act"
}

// actContext is what an act call had in view that the turn's own model
// output did not put there: everything before the turn's input message,
// the input itself, and the tool results that followed it.
func actContext(e exchange, input string) []string {
	start := len(e.Messages)
	for i := len(e.Messages) - 1; i >= 0; i-- {
		if e.Messages[i].Role == agent.RoleUser && e.Messages[i].Content == input {
			start = i
			break
		}
	}
	var out []string
	for i, m := range e.Messages {
		if i <= start || m.Role == agent.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// reached reports whether any act call of a turn had a key in view.
func reached(exchanges []exchange, input string, keys []string) bool {
	for _, e := range exchanges {
		if e.kind() != "act" {
			continue
		}
		for _, text := range actContext(e, input) {
			if containsAny(text, keys) {
				return true
			}
		}
	}
	return false
}

func containsAny(text string, keys []string) bool {
	lower := strings.ToLower(text)
	for _, k := range keys {
		if k != "" && strings.Contains(lower, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
