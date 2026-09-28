package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

// wordEmbedder counts each word into one of 256 dimensions, so texts that
// share words are similar and texts that share none are not.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 256)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.Trim(w, ".,:;[]()")))
		v[h.Sum32()%256]++
	}
	return v, nil
}

// replay answers the production store's synthesis requests with the mid
// summaries in order.
type replay struct {
	summaries []string
	next      int
}

func (r *replay) Summarize(context.Context, []string) (agent.Synthesis, error) {
	r.next++
	return agent.Synthesis{Summary: r.summaries[r.next-1]}, nil
}

// proxyStub stands in for the study proxy: every chat request is counted
// and answered with a memory_summary call that compresses the request's
// updates to their "alpha" words, the one word each mid keeps.
func proxyStub(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		var input struct {
			Updates []string `json:"updates"`
		}
		require.NoError(t, json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input))
		var kept []string
		for _, u := range input.Updates {
			for _, w := range strings.Fields(u) {
				if strings.HasPrefix(w, "alpha") {
					kept = append(kept, w)
				}
			}
		}
		args, _ := json.Marshal(map[string]any{"summary": "group: " + strings.Join(kept, " "), "entities": []string{}})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message":       map[string]any{"tool_calls": []any{map[string]any{"id": "1", "type": "function", "function": map[string]any{"name": "memory_summary", "arguments": string(args)}}}},
			"finish_reason": "tool_calls",
		}}})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// midText is mid i's summary: two words only it holds, and one it shares
// with every mid.
func midText(i int) string { return fmt.Sprintf("alpha%d beta%d shared", i, i) }

// newEnv writes a batch-4 shared store of n mids, recorded through the
// production store four messages at a time, and returns an Env over it
// with the proxy stub. It checks the store holds what it asked for.
func newEnv(t *testing.T, n int) (*study.Env, *atomic.Int32) {
	ctx := context.Background()
	server, calls := proxyStub(t)
	env := &study.Env{Embedder: wordEmbedder{}, Dir: t.TempDir(), Work: t.TempDir(), Proxy: server.URL, Arm: "H"}
	dir := filepath.Join(env.Work, "shared", "b4")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	require.NoError(t, err)
	summaries := &replay{}
	for i := 1; i <= n; i++ {
		summaries.summaries = append(summaries.summaries, midText(i))
	}
	for m := 1; m <= 4*n; m++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("message %d", m)}}, summaries))
	}
	require.NoError(t, store.Close())
	mids, err := readMids(filepath.Join(dir, "memory.db"))
	require.NoError(t, err)
	require.Len(t, mids, n, "the shared store holds the mids the fixture asked for")
	require.Equal(t, midText(n), mids[n-1].Text)
	return env, calls
}

type storedGroup struct {
	first, last int64
	summary     string
	dims        int
}

func readGroups(t *testing.T, dir string) []storedGroup {
	db, err := sql.Open("sqlite", filepath.Join(dir, "memory.db")+"?mode=ro")
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.Query(`SELECT source_first, source_last, summary, length(embedding)/4 FROM level_group ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []storedGroup
	for rows.Next() {
		var g storedGroup
		require.NoError(t, rows.Scan(&g.first, &g.last, &g.summary, &g.dims))
		out = append(out, g)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestBuild_StoresOneEmbeddedGroupPerCompleteWindow(t *testing.T) {
	env, calls := newEnv(t, 10)
	dir := filepath.Join(env.Work, "builds", "H", "g4")
	built, err := arm{}.Build(context.Background(), env, "g4", dir)
	require.NoError(t, err)

	groups := readGroups(t, dir)
	require.Len(t, groups, 2, "10 mids hold two complete windows of 4; the last 2 mids are left ungrouped")
	assert.Equal(t, []int64{1, 16, 17, 32}, []int64{groups[0].first, groups[0].last, groups[1].first, groups[1].last}, "each group spans the union of its window's intervals")
	for w, g := range groups {
		assert.Equal(t, 256, g.dims, "group %d is embedded", w+1)
		for i := 4*w + 1; i <= 4*w+4; i++ {
			assert.Contains(t, strings.Fields(g.summary), fmt.Sprintf("alpha%d", i), "group %d was synthesized over mid %d", w+1, i)
		}
	}
	assert.Equal(t, int32(2), calls.Load(), "one synthesis call per window")
	assert.Equal(t, map[string]int{"mid": 10, "group": 2}, built.Units)
	assert.Equal(t, 12, built.Vectors)
	midChars := 0
	for i := 1; i <= 10; i++ {
		midChars += len(midText(i))
	}
	assert.Equal(t, midChars+len(groups[0].summary)+len(groups[1].summary), built.StoredChars)
}

func TestBuild_ReplaysRecordedGroupsWithoutACall(t *testing.T) {
	env, calls := newEnv(t, 8)
	_, err := arm{}.Build(context.Background(), env, "g4", filepath.Join(env.Work, "first"))
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load(), "the first build synthesized both windows")

	built, err := arm{}.Build(context.Background(), env, "g4", filepath.Join(env.Work, "second"))
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "the second build made no call")
	assert.Equal(t, 0, built.Notes["synthesis_calls_this_build"])
	assert.Equal(t, readGroups(t, filepath.Join(env.Work, "first")), readGroups(t, filepath.Join(env.Work, "second")))
}

func TestGroupSize_AcceptsOnlyGAndANumberOfAtLeastTwo(t *testing.T) {
	for setting, want := range map[string]int{"g4": 4, "g8": 8} {
		g, err := groupSize(setting)
		require.NoError(t, err, setting)
		assert.Equal(t, want, g)
	}
	for _, setting := range []string{"g1", "b4", "g4x", "g"} {
		_, err := groupSize(setting)
		assert.Error(t, err, setting)
	}
}
