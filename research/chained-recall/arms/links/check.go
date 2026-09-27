package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// checkPrompt is L+chk's grounding check (#896): the one narrow call the
// runtime makes when recall or memory_search delivered walk mids.
const checkPrompt = "You check memory items before an answer is written. You get a question and numbered memory items. Name, in order, at most 3 items that together state the answer: the first states something about the entity the question asks about, each next item continues from the one before it, and the last states the attribute the question asks for. Name an item only for what its own text states. If the items do not state the asked attribute of the asked entity, name none. Call memory_grounding once."

// checkTool is the check's reserved schema: an ordered list of at most
// checkItems item IDs, empty for none.
const checkTool = "memory_grounding"

const checkItems = 3

var checkSchema = json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"integer"},"maxItems":%d}},"required":["items"],"additionalProperties":false}`, checkItems))

// noEvidence is what the turn is told when the check keeps no item.
const noEvidence = "No stored memory states this fact."

// checkAttempts bounds how often a malformed answer is asked again; after
// the last, the check keeps nothing (it fails closed).
const checkAttempts = 3

// checked is L+chk: L, with every recall block and memory_search result
// that holds a walk mid passed through the grounding check. Only the items
// the check names, and only when they connect the query's entity through
// stored entities, go on as evidence; otherwise the notice alone does.
type checked struct {
	*memory
	model agent.Model
	proxy string
	// probe is the live probe the store copy was made for, "" offline.
	probe  string
	opened string
	// log receives one record per checked call in the live stage; offline
	// the record is in the trace.
	log io.Writer
}

// checkLogs serialises the live workers' appends to one checks.jsonl.
var checkLogs sync.Mutex

// openChecked gives L's memory the check. Its model is the session model
// through the study proxy, under phase "check" and, live, the probe's
// episode, so the ledger counts the check's calls and tokens per turn.
func openChecked(env *study.Env, m *memory, setting, dir string) (study.Memory, error) {
	c := &checked{memory: m, proxy: env.Proxy, opened: time.Now().UTC().Format(time.RFC3339Nano)}
	episode := setting
	if filepath.Clean(dir) != filepath.Clean(env.BuildDir("L", setting)) {
		c.probe = filepath.Base(dir)
		episode = c.probe
		out := filepath.Join(env.Dir, "evidence", "live", "L", setting)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(filepath.Join(out, "checks.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		c.log = f
	}
	model, err := env.Model(episode, "check")
	if err != nil {
		return nil, err
	}
	c.model = model
	return c, nil
}

func (c *checked) Recall(ctx context.Context, task string) ([]study.Item, error) {
	items, err := c.memory.Recall(ctx, task)
	if err != nil {
		return nil, err
	}
	return c.ground(ctx, "recall", task, items)
}

func (c *checked) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	items, err := c.memory.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return c.ground(ctx, "search", query, items)
}

// checkRecord is what one checked call leaves for L+chk's control.
type checkRecord struct {
	Probe     string  `json:"probe,omitempty"`
	Opened    string  `json:"opened"`
	Call      string  `json:"call"`
	Question  string  `json:"question"`
	Delivered []int64 `json:"delivered"`
	Walk      []int64 `json:"walk"`
	Ran       bool    `json:"ran"`
	Attempts  int     `json:"attempts,omitempty"`
	Malformed bool    `json:"malformed,omitempty"`
	Named     []int64 `json:"named"`
	Connected bool    `json:"connected"`
	Kept      []int64 `json:"kept"`
}

// ground runs the check when the items hold a walk mid and returns what
// goes on as evidence: the named items in the check's order when they
// connect, else the notice. Items without a walk mid pass unchanged.
func (c *checked) ground(ctx context.Context, call, question string, items []study.Item) ([]study.Item, error) {
	rec := checkRecord{Probe: c.probe, Opened: c.opened, Call: call, Question: question, Delivered: midIDs(items), Walk: []int64{}, Named: []int64{}, Kept: []int64{}}
	for _, it := range items {
		if it.Kind == "linked" {
			rec.Walk = append(rec.Walk, it.ID)
		}
	}
	out := items
	if len(rec.Walk) > 0 {
		named, attempts, malformed, err := c.ask(ctx, question, items)
		if err != nil {
			return nil, err
		}
		seeds, _ := c.trace["seeds"].([]string)
		rec.Ran, rec.Attempts, rec.Malformed, rec.Named = true, attempts, malformed, named
		rec.Connected = c.graph.connects(named, seeds)
		out = []study.Item{{Level: "notice", Kind: "check", Text: noEvidence}}
		if rec.Connected {
			byID := map[int64]study.Item{}
			for _, it := range items {
				byID[it.ID] = it
			}
			out = out[:0]
			for _, id := range named {
				out = append(out, byID[id])
			}
		}
		rec.Kept = midIDs(out)
	}
	c.trace["check"] = rec
	return out, c.write(rec)
}

func (c *checked) write(rec checkRecord) error {
	if c.log == nil {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	checkLogs.Lock()
	defer checkLogs.Unlock()
	_, err = c.log.Write(append(line, '\n'))
	return err
}

func (c *checked) Close() error {
	if f, ok := c.log.(io.Closer); ok {
		_ = f.Close()
	}
	return c.memory.Close()
}

// midIDs lists the mid items' IDs; the notice has none.
func midIDs(items []study.Item) []int64 {
	out := []int64{}
	for _, it := range items {
		if it.Level == "mid" {
			out = append(out, it.ID)
		}
	}
	return out
}

// ask sends the check and returns the IDs it named. A transport or
// provider error is returned, so the live runner re-runs the probe; an
// answer that is not one well-formed call naming delivered items is asked
// again, and after checkAttempts counts as naming none.
func (c *checked) ask(ctx context.Context, question string, items []study.Item) ([]int64, int, bool, error) {
	type shown struct {
		ID   int64  `json:"id"`
		Text string `json:"text"`
	}
	var list []shown
	for _, it := range items {
		if it.Level == "mid" {
			list = append(list, shown{it.ID, it.Text})
		}
	}
	input, err := json.Marshal(struct {
		Question string  `json:"question"`
		Items    []shown `json:"items"`
	}{question, list})
	if err != nil {
		return nil, 0, false, err
	}
	messages := []agent.Message{{Role: agent.RoleSystem, Content: checkPrompt}, {Role: agent.RoleUser, Content: string(input)}}
	descriptor := []tool.Descriptor{{Name: checkTool, Description: "Name the memory items that state the answer", InputSchema: checkSchema}}
	for attempt := 1; attempt <= checkAttempts; attempt++ {
		study.WaitWhilePaused(ctx, c.proxy)
		response, err := c.model.Chat(ctx, messages, descriptor)
		if err != nil {
			return nil, attempt, false, fmt.Errorf("grounding check: %w", err)
		}
		named, err := parseCheck(response, midIDs(items))
		if err == nil {
			return named, attempt, false, nil
		}
		messages = append(messages[:2:2], agent.Message{Role: agent.RoleUser, Content: fmt.Sprintf("Attempt %d was not usable (%v). Call memory_grounding once, naming only IDs from the list.", attempt, err)})
	}
	return []int64{}, checkAttempts, true, nil
}

// parseCheck reads the check's answer: exactly one memory_grounding call
// naming at most checkItems distinct delivered IDs.
func parseCheck(response agent.Response, delivered []int64) ([]int64, error) {
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != checkTool {
		return nil, errors.New("the check must call memory_grounding exactly once")
	}
	var result struct {
		Items []int64 `json:"items"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(response.ToolCalls[0].Input)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("parse memory_grounding: %v", err)
	}
	if result.Items == nil {
		return nil, errors.New("memory_grounding must list its items")
	}
	if len(result.Items) > checkItems {
		return nil, fmt.Errorf("memory_grounding names %d items, at most %d", len(result.Items), checkItems)
	}
	seen := map[int64]bool{}
	for _, id := range result.Items {
		if !slices.Contains(delivered, id) || seen[id] {
			return nil, fmt.Errorf("item %d was not delivered or is named twice", id)
		}
		seen[id] = true
	}
	return result.Items, nil
}

// connects is the runtime's structural guard on a named set: the first
// item names one of the query's entities, and each next item shares a
// stored entity with the one before it. An empty set connects nothing.
func (g *graph) connects(named []int64, seeds []string) bool {
	if len(named) == 0 || !g.namesAny(named[0], seeds) {
		return false
	}
	for i := 1; i < len(named); i++ {
		if !g.namesAny(named[i], g.entities(named[i-1])) {
			return false
		}
	}
	return true
}
