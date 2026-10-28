package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// step is one step of a family as corpus.py writes it. A step's task and
// probe are the scorer's; the harness does not read them.
type step struct {
	Kind       string `json:"kind"`
	Index      int    `json:"index"`
	Content    string `json:"content"`
	Instance   int    `json:"instance"`
	Correction bool   `json:"correction"`
	Probe      any    `json:"probe"`
}

type family struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"`
	Group string            `json:"group"`
	Files map[string]string `json:"files"`
	Steps []step            `json:"steps"`
}

type corpusFile struct {
	Window   int            `json:"window"`
	Blocks   [][][2]message `json:"blocks"`
	Families []family       `json:"families"`
}

// toolUse is one tool call a turn made, from the session's event stream.
type toolUse struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Result string          `json:"result"`
	Failed bool            `json:"failed,omitempty"`
}

// callSummary is one model call of a turn, without what it was shown.
type callSummary struct {
	Kind      string   `json:"kind"`
	ToolCalls []string `json:"tool_calls,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// turn is one live input and everything it produced.
type turn struct {
	Step     int           `json:"step"`
	Instance int           `json:"instance"`
	Input    string        `json:"input"`
	Reply    string        `json:"reply"`
	Sent     []string      `json:"sent,omitempty"`
	Tools    []toolUse     `json:"tools,omitempty"`
	Calls    []callSummary `json:"calls"`
	Error    string        `json:"error,omitempty"`
	// Infra marks a turn that failed on HTTP 429, a server fault, a
	// transport error or the budget, which re-runs its episode.
	Infra bool `json:"infra,omitempty"`
	// CorrectionInWindow says whether the history a restart restored for
	// this turn holds an earlier correction of the family verbatim.
	CorrectionInWindow bool `json:"correction_in_window"`
	// RecallBlock says whether any act call of the turn was shown a
	// pre-turn recall block.
	RecallBlock bool `json:"recall_block"`
	// Memory is what the store holds after the turn beside the
	// conversation: S's note under "note", H's files by path.
	Memory  map[string]string `json:"memory"`
	Seconds float64           `json:"seconds"`
}

type result struct {
	Episode    string   `json:"episode"`
	Arm        string   `json:"arm"`
	Kind       string   `json:"kind"`
	Group      string   `json:"group"`
	Turns      []turn   `json:"turns"`
	SeedErrors []string `json:"seed_errors,omitempty"`
	Error      string   `json:"error,omitempty"`
	Infra      bool     `json:"infra,omitempty"`
	Seconds    float64  `json:"seconds"`
}

// recallHeading opens the shipped structure's pre-turn recall block
// (internal/agent/service.go frameContext at the S commit).
const recallHeading = "## Recalled memories"

type exchange struct {
	Tools    []string
	Messages []agent.Message
	Response agent.Response
	Err      error
}

func (e exchange) kind() string {
	if len(e.Tools) == 1 && e.Tools[0] == "memory_summary" {
		return "synthesis"
	}
	return "act"
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

type eventLog struct {
	mu     sync.Mutex
	events []agent.Event
}

func (l *eventLog) Emit(_ context.Context, e agent.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) take() []agent.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

// infraError matches a failure that says nothing about the arm: the
// provider's rate limit or fault, a transport error, or the ledger's
// ceiling. The summarizer flattens the model's error into text, so this is
// matched on the text.
var infraError = regexp.MustCompile(`openai provider: HTTP (402|429|5\d\d)|chat completions: `)

func isInfra(err error) bool {
	if err == nil {
		return false
	}
	var api *openai.APIError
	if errors.As(err, &api) {
		return api.StatusCode == 402 || api.StatusCode == 429 || api.StatusCode >= 500
	}
	return infraError.MatchString(err.Error())
}

type runnerConfig struct {
	arm, corpusPath, proxy, model string
	embedURL, embedModel          string
	toolsDir                      string
	window                        int
	// embedder, when set, replaces the embeddings endpoint.
	embedder agent.Embedder
}

type runner struct {
	cfg         runnerConfig
	corpus      *corpusFile
	embedder    agent.Embedder
	descriptors []tool.Descriptor
	window      int
}

func newRunner(cfg runnerConfig) (*runner, error) {
	data, err := os.ReadFile(cfg.corpusPath)
	if err != nil {
		return nil, err
	}
	var corpus corpusFile
	if err := json.Unmarshal(data, &corpus); err != nil {
		return nil, err
	}
	descriptors, err := loadDescriptors(cfg.toolsDir)
	if err != nil {
		return nil, err
	}
	embedder := cfg.embedder
	if embedder == nil {
		embedder, err = openai.NewEmbedder(openai.Config{BaseURL: cfg.embedURL, Model: cfg.embedModel})
		if err != nil {
			return nil, err
		}
	}
	window := cfg.window
	if window == 0 {
		window = corpus.Window
	}
	cfg.proxy = strings.TrimSuffix(cfg.proxy, "/")
	return &runner{cfg: cfg, corpus: &corpus, embedder: embedder, descriptors: descriptors, window: window}, nil
}

func selection(only string) map[string]bool {
	out := map[string]bool{}
	for _, id := range strings.Split(only, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	return out
}

// runAll runs every selected family the results file does not already hold
// complete. A family whose record failed on infrastructure is moved to a
// results-attempt file and run again from scratch.
func (r *runner) runAll(out string, only map[string]bool, workers int) error {
	resultsPath := filepath.Join(out, "results.jsonl")
	done, err := settle(resultsPath, filepath.Join(out, "results-attempt-"+time.Now().UTC().Format("20060102T150405")+".jsonl"))
	if err != nil {
		return err
	}
	var todo []family
	for _, f := range r.corpus.Families {
		if (len(only) == 0 || only[f.ID]) && !done[f.ID] {
			todo = append(todo, f)
		}
	}
	results, err := os.OpenFile(resultsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer results.Close()
	transcripts, err := os.OpenFile(filepath.Join(out, "transcripts-"+time.Now().UTC().Format("20060102T150405.000")+".jsonl.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer transcripts.Close()
	gz := gzip.NewWriter(transcripts)
	defer gz.Close()

	var mu sync.Mutex
	jobs := make(chan family)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				res, transcript := r.episode(context.Background(), f)
				mu.Lock()
				writeLine(results, res)
				writeLine(gz, map[string]any{"episode": f.ID, "arm": r.cfg.arm, "probes": transcript})
				_ = gz.Flush()
				mu.Unlock()
				log.Printf("%s %s: %d turns, infra=%v error=%q %.0fs", r.cfg.arm, f.ID, len(res.Turns), res.Infra, res.Error, res.Seconds)
			}
		}()
	}
	for _, f := range todo {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	return nil
}

// settle names the families the results file holds complete, and moves
// any record that failed on infrastructure to attempts, so the family runs
// again.
func settle(resultsPath, attemptsPath string) (map[string]bool, error) {
	done := map[string]bool{}
	var keep, failed [][]byte
	err := eachLine(resultsPath, func(line []byte) error {
		var res result
		if err := json.Unmarshal(line, &res); err != nil {
			return err
		}
		if res.Infra {
			failed = append(failed, append([]byte(nil), line...))
			return nil
		}
		done[res.Episode] = true
		keep = append(keep, append([]byte(nil), line...))
		return nil
	})
	if err != nil || len(failed) == 0 {
		return done, err
	}
	if err := appendLines(attemptsPath, failed); err != nil {
		return nil, err
	}
	if err := os.Remove(resultsPath); err != nil {
		return nil, err
	}
	return done, appendLines(resultsPath, keep)
}

func appendLines(path string, lines [][]byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func writeLine(w io.Writer, v any) {
	line, _ := json.Marshal(v)
	_, _ = w.Write(append(line, '\n'))
}

// modelFor points the production client at the proxy under this episode
// and phase, so the ledger attributes every call.
func (r *runner) modelFor(episode, phase string) (agent.Model, error) {
	return openai.New(openai.Config{BaseURL: fmt.Sprintf("%s/%s/%s/%s/v1", r.cfg.proxy, r.cfg.arm, episode, phase), Model: r.cfg.model, APIKey: "study", Timeout: 5 * time.Minute})
}

// episode runs one family on an empty store. Filler enters through
// RecordTurn, as completed turns do; each live input runs in a runtime
// built for it, so every input starts from what a restart restores.
func (r *runner) episode(ctx context.Context, f family) (result, []map[string]any) {
	started := time.Now()
	res := result{Episode: f.ID, Arm: r.cfg.arm, Kind: f.Kind, Group: f.Group}
	var transcript []map[string]any
	dir, err := os.MkdirTemp("", "general-agent-1164-"+r.cfg.arm+"-"+f.ID+"-")
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "memory.db")
	ws := newWorkspace(r.descriptors, f.Files)
	var pending [][2]message
	var corrections []string
	for i, s := range f.Steps {
		if s.Kind == "block" {
			pending = append(pending, r.corpus.Blocks[s.Index]...)
			continue
		}
		seedErrors, err := r.seed(ctx, f.ID, path, pending)
		res.SeedErrors = append(res.SeedErrors, seedErrors...)
		if err != nil {
			res.Error, res.Infra = fmt.Sprintf("seed before step %d: %v", i, err), isInfra(err)
			break
		}
		pending = nil
		t, exchanges := r.live(ctx, f.ID, path, ws, i, s, corrections)
		res.Turns = append(res.Turns, t)
		if s.Probe != nil {
			transcript = append(transcript, map[string]any{"step": i, "exchanges": compact(exchanges)})
		}
		if t.Infra {
			res.Error, res.Infra = fmt.Sprintf("step %d: %s", i, t.Error), true
			break
		}
		if s.Correction {
			corrections = append(corrections, s.Content)
		}
	}
	res.Seconds = time.Since(started).Seconds()
	return res, transcript
}

// seed records each filler pair as a completed turn. A synthesis the model
// answers without a usable call leaves its batch pending, as in
// production, and the next completed turn retries it; only an
// infrastructure failure stops the episode.
func (r *runner) seed(ctx context.Context, episode, path string, pairs [][2]message) ([]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	model, err := r.modelFor(episode, "seed")
	if err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(path, r.embedder)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	summarizer := memory.ConversationSummary{Model: model}
	var soft []string
	for _, p := range pairs {
		sources := []agent.SourceMessage{
			{Role: agent.Role(p[0].Role), Content: p[0].Content},
			{Role: agent.Role(p[1].Role), Content: p[1].Content},
		}
		if err := store.RecordTurn(ctx, sources, summarizer); err != nil {
			if isInfra(err) {
				return soft, err
			}
			soft = append(soft, err.Error())
		}
	}
	return soft, nil
}

func renderInput(episode string, step int, content string) string {
	return fmt.Sprintf("[msg-%s-%d] owner: %s", episode, step, content)
}

// live runs one input through the arm's production runtime, rendered and
// sourced as the gateway renders and sources a drained message.
func (r *runner) live(ctx context.Context, episode, path string, ws *workspace, index int, s step, corrections []string) (turn, []exchange) {
	started := time.Now()
	t := turn{Step: index, Instance: s.Instance, Input: s.Content}
	inner, err := r.modelFor(episode, fmt.Sprintf("live%d", index))
	if err != nil {
		t.Error = err.Error()
		return t, nil
	}
	rec := &recorder{inner: inner}
	events := &eventLog{}
	factory := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return rec }, Embedder: r.embedder, MaxHistoryMessages: r.window, Tools: ws}
	rt, err := factory.New(ctx, "sherlock-study", events)
	if err != nil {
		t.Error, t.Infra = fmt.Sprintf("runtime: %v", err), isInfra(err)
		return t, rec.take()
	}
	for _, m := range rt.Session.History() {
		for _, c := range corrections {
			if strings.Contains(m.Content, c) {
				t.CorrectionInWindow = true
			}
		}
	}
	id := fmt.Sprintf("msg-%s-%d", episode, index)
	reply, err := rt.Session.Run(agent.WithInboundSources(ctx, []agent.SourceMessage{{ExternalID: id, Role: agent.RoleUser, Content: s.Content}}), renderInput(episode, index, s.Content))
	if err != nil {
		t.Error, t.Infra = err.Error(), isInfra(err)
	}
	t.Reply = reply
	if err := rt.Close(ctx); err != nil && t.Error == "" {
		t.Error = fmt.Sprintf("close: %v", err)
	}
	for _, e := range events.take() {
		if e.Kind == agent.EventToolCall {
			t.Tools = append(t.Tools, toolUse{Name: e.ToolName, Input: e.ToolInput, Result: e.ToolResult, Failed: e.ToolFailed})
		}
	}
	t.Sent = ws.takeSent()
	exchanges := rec.take()
	for _, x := range exchanges {
		c := callSummary{Kind: x.kind()}
		for _, tc := range x.Response.ToolCalls {
			c.ToolCalls = append(c.ToolCalls, tc.Name)
		}
		if x.Err != nil {
			c.Error = x.Err.Error()
		}
		t.Calls = append(t.Calls, c)
		if c.Kind == "act" {
			for _, m := range x.Messages {
				if strings.Contains(m.Content, recallHeading) {
					t.RecallBlock = true
				}
			}
		}
	}
	if t.Memory, err = readMemory(path); err != nil && t.Error == "" {
		t.Error = fmt.Sprintf("read memory: %v", err)
	}
	t.Seconds = time.Since(started).Seconds()
	return t, exchanges
}

// readMemory reads what the store keeps beside the conversation straight
// from the file, so the harness needs no store API either arm lacks: S's
// note, and H's memory files and skills.
func readMemory(path string) (map[string]string, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := map[string]string{}
	var note string
	err = db.QueryRow(`SELECT content FROM conversation_note WHERE id=1`).Scan(&note)
	if err == nil {
		out["note"] = note
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var files int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agent_file'`).Scan(&files); err != nil {
		return nil, err
	}
	if files == 0 {
		return out, nil
	}
	rows, err := db.Query(`SELECT path, content FROM agent_file`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p, content string
		if err := rows.Scan(&p, &content); err != nil {
			return nil, err
		}
		out[p] = content
	}
	return out, rows.Err()
}

// compact keeps every answer of a turn but only the last act call's full
// context, which holds the earlier ones as its prefix.
func compact(exchanges []exchange) []map[string]any {
	last := -1
	for i, x := range exchanges {
		if x.kind() == "act" {
			last = i
		}
	}
	out := make([]map[string]any, 0, len(exchanges))
	for i, x := range exchanges {
		entry := map[string]any{"kind": x.kind(), "tools": x.Tools, "response": x.Response}
		if x.Err != nil {
			entry["error"] = x.Err.Error()
		}
		if i == last {
			entry["messages"] = x.Messages
		}
		out = append(out, entry)
	}
	return out
}
