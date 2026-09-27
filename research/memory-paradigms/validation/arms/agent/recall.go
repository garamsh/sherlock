package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

const (
	// maxLoopCalls bounds the recall step, as the session's per-tool budget
	// bounds a turn's searches (maxToolCallsPerTurn, internal/agent/service.go).
	maxLoopCalls = 6
	// windowTail is how many of the newest window messages the step reads.
	windowTail = 4
	// searchLimit is production's results per memory_search.
	searchLimit  = 5
	maxFacts     = 8
	maxStatement = 300
	resultTool   = "recall_result"
)

const recallInstruction = "You are the recall step of an assistant. Before the assistant answers the task below, find what the user's stored conversation says that the task needs. memory_search finds mid episodes: summaries of earlier conversation with the source IDs they cover. memory_inspect reads one exact source message by ID. A summary is only a lead: read the source messages that state a fact before you report it. When a message you read names a person, place, organization or thing the task depends on, search for that name too, and keep following until you reach what the task needs. Report by calling recall_result: each fact in one sentence, with the IDs of the source messages you read that state it. Report only what those messages state. If nothing stored answers the task, call recall_result with an empty list. You may make at most 6 calls in all."

const resultSchema = `{"type":"object","properties":{"facts":{"type":"array","maxItems":8,"items":{"type":"object","properties":{"statement":{"type":"string","minLength":1,"maxLength":300},"sources":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"integer","minimum":1}}},"required":["statement","sources"],"additionalProperties":false}}},"required":["facts"],"additionalProperties":false}`

func resultDescriptor() tool.Descriptor {
	return tool.Descriptor{Name: resultTool, Description: "Report the facts found for the task, each with the IDs of the source messages you read with memory_inspect that state it; an empty list when nothing stored answers the task. Ends the recall step.", InputSchema: json.RawMessage(resultSchema)}
}

// Fact is one fact the recall step reported.
type Fact struct {
	Statement string  `json:"statement"`
	Sources   []int64 `json:"sources"`
}

// memory is R's memory: the shipped search for the acting model, and the
// recall step in place of the shipped recall.
type memory struct {
	study.ShippedMemory
	newModel func(episode string) (agent.Model, error)
	// probes maps a live probe's query to its ID, which names the step's
	// calls in the ledger.
	probes map[string]string
	trace  map[string]any
}

func (m *memory) LastTrace() map[string]any { return m.trace }

// Recall runs the recall step and delivers the facts whose every cited
// source the step read, or one line saying it found nothing.
func (m *memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	probe, ok := m.probeOf(task)
	if !ok {
		return nil, fmt.Errorf("no live probe asks %q", task)
	}
	model, err := m.newModel(probe)
	if err != nil {
		return nil, err
	}
	facts, inspected, trace, err := m.loop(ctx, model, task)
	if err != nil {
		return nil, err
	}
	kept, dropped := CheckCitations(facts, inspected)
	trace["returned"] = len(facts)
	trace["kept"] = kept
	trace["dropped"] = dropped
	m.trace = trace
	return Items(kept), nil
}

// probeOf names the probe a task belongs to. The live session hands recall
// the task as "[msg-<probe>] owner: <query>" (study.liveOnce); the bare
// query is accepted too (Amendment 5).
func (m *memory) probeOf(task string) (string, bool) {
	if rest, ok := strings.CutPrefix(task, "[msg-"); ok {
		if id, query, ok := strings.Cut(rest, "] owner: "); ok && m.probes[query] == id {
			return id, true
		}
	}
	id, ok := m.probes[task]
	return id, ok
}

// CheckCitations keeps a fact only when it cites at least one source and
// the step read every source it cites.
func CheckCitations(facts []Fact, inspected map[int64]bool) (kept, dropped []Fact) {
	kept, dropped = []Fact{}, []Fact{}
	for _, f := range facts {
		ok := len(f.Sources) > 0
		for _, s := range f.Sources {
			ok = ok && inspected[s]
		}
		if ok {
			kept = append(kept, f)
		} else {
			dropped = append(dropped, f)
		}
	}
	return kept, dropped
}

// Items renders kept facts for the recall block, each labelled with the
// sources it was read from; with none, one line saying so.
func Items(kept []Fact) []study.Item {
	if len(kept) == 0 {
		return []study.Item{{Level: "recall", Kind: "none", Text: "The recall step found no stored evidence for this task.", Label: "[recall step]"}}
	}
	out := make([]study.Item, 0, len(kept))
	for i, f := range kept {
		ids := make([]string, len(f.Sources))
		for j, s := range f.Sources {
			ids[j] = strconv.FormatInt(s, 10)
		}
		out = append(out, study.Item{
			ID: int64(i + 1), Level: "fact", Kind: "verified",
			SourceFirst: slices.Min(f.Sources), SourceLast: slices.Max(f.Sources),
			Text:  f.Statement,
			Label: fmt.Sprintf("[fact:%d from sources %s, each read by the recall step]", i+1, strings.Join(ids, ", ")),
		})
	}
	return out
}

// loop runs the recall step: at most maxLoopCalls model calls, the last
// offering only recall_result. It returns the facts reported, the sources
// memory_inspect read, and what the step did.
func (m *memory) loop(ctx context.Context, model agent.Model, task string) ([]Fact, map[int64]bool, map[string]any, error) {
	recent, err := m.Conversation.Recent(ctx, windowTail)
	if err != nil {
		return nil, nil, nil, err
	}
	var lines []string
	for _, r := range recent {
		lines = append(lines, string(r.Role)+": "+r.Content)
	}
	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: recallInstruction},
		{Role: agent.RoleUser, Content: "Task: " + task + "\n\nThe latest messages of the conversation, oldest first:\n" + strings.Join(lines, "\n")},
	}
	all := []tool.Descriptor{agent.MemorySearchDescriptor(), agent.MemoryInspectDescriptor(), resultDescriptor()}
	inspected := map[int64]bool{}
	var facts []Fact
	calls, searches, parseErrors, ended := 0, 0, 0, "exhausted"
	for calls < maxLoopCalls && ended != "result" {
		calls++
		tools := all
		if calls == maxLoopCalls {
			tools = []tool.Descriptor{resultDescriptor()}
		}
		resp, err := model.Chat(ctx, messages, tools)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("recall step: %w", err)
		}
		messages = append(messages, agent.Message{Role: agent.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})
		if len(resp.ToolCalls) == 0 {
			messages = append(messages, agent.Message{Role: agent.RoleUser, Content: "Call memory_search, memory_inspect or recall_result."})
			continue
		}
		for _, call := range resp.ToolCalls {
			var result string
			switch call.Name {
			case resultTool:
				parsed, err := parseResult(call.Input)
				if err != nil {
					parseErrors++
					result = err.Error()
					break
				}
				facts, ended = parsed, "result"
				result = "recorded"
			case agent.MemorySearchTool:
				searches++
				result = m.search(ctx, call.Input)
			case agent.MemoryInspectTool:
				var id int64
				result, id = m.inspect(ctx, call.Input)
				if id > 0 {
					inspected[id] = true
				}
			default:
				result = fmt.Sprintf("tool %q not found", call.Name)
			}
			messages = append(messages, agent.Message{Role: agent.RoleTool, Name: call.Name, ToolCallID: call.ID, Content: result})
		}
	}
	read := make([]int64, 0, len(inspected))
	for id := range inspected {
		read = append(read, id)
	}
	slices.Sort(read)
	return facts, inspected, map[string]any{"loop_calls": calls, "ended": ended, "searches": searches, "inspected": read, "parse_errors": parseErrors}, nil
}

// search answers memory_search as production does (internal/agent/search.go).
func (m *memory) search(ctx context.Context, input json.RawMessage) string {
	var request struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &request); err != nil || strings.TrimSpace(request.Query) == "" {
		return "query must not be empty"
	}
	items, err := m.Search(ctx, strings.TrimSpace(request.Query), searchLimit)
	if err != nil {
		return err.Error()
	}
	type hit struct {
		ID          int64  `json:"mid"`
		Summary     string `json:"summary"`
		SourceFirst int64  `json:"source_first"`
		SourceLast  int64  `json:"source_last"`
	}
	out := make([]hit, 0, len(items))
	for _, it := range items {
		out = append(out, hit{ID: it.ID, Summary: strings.TrimSpace(it.Text), SourceFirst: it.SourceFirst, SourceLast: it.SourceLast})
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// inspect answers memory_inspect as production does (internal/agent/inspect.go),
// and returns the ID it read, 0 when it read none.
func (m *memory) inspect(ctx context.Context, input json.RawMessage) (string, int64) {
	var request struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &request); err != nil || request.ID <= 0 {
		return "memory id must be positive", 0
	}
	source, err := m.Conversation.Source(ctx, request.ID)
	if err != nil {
		return err.Error(), 0
	}
	data, _ := json.Marshal(source)
	return string(data), request.ID
}

// parseResult checks a recall_result call against its schema's bounds.
func parseResult(input json.RawMessage) ([]Fact, error) {
	var result struct {
		Facts *[]Fact `json:"facts"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(input)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("parse recall_result: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("recall_result must contain one JSON object")
	}
	if result.Facts == nil {
		return nil, errors.New("recall_result must list its facts")
	}
	if len(*result.Facts) > maxFacts {
		return nil, fmt.Errorf("recall_result lists %d facts; at most %d", len(*result.Facts), maxFacts)
	}
	for _, f := range *result.Facts {
		if n := len([]rune(strings.TrimSpace(f.Statement))); n == 0 || n > maxStatement {
			return nil, fmt.Errorf("a fact must hold 1 to %d characters", maxStatement)
		}
	}
	return *result.Facts, nil
}
