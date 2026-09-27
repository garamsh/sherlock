package study

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// fakeProvider answers chat completions: a memory_search call first, then
// the terminal memory_write; it keeps the system messages it was shown.
func fakeProvider(t *testing.T, calls *atomic.Int32, shown *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		for _, m := range req.Messages {
			if m.Role == "system" {
				*shown = append(*shown, m.Content)
			}
		}
		call := `{"id":"c1","type":"function","function":{"name":"memory_search","arguments":"{\"query\":\"apple\"}"}}`
		if calls.Add(1) > 1 {
			call = `{"id":"c2","type":"function","function":{"name":"memory_write","arguments":"{\"reply\":\"the answer is 4471\",\"note\":\"\"}"}}`
		}
		_, _ = w.Write([]byte(`{"id":"x","model":"MiniMax-M2","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[` + call + `]}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
}

type builtArm struct{ ShippedArm }

func TestLiveOnce_RunsAProbeThroughTheProductionSessionWithTheArmsRecallAndSearch(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	build := filepath.Join(work, "builds", "M", "b4")
	require.NoError(t, os.MkdirAll(build, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(build, "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	for i := range 8 {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "apple fact " + string(rune('a'+i))}}, recordingSummary{}))
	}
	require.NoError(t, store.Close())
	var calls atomic.Int32
	var shown []string
	server := fakeProvider(t, &calls, &shown)
	defer server.Close()
	// The proxy route is /<arm>/<episode>/<phase>/v1/chat/completions.
	proxy := httptest.NewServer(http.StripPrefix("/M/C1-01/live", server.Config.Handler))
	defer proxy.Close()
	env := &Env{Corpus: &Corpus{Window: 32}, Embedder: wordEmbedder{}, Work: work, Proxy: proxy.URL, Arm: "M"}
	arm := builtArm{ShippedArm{ArmName: "M", Instruction: "FOLLOW THE CHAIN.", Batches: []string{"b4"}}}
	descriptors, err := loadDescriptors(filepath.Join("..", "..", "..", "..", "tools"))
	require.NoError(t, err)

	r := liveOnce(ctx, env, arm, "b4", LiveProbe{ID: "C1-01", Family: "C1", Query: "what about apple?"}, descriptors)

	require.Empty(t, r.Error)
	assert.Equal(t, "the answer is 4471", r.Reply)
	assert.Equal(t, 2, r.Calls)
	require.Len(t, r.Tools, 1)
	assert.Equal(t, "memory_search", r.Tools[0].Name)
	assert.Contains(t, r.Tools[0].Result, "apple fact", "the arm's search answered the call")
	assert.Contains(t, r.Recall, "## Recalled memories")
	assert.Contains(t, r.Recall, "sources:1-4", "the arm's recall reached the frame")
	assert.True(t, strings.Contains(strings.Join(shown, "\n"), "FOLLOW THE CHAIN."), "the arm's instruction reached the model")
	probeStore, err := sqlite.OpenConversation(filepath.Join(env.LiveDir("M", "b4", "C1-01"), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer probeStore.Close()
	recent, err := probeStore.Recent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, recent, 2, "the probe's turn was recorded in its own store copy")
	assert.Equal(t, "the answer is 4471", recent[1].Content)
}

// settingArm instructs by setting, as L and L+A do.
type settingArm struct{ ShippedArm }

func (settingArm) InstructionsFor(setting string) string {
	if setting == "b4-a" {
		return "ANSWER ONLY FROM A SOURCE."
	}
	return ""
}

func TestLiveOnce_AppendsTheSettingsInstruction(t *testing.T) {
	ctx := context.Background()
	for _, setting := range []string{"b4-a", "b4"} {
		work := t.TempDir()
		build := filepath.Join(work, "builds", "L", setting)
		require.NoError(t, os.MkdirAll(build, 0o755))
		store, err := sqlite.OpenConversation(filepath.Join(build, "memory.db"), wordEmbedder{})
		require.NoError(t, err)
		for i := range 4 {
			require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "apple fact " + string(rune('a'+i))}}, recordingSummary{}))
		}
		require.NoError(t, store.Close())
		var calls atomic.Int32
		var shown []string
		server := fakeProvider(t, &calls, &shown)
		proxy := httptest.NewServer(http.StripPrefix("/L/N-01/live", server.Config.Handler))
		env := &Env{Corpus: &Corpus{Window: 32}, Embedder: wordEmbedder{}, Work: work, Proxy: proxy.URL, Arm: "L"}
		arm := settingArm{ShippedArm{ArmName: "L", Instruction: "THE ARM-WIDE TEXT.", Batches: []string{"b4", "b4-a"}}}
		descriptors, err := loadDescriptors(filepath.Join("..", "..", "..", "..", "tools"))
		require.NoError(t, err)

		r := liveOnce(ctx, env, arm, setting, LiveProbe{ID: "N-01", Family: "N", Query: "what about apple?"}, descriptors)
		proxy.Close()
		server.Close()

		require.Empty(t, r.Error)
		all := strings.Join(shown, "\n")
		assert.Equal(t, setting == "b4-a", strings.Contains(all, "ANSWER ONLY FROM A SOURCE."), setting)
		assert.NotContains(t, all, "THE ARM-WIDE TEXT.", "the setting's instruction replaces the arm-wide one")
	}
}

func TestInfrastructure_SeparatesProviderFailuresFromTheArmsOwn(t *testing.T) {
	assert.True(t, infrastructure(LiveResult{CallErrors: []string{"chat: status 429: usage limit (2056)"}}))
	assert.True(t, infrastructure(LiveResult{Error: "Post \"http://127.0.0.1:8091\": connection refused"}))
	assert.False(t, infrastructure(LiveResult{Error: "model must call memory_write"}), "the control: a model failure is the arm's")
}

// associatingArm's memory answers memory_associate with one fixed mid, as
// L-T's does with the walk's.
type associatingArm struct{ ShippedArm }

type associatingMemory struct {
	Memory
	queries []string
}

func (m *associatingMemory) Associate(_ context.Context, query string, limit int) ([]Item, error) {
	m.queries = append(m.queries, query)
	return []Item{{ID: 9, Level: "mid", Kind: "linked", SourceFirst: 33, SourceLast: 36, Text: "Mateo Deatreal answers on extension 5504."}}[:min(limit, 1)], nil
}

func (a associatingArm) Open(ctx context.Context, env *Env, setting, dir string) (Memory, error) {
	m, err := a.ShippedArm.Open(ctx, env, setting, dir)
	if err != nil {
		return nil, err
	}
	return &associatingMemory{Memory: m}, nil
}

// associateProvider calls memory_associate first, then the terminal
// memory_write; it keeps the tool names it was offered.
func associateProvider(t *testing.T, calls *atomic.Int32, offered *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		*offered = (*offered)[:0]
		for _, tool := range req.Tools {
			*offered = append(*offered, tool.Function.Name)
		}
		call := `{"id":"c1","type":"function","function":{"name":"memory_associate","arguments":"{\"query\":\"Rodend\"}"}}`
		if calls.Add(1) > 1 {
			call = `{"id":"c2","type":"function","function":{"name":"memory_write","arguments":"{\"reply\":\"dial 5504\",\"note\":\"\"}"}}`
		}
		_, _ = w.Write([]byte(`{"id":"x","model":"MiniMax-M2","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[` + call + `]}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
}

func TestLiveOnce_OffersMemoryAssociateOnlyWhenTheMemoryAnswersIt(t *testing.T) {
	ctx := context.Background()
	for _, associates := range []bool{true, false} {
		work := t.TempDir()
		build := filepath.Join(work, "builds", "L", "b4")
		require.NoError(t, os.MkdirAll(build, 0o755))
		store, err := sqlite.OpenConversation(filepath.Join(build, "memory.db"), wordEmbedder{})
		require.NoError(t, err)
		for i := range 4 {
			require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "apple fact " + string(rune('a'+i))}}, recordingSummary{}))
		}
		require.NoError(t, store.Close())
		var calls atomic.Int32
		var offered []string
		server := associateProvider(t, &calls, &offered)
		proxy := httptest.NewServer(http.StripPrefix("/L/C2-01/live", server.Config.Handler))
		env := &Env{Corpus: &Corpus{Window: 32}, Embedder: wordEmbedder{}, Work: work, Proxy: proxy.URL, Arm: "L"}
		shipped := ShippedArm{ArmName: "L", Batches: []string{"b4"}}
		var arm Arm = shipped
		if associates {
			arm = associatingArm{shipped}
		}
		descriptors, err := loadDescriptors(filepath.Join("..", "..", "..", "..", "tools"))
		require.NoError(t, err)

		r := liveOnce(ctx, env, arm, "b4", LiveProbe{ID: "C2-01", Family: "C2", Query: "What number do I dial about Rodend?"}, descriptors)
		proxy.Close()
		server.Close()

		require.Empty(t, r.Error)
		require.Len(t, r.Tools, 1)
		assert.Equal(t, AssociateTool, r.Tools[0].Name)
		assert.Equal(t, associates, slices.Contains(offered, AssociateTool), "offered with an associating memory only")
		assert.Contains(t, offered, "memory_search", "the control: the session's own calls are offered either way")
		if associates {
			assert.False(t, r.Tools[0].Failed)
			assert.JSONEq(t, `[{"mid":9,"summary":"Mateo Deatreal answers on extension 5504.","source_first":33,"source_last":36}]`, r.Tools[0].Result)
		} else {
			assert.True(t, r.Tools[0].Failed, "the control: without an associating memory the call is a missing tool")
		}
	}
}

func TestWorkspace_RefusesAnEmptyAssociateQuery(t *testing.T) {
	ws := newWorkspace(nil)
	ws.associate = &associatingMemory{}
	_, err := ws.Call(context.Background(), AssociateTool, json.RawMessage(`{"query":"  "}`))
	assert.Error(t, err)
	out, err := ws.Call(context.Background(), AssociateTool, json.RawMessage(`{"query":"Rodend"}`))
	require.NoError(t, err, "the control: a query naming something is answered")
	assert.Contains(t, string(out), `"mid":9`)
}

// windowArm restores three messages and traces its recall.
type windowArm struct{ ShippedArm }

func (windowArm) WindowFor(string) int { return 3 }

func (a windowArm) Open(ctx context.Context, env *Env, setting, dir string) (Memory, error) {
	m, err := a.ShippedArm.Open(ctx, env, setting, dir)
	if err != nil {
		return nil, err
	}
	return tracingMemory{m.(ShippedMemory)}, nil
}

type tracingMemory struct{ ShippedMemory }

func (tracingMemory) LastTrace() map[string]any { return map[string]any{"loop_calls": 2} }

func TestLiveOnce_RestoresTheArmsWindowAndRecordsItsTrace(t *testing.T) {
	ctx := context.Background()
	for _, own := range []bool{true, false} {
		work := t.TempDir()
		build := filepath.Join(work, "builds", "W", "b4")
		require.NoError(t, os.MkdirAll(build, 0o755))
		store, err := sqlite.OpenConversation(filepath.Join(build, "memory.db"), wordEmbedder{})
		require.NoError(t, err)
		for i := range 8 {
			require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "apple fact " + string(rune('a'+i))}}, recordingSummary{}))
		}
		require.NoError(t, store.Close())
		var calls atomic.Int32
		var shown []string
		server := fakeProvider(t, &calls, &shown)
		proxy := httptest.NewServer(http.StripPrefix("/W/C2-01/live", server.Config.Handler))
		env := &Env{Corpus: &Corpus{Window: 32}, Embedder: wordEmbedder{}, Work: work, Proxy: proxy.URL, Arm: "W"}
		shipped := ShippedArm{ArmName: "W", Batches: []string{"b4"}}
		var arm Arm = shipped
		if own {
			arm = windowArm{shipped}
		}
		descriptors, err := loadDescriptors(filepath.Join("..", "..", "..", "..", "tools"))
		require.NoError(t, err)

		r := liveOnce(ctx, env, arm, "b4", LiveProbe{ID: "C2-01", Family: "C2", Query: "what about apple?"}, descriptors)
		proxy.Close()
		server.Close()

		require.Empty(t, r.Error)
		if own {
			assert.Equal(t, 3, r.WindowMessages)
			assert.Equal(t, int64(6), r.WindowFirst)
			assert.Equal(t, map[string]any{"loop_calls": 2}, r.Trace)
		} else {
			assert.Equal(t, 8, r.WindowMessages, "the control: the corpus's window restores every message of eight")
			assert.Equal(t, int64(1), r.WindowFirst)
			assert.Nil(t, r.Trace, "the control: a memory that traces nothing records nothing")
		}
	}
}
