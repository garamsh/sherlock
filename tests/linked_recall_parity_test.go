package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

// The study this test holds production to: #916's run of L-attr, the
// links arm's b4-model-attr setting, and the shipped recall S beside it on
// the same store.
const (
	attributedStudy = "research/memory-encoding/attributed"
	baseSyntheses   = "research/mid-memory-rag/evidence/replication/synthesis-n4.jsonl"
)

// studyItem and studyRecord are the fields of a study.Offline record this
// test reads.
type studyItem struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	First int64  `json:"first"`
	Last  int64  `json:"last"`
	Text  string `json:"text"`
}

type studyRecord struct {
	Probe string      `json:"probe"`
	Hop   int         `json:"hop"`
	Items []studyItem `json:"items"`
}

func (r studyRecord) key() string { return fmt.Sprintf("%s/%d", r.Probe, r.Hop) }

func readJSONL[T any](t *testing.T, path string) []T {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var v T
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &v), path)
		out = append(out, v)
	}
	require.NoError(t, scanner.Err())
	return out
}

// recordedSyntheses answers the store's synthesis requests, in order, with
// #916's recorded batch-4 summaries and the entities its extended call
// wrote for the same batch.
type recordedSyntheses struct {
	syntheses []agent.Synthesis
	next      int
}

func (r *recordedSyntheses) Summarize(_ context.Context, updates []string) (agent.Synthesis, error) {
	if len(updates) != 4 || r.next == len(r.syntheses) {
		return agent.Synthesis{}, fmt.Errorf("batch %d: %d messages, %d recordings", r.next+1, len(updates), len(r.syntheses))
	}
	r.next++
	return r.syntheses[r.next-1], nil
}

func loadSyntheses(t *testing.T, root string) []agent.Synthesis {
	t.Helper()
	type base struct {
		Batch   int    `json:"batch"`
		Summary string `json:"summary"`
	}
	type stream struct {
		First   int64  `json:"first"`
		Summary string `json:"summary"`
	}
	type named struct {
		First    int64    `json:"first"`
		Entities []string `json:"entities"`
	}
	summaries := map[int64]string{}
	for _, b := range readJSONL[base](t, filepath.Join(root, baseSyntheses)) {
		summaries[int64(b.Batch-1)*4+1] = b.Summary
	}
	for _, s := range readJSONL[stream](t, filepath.Join(root, attributedStudy, "evidence/synthesis/stream-b4.jsonl")) {
		summaries[s.First] = s.Summary
	}
	entities := map[int64][]string{}
	for _, n := range readJSONL[named](t, filepath.Join(root, attributedStudy, "arms/links/evidence/entities-b4.jsonl")) {
		entities[n.First] = n.Entities
	}
	out := make([]agent.Synthesis, len(summaries))
	for i := range out {
		first := int64(4*i + 1)
		summary, ok := summaries[first]
		require.True(t, ok, "no recorded summary for the batch at %d", first)
		out[i] = agent.Synthesis{Summary: summary, Entities: entities[first]}
	}
	require.Len(t, entities, len(out), "the setup: every batch has its entity record")
	return out
}

// rankedEmbedder reproduces the similarity rankings S recorded on the same
// store. Query i embeds as axis i; a mid embeds with 6-r on axis i when S
// ranked it r-th (from 0) among the non-recent mids for query i, and a
// last axis pads every mid to the same length. Cosine then orders S's
// ranked mids as S did, and every other mid below them at zero. The store
// embeds each mid once, right after its checkpoint, so any text that is no
// query is the next mid.
type rankedEmbedder struct {
	queries  map[string]int
	mids     [][]float32
	embedded int
}

func (e *rankedEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if i, ok := e.queries[text]; ok {
		v := make([]float32, len(e.queries)+1)
		v[i] = 1
		return v, nil
	}
	if e.embedded == len(e.mids) {
		return nil, fmt.Errorf("no recorded ranking for %q", text)
	}
	e.embedded++
	return e.mids[e.embedded-1], nil
}

func newRankedEmbedder(mids int, rankings map[string][]int64) *rankedEmbedder {
	e := &rankedEmbedder{queries: map[string]int{}, mids: make([][]float32, mids)}
	axis := map[int64]map[int]float32{}
	for query, ranked := range rankings {
		i := len(e.queries)
		e.queries[query] = i
		for r, id := range ranked {
			if axis[id] == nil {
				axis[id] = map[int]float32{}
			}
			axis[id][i] = float32(6 - r)
		}
	}
	length := 0.0
	for _, values := range axis {
		sum := 0.0
		for _, x := range values {
			sum += float64(x) * float64(x)
		}
		length = math.Max(length, math.Sqrt(sum))
	}
	length++
	for i := range e.mids {
		v := make([]float32, len(e.queries)+1)
		sum := 0.0
		for q, x := range axis[int64(i+1)] {
			v[q] = x
			sum += float64(x) * float64(x)
		}
		v[len(e.queries)] = float32(math.Sqrt(length*length - sum))
		e.mids[i] = v
	}
	return e
}

// readOnly keeps a session from writing its own turn into the store under
// test.
type readOnly struct{ agent.ConversationMemory }

func (readOnly) RecordTurn(context.Context, []agent.SourceMessage, agent.ConversationSummarizer) error {
	return nil
}

// searchOnce calls memory_search once with its query and keeps the result
// the session returns to it.
type searchOnce struct {
	query  string
	result string
}

func (m *searchOnce) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	if last := messages[len(messages)-1]; last.Role == agent.RoleTool {
		m.result = last.Content
		return agent.Response{ToolCalls: []agent.ToolCall{{ID: "write", Name: agent.MemoryWriteTool, Input: json.RawMessage(`{"reply":"done","note":null}`)}}}, nil
	}
	input, err := json.Marshal(map[string]string{"query": m.query})
	return agent.Response{ToolCalls: []agent.ToolCall{{ID: "search", Name: agent.MemorySearchTool, Input: input}}}, err
}

type noTools struct{}

func (noTools) List() []tool.Descriptor { return nil }
func (noTools) Call(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("no tools")
}

// TestLinkedRecall_MatchesTheStudiedSettingOnItsRecordedProbes builds #916's
// store through the production write path from its recorded syntheses and
// entities, then runs memory_search on all 505 hop statements, its 470
// probes checking the setup only. Each search must deliver the items
// b4-model-attr recorded offline, in its order, with its kinds and its
// attribution openings. No model is called.
func TestLinkedRecall_MatchesTheStudiedSettingOnItsRecordedProbes(t *testing.T) {
	ctx := context.Background()
	root := moduleRoot(t)
	offline := filepath.Join(root, attributedStudy, "evidence/offline")
	var corpus struct {
		Chains []struct {
			ID    string `json:"id"`
			Query string `json:"query"`
			Hops  []struct {
				Statement string `json:"statement"`
			} `json:"hops"`
		} `json:"chains"`
		Guards []struct {
			ID    string `json:"id"`
			Query string `json:"query"`
		} `json:"guards"`
	}
	data, err := os.ReadFile(filepath.Join(root, attributedStudy, "corpus.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &corpus))
	queries := map[string]string{}
	for _, c := range corpus.Chains {
		queries[c.ID+"/0"] = c.Query
		for i, h := range c.Hops {
			queries[fmt.Sprintf("%s/%d", c.ID, i+1)] = h.Statement
		}
	}
	for _, g := range corpus.Guards {
		queries[g.ID+"/0"] = g.Query
	}

	shipped := append(readJSONL[studyRecord](t, filepath.Join(offline, "S/b4/probes.jsonl")), readJSONL[studyRecord](t, filepath.Join(offline, "S/b4/oracle.jsonl"))...)
	rankings := map[string][]int64{}
	for _, r := range shipped {
		query, ok := queries[r.key()]
		require.True(t, ok, r.key())
		for _, it := range r.Items {
			if it.Kind == "historical" {
				rankings[query] = append(rankings[query], it.ID)
			}
		}
	}
	syntheses := loadSyntheses(t, root)
	embedder := newRankedEmbedder(len(syntheses), rankings)
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), embedder)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	replay := &recordedSyntheses{syntheses: syntheses}
	for i := range syntheses {
		batch := make([]agent.SourceMessage, 4)
		for j := range batch {
			batch[j] = agent.SourceMessage{Role: agent.RoleUser, Content: fmt.Sprintf("source %d", 4*i+j+1)}
		}
		require.NoError(t, store.RecordTurn(ctx, batch, replay))
	}
	require.Equal(t, len(syntheses), replay.next, "the setup: every recorded batch became a mid")
	require.Equal(t, len(syntheses), embedder.embedded, "the setup: every mid was embedded once")

	probes := readJSONL[studyRecord](t, filepath.Join(offline, "L/b4-model-attr/probes.jsonl"))
	hops := readJSONL[studyRecord](t, filepath.Join(offline, "L/b4-model-attr/oracle.jsonl"))
	require.Len(t, probes, 470)
	require.Len(t, hops, 505)
	// The records hold each case the port must reproduce: walk items shown
	// with an attribution opening, and walk items about a queried entity
	// shown plain.
	walked := func(records []studyRecord) (attributed, plain int) {
		for _, r := range records {
			for _, it := range r.Items {
				if it.Kind == "linked" && strings.HasPrefix(it.Text, "About ") {
					attributed++
				} else if it.Kind == "linked" {
					plain++
				}
			}
		}
		return attributed, plain
	}
	attributed, plain := walked(probes)
	require.Equal(t, []int{440, 208}, []int{attributed, plain}, "the setup: recall's walk items")
	attributed, plain = walked(hops)
	require.Equal(t, []int{555, 284}, []int{attributed, plain}, "the setup: memory_search's walk items")

	t.Run("memory_search", func(t *testing.T) {
		for _, want := range hops {
			t.Run(want.key(), func(t *testing.T) {
				t.Parallel()
				model := &searchOnce{query: queries[want.key()]}
				_, err := agent.NewSession(model, noTools{}, agent.Options{Conversation: readOnly{store}}).Run(ctx, "search")
				require.NoError(t, err)
				var got []struct {
					ID      int64  `json:"mid"`
					Summary string `json:"summary"`
					First   int64  `json:"source_first"`
					Last    int64  `json:"source_last"`
				}
				require.NoError(t, json.Unmarshal([]byte(model.result), &got))
				require.Len(t, got, len(want.Items))
				for i, it := range want.Items {
					assert.Equal(t, []any{it.ID, strings.TrimSpace(it.Text), it.First, it.Last}, []any{got[i].ID, got[i].Summary, got[i].First, got[i].Last}, "item %d", i)
				}
			})
		}
	})
}
