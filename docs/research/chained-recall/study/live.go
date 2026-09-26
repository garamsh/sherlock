package study

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/tool"
)

// LiveProbe is one query the live stage asks.
type LiveProbe struct {
	ID     string
	Family string
	Query  string
}

// LiveProbes are every chain query and every guard query.
func (c *Corpus) LiveProbes() []LiveProbe {
	var out []LiveProbe
	for _, ch := range c.Chains {
		out = append(out, LiveProbe{ID: ch.ID, Family: ch.Family, Query: ch.Query})
	}
	for _, g := range c.Guards {
		out = append(out, LiveProbe{ID: g.ID, Family: g.Family, Query: g.Query})
	}
	return out
}

// ToolUse is one tool call a live turn made.
type ToolUse struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Result string          `json:"result"`
	Failed bool            `json:"failed,omitempty"`
}

// LiveResult is one live probe: what the turn was shown before it acted,
// what it called and what it answered.
type LiveResult struct {
	Probe   string            `json:"probe"`
	Family  string            `json:"family"`
	Arm     string            `json:"arm"`
	Setting string            `json:"setting"`
	Attempt int               `json:"attempt"`
	Reply   string            `json:"reply"`
	Sent    []string          `json:"sent,omitempty"`
	Written map[string]string `json:"written,omitempty"`
	Tools   []ToolUse         `json:"tools,omitempty"`
	// Recall is the turn's recall block, the frame message the session
	// added before the act loop.
	Recall     string   `json:"recall"`
	Calls      int      `json:"calls"`
	CallErrors []string `json:"call_errors,omitempty"`
	Error      string   `json:"error,omitempty"`
	Seconds    float64  `json:"seconds"`
}

// recorder keeps every exchange a turn made with the model.
type recorder struct {
	inner agent.Model
	mu    sync.Mutex
	calls []recorded
}

type recorded struct {
	messages []agent.Message
	err      error
}

func (r *recorder) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	resp, err := r.inner.Chat(ctx, messages, tools)
	r.mu.Lock()
	r.calls = append(r.calls, recorded{messages: append([]agent.Message(nil), messages...), err: err})
	r.mu.Unlock()
	return resp, err
}

type eventLog struct {
	mu     sync.Mutex
	events []agent.Event
}

func (l *eventLog) Emit(_ context.Context, e agent.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

// LiveDir is where a probe's store copy lives until scoring.
func (e *Env) LiveDir(arm, setting, probe string) string {
	return filepath.Join(e.Work, "live", arm, setting, probe)
}

// liveOnce runs one probe as one turn of a runtime built as production
// builds it (internal/gateway/runtime.go): the arm's store copied for the
// probe, the recent visible window restored, recall and memory_search
// answered by the arm, and the note, inspection and synthesis of the
// production store. The shared stores hold no pending batch, so the
// startup catch-up production runs has nothing to do and is not run.
func liveOnce(ctx context.Context, env *Env, arm Arm, setting string, p LiveProbe, descriptors []tool.Descriptor) LiveResult {
	started := time.Now()
	res := LiveResult{Probe: p.ID, Family: p.Family, Arm: arm.Name(), Setting: setting}
	dir := env.LiveDir(arm.Name(), setting, p.ID)
	if err := os.RemoveAll(dir); err != nil {
		res.Error = err.Error()
		return res
	}
	if err := CopyDir(env.BuildDir(arm.Name(), setting), dir); err != nil {
		res.Error = err.Error()
		return res
	}
	memory, err := arm.Open(ctx, env, setting, dir)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer memory.Close()
	inner, err := env.Model(p.ID, "live")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	rec := &recorder{inner: inner}
	store := memory.Store()
	window := env.Corpus.Window
	recent, err := store.Recent(ctx, window)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	initial := make([]agent.Message, 0, len(recent))
	for _, m := range recent {
		initial = append(initial, agent.Message{Role: m.Role, Content: m.Content})
	}
	ego := ""
	extra := arm.Instructions()
	if si, ok := arm.(SettingInstructor); ok {
		extra = si.InstructionsFor(setting)
	}
	if extra != "" {
		ego = agent.DefaultEgo + "\n\n" + extra
	}
	events := &eventLog{}
	ws := newWorkspace(descriptors)
	session := agent.NewSession(rec, ws, agent.Options{
		Recaller:           recaller{memory: memory},
		Conversation:       conversation{ConversationMemory: store, memory: memory},
		Summarizer:         agentmemory.ConversationSummary{Model: rec},
		InitialHistory:     initial,
		Events:             events,
		Compactor:          agentmemory.Compactor{MaxHistoryMessages: window},
		Ego:                ego,
		MaxHistoryMessages: window,
	})
	id := "msg-" + p.ID
	reply, err := session.Run(agent.WithInboundSources(ctx, []agent.SourceMessage{{ExternalID: id, Role: agent.RoleUser, Content: p.Query}}), fmt.Sprintf("[%s] owner: %s", id, p.Query))
	if err != nil {
		res.Error = err.Error()
	}
	res.Reply = reply
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall {
			res.Tools = append(res.Tools, ToolUse{Name: e.ToolName, Input: e.ToolInput, Result: e.ToolResult, Failed: e.ToolFailed})
		}
	}
	res.Sent, res.Written = ws.take()
	for _, c := range rec.calls {
		res.Calls++
		if c.err != nil {
			res.CallErrors = append(res.CallErrors, c.err.Error())
		}
		for _, m := range c.messages {
			if res.Recall == "" && m.Role == agent.RoleSystem && strings.HasPrefix(m.Content, "## Current task") {
				res.Recall = m.Content
			}
		}
	}
	res.Seconds = time.Since(started).Seconds()
	return res
}

// infrastructure reports whether a probe failed for a reason outside the
// arm: the provider's limits, a server fault or the network.
func infrastructure(r LiveResult) bool {
	text := r.Error + " " + strings.Join(r.CallErrors, " ")
	for _, marker := range []string{"429", "500", "502", "503", "504", "2056", "usage limit", "timeout", "connection", "EOF", "transport"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// Live runs every live probe for one setting, a few at a time, appending
// each result to evidence/live/<arm>/<setting>/results.jsonl. A probe that
// failed on infrastructure is run again once, after any pause; its first
// record moves to results-attempt1.jsonl. A run resumes after the probes
// already recorded.
func Live(ctx context.Context, env *Env, arm Arm, setting string, workers int, only map[string]bool) error {
	out := filepath.Join(env.Dir, "evidence", "live", arm.Name(), setting)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	done := map[string]bool{}
	if _, err := os.Stat(filepath.Join(out, "results.jsonl")); err == nil {
		prior, err := ReadJSONL[LiveResult](filepath.Join(out, "results.jsonl"))
		if err != nil {
			return err
		}
		for _, r := range prior {
			done[r.Probe] = true
		}
	}
	descriptors, err := loadDescriptors(filepath.Join(ResearchDir(env.Dir), "..", "..", "tools"))
	if err != nil {
		return err
	}
	results, err := os.OpenFile(filepath.Join(out, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer results.Close()
	firsts, err := os.OpenFile(filepath.Join(out, "results-attempt1.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer firsts.Close()
	jobs := make(chan LiveProbe)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				WaitWhilePaused(ctx, env.Proxy)
				r := liveOnce(ctx, env, arm, setting, p, descriptors)
				r.Attempt = 1
				if infrastructure(r) {
					mu.Lock()
					_ = writeLine(firsts, r)
					mu.Unlock()
					WaitWhilePaused(ctx, env.Proxy)
					r = liveOnce(ctx, env, arm, setting, p, descriptors)
					r.Attempt = 2
				}
				mu.Lock()
				_ = writeLine(results, r)
				mu.Unlock()
				log.Printf("%s %s %s: calls %d, error %q, %.0fs", arm.Name(), setting, p.ID, r.Calls, r.Error, r.Seconds)
			}
		}()
	}
	for _, p := range env.Corpus.LiveProbes() {
		if !done[p.ID] && (len(only) == 0 || only[p.ID]) {
			jobs <- p
		}
	}
	close(jobs)
	wg.Wait()
	return nil
}

// Paused asks the study proxy whether it is paused on the usage limit.
func Paused(proxy string) bool {
	resp, err := http.Get(strings.TrimSuffix(proxy, "/") + "/budget")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var state struct {
		Paused bool `json:"paused"`
	}
	return json.NewDecoder(bufio.NewReader(resp.Body)).Decode(&state) == nil && state.Paused
}

// WaitWhilePaused blocks while the proxy is paused, reading its local
// state once a minute; only the proxy probes the provider.
func WaitWhilePaused(ctx context.Context, proxy string) {
	logged := false
	for Paused(proxy) {
		if !logged {
			log.Print("proxy paused on the usage limit; waiting")
			logged = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

var errNoProxy = errors.New("the live stage needs -proxy")
