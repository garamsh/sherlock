package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

type pair []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type step struct {
	Kind    string `json:"kind"`
	Index   int    `json:"index"`
	Pairs   []pair `json:"pairs"`
	Content string `json:"content"`
	// Messages are an align step's same-topic messages, of which the
	// harness seeds as many as close the current batch.
	Messages pair `json:"messages"`
}

type episode struct {
	ID     string          `json:"id"`
	Family string          `json:"family"`
	Steps  []step          `json:"steps"`
	Check  json.RawMessage `json:"check"`
	Reach  []string        `json:"reach"`
	Oracle string          `json:"oracle"`
	// Followups are the user messages an unmixed episode puts in its
	// evidence batch beside the statement (issue #870).
	Followups []string `json:"followups"`
}

type corpusFile struct {
	Window   int       `json:"window"`
	Blocks   [][]pair  `json:"blocks"`
	Pads     []pair    `json:"pads"`
	Episodes []episode `json:"episodes"`
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
	Step        int               `json:"step"`
	Input       string            `json:"input"`
	Reply       string            `json:"reply"`
	Error       string            `json:"error,omitempty"`
	Tools       []toolUse         `json:"tools,omitempty"`
	Sent        []string          `json:"sent,omitempty"`
	Written     map[string]string `json:"written,omitempty"`
	Calls       []callSummary     `json:"calls"`
	Understood  *understanding    `json:"understood,omitempty"`
	RecalledIDs []string          `json:"recalled_ids,omitempty"`
	NoteAfter   *string           `json:"note_after,omitempty"`
	Seconds     float64           `json:"seconds"`
}

// understanding is what the understanding step answered, read from its
// exchange: whether it answered through its reserved call, and with what.
type understanding struct {
	Called   bool            `json:"called"`
	Answered json.RawMessage `json:"answered,omitempty"`
}

type result struct {
	Episode   string `json:"episode"`
	Family    string `json:"family"`
	Arm       string `json:"arm"`
	Turns     []turn `json:"turns"`
	Reached   bool   `json:"reached"`
	Inspected bool   `json:"inspected"`
	// Oracle says whether the evidence source was reachable at the probe
	// (oracleCheck); OracleDetail says how. Nil where the episode has no
	// evidence.
	Oracle       *bool        `json:"oracle,omitempty"`
	OracleDetail *oracleCheck `json:"oracle_detail,omitempty"`
	Error        string       `json:"error,omitempty"`
	Seconds      float64      `json:"seconds"`
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

type runner struct {
	arm         string
	corpus      *corpusFile
	base        string
	proxy       string
	embedder    agent.Embedder
	descriptors []tool.Descriptor
	scratch     string
	// keep, when set, is where each episode's store is copied after its
	// probe, for diagnosis before scoring.
	keep string
}

func runArm(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	arm := fs.String("arm", "", "arm name: S, A or B")
	corpusPath := fs.String("episodes", "docs/research/agent-context/episodes.json", "episodes file")
	base := fs.String("base", "", "base store")
	out := fs.String("out", "", "output directory for results.jsonl and transcripts.jsonl.gz")
	proxyURL := fs.String("proxy", "http://127.0.0.1:8091", "study proxy")
	embedURL := fs.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model name")
	toolsDir := fs.String("tools", "tools", "directory holding the shipped tool manifests")
	only := fs.String("only", "", "comma-separated episode IDs to run; empty runs all")
	workers := fs.Int("workers", 4, "episodes run at once")
	keep := fs.String("keep-stores", "", "directory to copy each episode's store into after its probe")
	_ = fs.Parse(args)
	if *arm == "" || *base == "" || *out == "" {
		return errors.New("run needs -arm, -base and -out")
	}
	data, err := os.ReadFile(*corpusPath)
	if err != nil {
		return err
	}
	var corpus corpusFile
	if err := json.Unmarshal(data, &corpus); err != nil {
		return err
	}
	descriptors, err := loadDescriptors(*toolsDir)
	if err != nil {
		return err
	}
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: *embedURL, Model: *embedModel})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	done, err := finished(filepath.Join(*out, "results.jsonl"))
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(*only, ",") {
		if id = strings.TrimSpace(id); id != "" {
			selected[id] = true
		}
	}
	var todo []episode
	for _, e := range corpus.Episodes {
		if (len(selected) == 0 || selected[e.ID]) && !done[e.ID] {
			todo = append(todo, e)
		}
	}
	scratch, err := os.MkdirTemp("", "agent-context-862-"+*arm+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	r := &runner{arm: *arm, corpus: &corpus, base: *base, proxy: strings.TrimSuffix(*proxyURL, "/"), embedder: embedder, descriptors: descriptors, scratch: scratch, keep: *keep}
	if r.keep != "" {
		if err := os.MkdirAll(r.keep, 0o755); err != nil {
			return err
		}
	}

	results, err := os.OpenFile(filepath.Join(*out, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer results.Close()
	transcripts, err := os.OpenFile(filepath.Join(*out, "transcripts-"+time.Now().UTC().Format("20060102T150405")+".jsonl.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer transcripts.Close()
	gz := gzip.NewWriter(transcripts)
	defer gz.Close()

	var mu sync.Mutex
	jobs := make(chan episode)
	var wg sync.WaitGroup
	for range *workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				res, transcript := r.episode(context.Background(), e)
				mu.Lock()
				writeLine(results, res)
				writeLine(gz, map[string]any{"episode": e.ID, "arm": r.arm, "exchanges": transcript})
				_ = gz.Flush()
				mu.Unlock()
				log.Printf("%s %s: reached=%v error=%q %.0fs", r.arm, e.ID, res.Reached, res.Error, res.Seconds)
			}
		}()
	}
	for _, e := range todo {
		jobs <- e
	}
	close(jobs)
	wg.Wait()
	return nil
}

func writeLine(w io.Writer, v any) {
	line, _ := json.Marshal(v)
	_, _ = w.Write(append(line, '\n'))
}

// finished names the episodes a results file already holds, so a stopped
// run resumes after them.
func finished(path string) (map[string]bool, error) {
	out := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var r result
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return nil, err
		}
		out[r.Episode] = true
	}
	return out, scanner.Err()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// modelFor points the production client at the proxy under this episode
// and phase, so the ledger attributes every call.
func (r *runner) modelFor(episode, phase string) (agent.Model, error) {
	return openai.New(openai.Config{BaseURL: fmt.Sprintf("%s/%s/%s/%s/v1", r.proxy, r.arm, episode, phase), Model: "MiniMax-M2", APIKey: "study", Timeout: 5 * time.Minute})
}

// episode runs one episode on a fresh copy of the base store. Seeded pairs
// enter through RecordTurn, as completed turns do; each live input runs in
// a runtime built for it, so every input starts from what a restart
// restores.
func (r *runner) episode(ctx context.Context, e episode) (result, []map[string]any) {
	started := time.Now()
	res := result{Episode: e.ID, Family: e.Family, Arm: r.arm}
	var transcript []map[string]any
	dir, err := os.MkdirTemp(r.scratch, e.ID+"-")
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "memory.db")
	if err := copyFile(r.base, path); err != nil {
		res.Error = err.Error()
		return res, nil
	}
	ws := newWorkspace(r.descriptors)
	var pending []pair
	liveSteps := 0
	for _, s := range e.Steps {
		if s.Kind == "live" {
			liveSteps++
		}
	}
	liveSeen := 0
	for i, s := range e.Steps {
		switch s.Kind {
		case "block":
			pending = append(pending, r.corpus.Blocks[s.Index]...)
			continue
		case "pad":
			pending = append(pending, r.corpus.Pads[s.Index])
			continue
		case "seed":
			pending = append(pending, s.Pairs...)
			continue
		case "align":
			if err := r.seed(ctx, e.ID, path, pending); err != nil {
				res.Error = fmt.Sprintf("seed before step %d: %v", i, err)
				return res, transcript
			}
			pending = nil
			if err := r.align(ctx, e.ID, path, s.Messages); err != nil {
				res.Error = fmt.Sprintf("align at step %d: %v", i, err)
				return res, transcript
			}
			continue
		}
		if err := r.seed(ctx, e.ID, path, pending); err != nil {
			res.Error = fmt.Sprintf("seed before step %d: %v", i, err)
			return res, transcript
		}
		pending = nil
		liveSeen++
		probe := liveSeen == liveSteps
		t, exchanges := r.live(ctx, e.ID, path, ws, i, s.Content, probe)
		if probe {
			input := renderInput(e.ID, i, s.Content)
			res.Reached = reached(exchanges, input, e.Reach)
			for _, u := range t.Tools {
				if u.Name == agent.MemoryInspectTool && !u.Failed && containsAny(u.Result, e.Reach) {
					res.Inspected = true
				}
			}
			transcript = append(transcript, map[string]any{"step": i, "exchanges": compact(exchanges)})
		}
		res.Turns = append(res.Turns, t)
	}
	if e.Oracle != "" {
		check, err := r.oracle(ctx, path, e.Oracle, e.Reach, e.Followups)
		if err != nil {
			res.Error = fmt.Sprintf("oracle: %v", err)
		} else {
			res.Oracle, res.OracleDetail = &check.Reached, &check
		}
		if r.keep != "" {
			if err := copyFile(path, filepath.Join(r.keep, e.ID+".db")); err != nil && res.Error == "" {
				res.Error = fmt.Sprintf("keep store: %v", err)
			}
		}
	}
	res.Seconds = time.Since(started).Seconds()
	return res, transcript
}

// oracleCheck says whether the evidence was reachable through the arm's
// own store, at the level of sources: a mid whose interval holds the
// evidence source ranks among the top five for the evidence's own
// sentence. A miss is classified from the same store: no mid holds the
// source yet, or the mid that holds it ranks lower and its summary does
// or does not state the key.
type oracleCheck struct {
	Reached bool    `json:"reached"`
	Sources []int64 `json:"sources"`
	Mid     int64   `json:"mid,omitempty"`
	Rank    int     `json:"rank,omitempty"`
	Summary string  `json:"summary,omitempty"`
	// KeyStated says whether the holding mid's summary states the key.
	KeyStated bool `json:"key_stated"`
	// Class is empty when reached, else "unsummarized", "reworded
	// summary" (the holding mid's summary does not state the key) or
	// "ranking miss" (it does, and still ranks below five).
	Class string `json:"class,omitempty"`
	// Unmixed, for an episode that declares its follow-ups, says whether
	// the evidence source's batch holds only its own exchange.
	Unmixed *bool `json:"unmixed,omitempty"`
}

// unmixedBatch reports whether the batch holding source is the evidence
// exchange alone: a mid spans exactly the source and the three messages
// after it, and each of those is an assistant message or one of the
// episode's declared same-topic follow-ups.
func unmixedBatch(ctx context.Context, db *sql.DB, source int64, followups []string) (bool, error) {
	var first, last int64
	err := db.QueryRowContext(ctx, `SELECT source_first, source_last FROM conversation_mid WHERE source_first <= ? AND source_last >= ?`, source, source).Scan(&first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if first != source || last != source+batchSize-1 {
		return false, nil
	}
	allowed := map[string]bool{}
	for _, f := range followups {
		allowed[f] = true
	}
	rows, err := db.QueryContext(ctx, `SELECT role, content FROM conversation_sources WHERE id > ? AND id <= ? ORDER BY id`, first, last)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return false, err
		}
		n++
		if role != "assistant" && !allowed[content] {
			return false, nil
		}
	}
	return n == batchSize-1, rows.Err()
}

// oracleTop is how many mids a reached evidence source may rank within.
const oracleTop = 5

func (r *runner) oracle(ctx context.Context, path, statement string, keys, followups []string) (oracleCheck, error) {
	var out oracleCheck
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return out, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM conversation_sources WHERE role='user' AND content=? ORDER BY id`, statement)
	if err != nil {
		db.Close()
		return out, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			db.Close()
			return out, err
		}
		out.Sources = append(out.Sources, id)
	}
	rows.Close()
	if len(out.Sources) == 0 {
		db.Close()
		return out, fmt.Errorf("no stored source is the evidence statement %q", statement)
	}
	if followups != nil {
		unmixed, err := unmixedBatch(ctx, db, out.Sources[len(out.Sources)-1], followups)
		if err != nil {
			db.Close()
			return out, err
		}
		out.Unmixed = &unmixed
	}
	db.Close()
	store, err := sqlite.OpenConversation(path, r.embedder)
	if err != nil {
		return out, err
	}
	defer store.Close()
	ranked, err := store.Search(ctx, statement, 1<<20)
	if err != nil {
		return out, err
	}
	for i, h := range ranked {
		held := false
		for _, id := range out.Sources {
			held = held || (h.SourceFirst <= id && id <= h.SourceLast)
		}
		if !held {
			continue
		}
		out.Mid, out.Rank, out.Summary = h.ID, i+1, h.Summary
		out.KeyStated = containsAny(h.Summary, keys)
		out.Reached = out.Rank <= oracleTop
		switch {
		case out.Reached:
		case out.KeyStated:
			out.Class = "ranking miss"
		default:
			out.Class = "reworded summary"
		}
		return out, nil
	}
	out.Class = "unsummarized"
	return out, nil
}

// align seeds as many of messages as close the batch the stored sources
// leave open, in one completed turn, so a live turn's exchange fills its
// batch alone whatever number of messages the turn stored.
func (r *runner) align(ctx context.Context, episode, path string, messages pair) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	var stored int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_sources`).Scan(&stored)
	db.Close()
	if err != nil {
		return err
	}
	need, err := alignNeed(stored, len(messages))
	if err != nil || need == 0 {
		return err
	}
	return r.seed(ctx, episode, path, []pair{messages[:need]})
}

// alignNeed is how many messages close the batch that stored sources
// leave open.
func alignNeed(stored, offered int) (int, error) {
	need := (batchSize - stored%batchSize) % batchSize
	if need > offered {
		return 0, fmt.Errorf("batch needs %d messages, the step offers %d", need, offered)
	}
	return need, nil
}

// batchSize is the store's batch, four completed messages since #859.
const batchSize = 4

func (r *runner) seed(ctx context.Context, episode, path string, pairs []pair) error {
	if len(pairs) == 0 {
		return nil
	}
	model, err := r.modelFor(episode, "seed")
	if err != nil {
		return err
	}
	store, err := sqlite.OpenConversation(path, r.embedder)
	if err != nil {
		return err
	}
	summarizer := memory.ConversationSummary{Model: model}
	for _, p := range pairs {
		sources := make([]agent.SourceMessage, len(p))
		for j, m := range p {
			sources[j] = agent.SourceMessage{Role: agent.Role(m.Role), Content: m.Content}
		}
		if err := store.RecordTurn(ctx, sources, summarizer); err != nil {
			store.Close()
			return err
		}
	}
	return store.Close()
}

func renderInput(episode string, step int, content string) string {
	return fmt.Sprintf("[msg-%s-%d] owner: %s", episode, step, content)
}

// live runs one input through the arm's production runtime, rendered and
// sourced as the gateway renders and sources a drained message.
func (r *runner) live(ctx context.Context, episode, path string, ws *workspace, step int, content string, probe bool) (turn, []exchange) {
	started := time.Now()
	t := turn{Step: step, Input: content}
	phase := fmt.Sprintf("live%d", step)
	if probe {
		phase = "probe"
	}
	inner, err := r.modelFor(episode, phase)
	if err != nil {
		t.Error = err.Error()
		return t, nil
	}
	rec := &recorder{inner: inner}
	events := &eventLog{}
	factory := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return rec }, Embedder: r.embedder, MaxHistoryMessages: r.corpus.Window, Tools: ws}
	rt, err := factory.New(ctx, "sherlock-study", events)
	if err != nil {
		t.Error = fmt.Sprintf("runtime: %v", err)
		return t, rec.take()
	}
	id := fmt.Sprintf("msg-%s-%d", episode, step)
	input := renderInput(episode, step, content)
	reply, err := rt.Session.Run(agent.WithInboundSources(ctx, []agent.SourceMessage{{ExternalID: id, Role: agent.RoleUser, Content: content}}), input)
	if err != nil {
		t.Error = err.Error()
	}
	t.Reply = reply
	if err := rt.Close(ctx); err != nil && t.Error == "" {
		t.Error = fmt.Sprintf("close: %v", err)
	}
	for _, e := range events.take() {
		switch e.Kind {
		case agent.EventToolCall:
			t.Tools = append(t.Tools, toolUse{Name: e.ToolName, Input: e.ToolInput, Result: e.ToolResult, Failed: e.ToolFailed})
		case agent.EventRecall:
			t.RecalledIDs = e.RecalledIDs
		}
	}
	t.Sent, t.Written = ws.take()
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
		if c.Kind == "understand" {
			u := &understanding{}
			for _, tc := range x.Response.ToolCalls {
				if tc.Name == "memory_understand" {
					u.Called, u.Answered = true, tc.Input
				}
			}
			t.Understood = u
		}
	}
	note, err := readNote(path)
	if err == nil {
		t.NoteAfter = note
	}
	t.Seconds = time.Since(started).Seconds()
	return t, exchanges
}

// readNote reads the saved note straight from the file, so the harness
// needs no store API the shipped structure lacks. A file without the note
// table, the shipped structure's, has no note.
func readNote(path string) (*string, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='conversation_note'`).Scan(&n); err != nil || n == 0 {
		return nil, err
	}
	var content string
	err = db.QueryRow(`SELECT content FROM conversation_note WHERE id=1`).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		content = ""
	} else if err != nil {
		return nil, err
	}
	return &content, nil
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
		if i == last || x.kind() == "understand" {
			entry["messages"] = x.Messages
		}
		out = append(out, entry)
	}
	return out
}
