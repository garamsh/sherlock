package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// wordEmbedder places a text by the words it holds, so texts that share
// names are near and the ranking can be read off the corpus.
type wordEmbedder struct{}

var word = regexp.MustCompile(`[a-z0-9]+`)

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 64)
	for _, w := range word.FindAllString(strings.ToLower(text), -1) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%64]++
	}
	return v, nil
}

// proxy answers each chat request as the model would, from what it was
// sent: RW's summary names the batch's first message and the memories it
// was given; RW-key names Chloe Traike when it can, and every mid it was
// sent. It records every request.
type proxy struct {
	mu     sync.Mutex
	routes []string
	inputs []string
}

func (p *proxy) server(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/budget" {
			fmt.Fprint(w, `{"paused":false}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		var in struct {
			RetrievedMemories []memoryInput `json:"retrieved_memories"`
			Updates           []string      `json:"updates"`
		}
		require.NoError(t, json.Unmarshal([]byte(req.Messages[1].Content), &in))
		p.mu.Lock()
		p.routes = append(p.routes, r.URL.Path)
		p.inputs = append(p.inputs, req.Messages[1].Content)
		p.mu.Unlock()
		var name, args string
		switch req.Tools[0].Function.Name {
		case "memory_summary":
			var given []string
			for _, m := range in.RetrievedMemories {
				given = append(given, m.Summary)
			}
			out, _ := json.Marshal(map[string]string{"summary": "RW: " + in.Updates[0] + " | given: " + strings.Join(given, " / ")})
			name, args = "memory_summary", string(out)
		case "memory_related":
			entities := []string{}
			if strings.Contains(req.Messages[1].Content, "Chloe Traike") {
				entities = append(entities, "Chloe Traike")
			}
			mids := []int64{}
			for _, m := range in.RetrievedMemories {
				mids = append(mids, m.Mid)
			}
			out, _ := json.Marshal(map[string]any{"entities": entities, "mids": mids})
			name, args = "memory_related", string(out)
		}
		quoted, _ := json.Marshal(args)
		fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":%q,"arguments":%s}}]}}]}`, name, quoted)
	}))
	t.Cleanup(s.Close)
	return s
}

func (p *proxy) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.routes)
}

// corpus is two base batches and four stream batches. The first base
// message and the first and third stream batches name Chloe Traike.
func corpus() *study.Corpus {
	contents := []string{
		"Our account at Zevai Components is looked after by Chloe Traike.", "Noted.", "Base weather talk.", "Fine.",
		"Base bread recipe.", "Nice.", "Base studio lamp.", "Ok.",
		"Chloe Traike answers on extension 6200.", "Got it.", "That is useful.", "Yes.",
		"The harbour ferry leaves at noon.", "Noted.", "Harbour tickets.", "Ok.",
		"Chloe Traike moved to the third floor.", "Got it.", "Third floor desk.", "Yes.",
		"Painting the studio wall.", "Nice.", "Wall colour.", "Ok.",
	}
	var all []study.Message
	for i, c := range contents {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		all = append(all, study.Message{ID: int64(i + 1), Role: role, Content: c})
	}
	return &study.Corpus{Base: all[:8], Stream: all[8:]}
}

// sSummarizer writes S's summary: the batch's first message, marked.
type sSummarizer struct{}

func (sSummarizer) Summarize(_ context.Context, updates []string) (string, error) {
	return "S: " + updates[0], nil
}

// env builds the shared store the way harness build does, over recorded
// S summaries, and returns an environment on it with the proxy.
func env(t *testing.T, p *proxy) *study.Env {
	t.Helper()
	ctx := context.Background()
	e := &study.Env{Corpus: corpus(), Embedder: wordEmbedder{}, Dir: t.TempDir(), Work: t.TempDir(), Arm: "RW"}
	if p != nil {
		e.Proxy = p.server(t).URL
	}
	shared := filepath.Join(e.Work, "shared", "b4")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(shared, "memory.db"), e.Embedder)
	require.NoError(t, err)
	for _, m := range e.Corpus.Messages() {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.Role(m.Role), Content: m.Content}}, sSummarizer{}))
	}
	require.NoError(t, store.Close())
	return e
}

func recording(t *testing.T, e *study.Env, setting string) []record {
	t.Helper()
	rows, err := study.ReadJSONL[record](recordingPath(e, setting))
	require.NoError(t, err)
	return rows
}

// row is one stored mid.
type row struct {
	ID        int64
	First     int64
	Summary   string
	Related   string
	Embedding []byte
}

func rows(t *testing.T, path string) []row {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?mode=ro")
	require.NoError(t, err)
	defer db.Close()
	related := "''"
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_mid') WHERE name='related'`).Scan(&n))
	if n == 1 {
		related = "coalesce(related,'')"
	}
	rs, err := db.Query(`SELECT id, source_first, summary, ` + related + `, embedding FROM conversation_mid ORDER BY id`)
	require.NoError(t, err)
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		require.NoError(t, rs.Scan(&r.ID, &r.First, &r.Summary, &r.Related, &r.Embedding))
		out = append(out, r)
	}
	return out
}

func embedded(t *testing.T, text string) []byte {
	v, err := wordEmbedder{}.Embed(context.Background(), text)
	require.NoError(t, err)
	return encodeVector(v)
}

func TestBuildRW_WritesInOrderAndSearchesItsOwnEarlierMids(t *testing.T) {
	p := &proxy{}
	e := env(t, p)
	dir := filepath.Join(e.Work, "builds", "RW", settingRW)
	built, err := arm{}.Build(context.Background(), e, settingRW, dir)
	require.NoError(t, err)
	assert.Equal(t, 4, p.calls(), "one call per stream batch; the base is never synthesised")
	assert.Equal(t, "/RW/b4-00003/rw-b4/v1/chat/completions", p.routes[0])

	recs := recording(t, e, settingRW)
	require.Len(t, recs, 4)
	for _, r := range recs {
		for _, id := range r.Messages {
			assert.Less(t, id, r.First, "no source at or after the batch")
		}
		for _, m := range r.Mids {
			assert.Less(t, m.Last, r.First, "no mid reaching the batch")
		}
		assert.LessOrEqual(t, len(r.Messages), kMessages)
		assert.LessOrEqual(t, len(r.Mids), kMids)
	}
	third := recs[2]
	assert.Equal(t, int64(17), third.First)
	assert.Contains(t, third.Messages, int64(1), "the batch naming Chloe Traike finds the base message naming her")
	assert.Contains(t, third.Messages, int64(9))
	assert.Contains(t, p.inputs[2], "RW: user: Chloe Traike answers on extension 6200.",
		"the earlier stream mid it is given is RW's own summary, not S's")
	assert.NotContains(t, p.inputs[2], `"summary":"S: user: Chloe Traike answers`)

	stored := rows(t, filepath.Join(dir, "memory.db"))
	require.Len(t, stored, 6)
	for i, r := range stored {
		if i < 2 {
			assert.True(t, strings.HasPrefix(r.Summary, "S: "), "the base keeps its recorded summary")
			continue
		}
		assert.Equal(t, strings.TrimSpace(recs[i-2].Summary), r.Summary, "the stream mid is RW's summary")
		assert.Equal(t, embedded(t, r.Summary), r.Embedding, "and its vector is RW's summary's")
		assert.Empty(t, r.Related)
	}
	assert.Equal(t, 6+24, built.Vectors, "a vector per mid and per source message")
	assert.Equal(t, 0, built.Notes["related_lines"])

	m, err := arm{}.Open(context.Background(), e, settingRW, dir)
	require.NoError(t, err)
	defer m.Close()
	items, err := m.Search(context.Background(), "Chloe Traike extension", 6)
	require.NoError(t, err)
	var texts []string
	for _, it := range items {
		texts = append(texts, it.Text)
	}
	assert.Contains(t, texts, stored[2].Summary, "recall shows RW's summaries")
}

func TestBuildRW_ResumesAndKeepsAFailedBatchAsS(t *testing.T) {
	p := &proxy{}
	e := env(t, p)
	path := recordingPath(e, settingRW)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	lines := `{"first":9,"last":12,"messages":[1],"mids":[],"summary":"RW recorded: Chloe Traike is on 6200."}` + "\n" +
		`{"first":13,"last":16,"messages":[],"mids":[],"failed":"no answer after two builds"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o644))

	dir := filepath.Join(e.Work, "builds", "RW", settingRW)
	_, err := arm{}.Build(context.Background(), e, settingRW, dir)
	require.NoError(t, err)
	assert.Equal(t, 2, p.calls(), "only the two unrecorded batches are sent")
	assert.Contains(t, p.inputs[0], "RW recorded: Chloe Traike is on 6200.", "a recorded RW summary is searched as RW's")
	stored := rows(t, filepath.Join(dir, "memory.db"))
	assert.Equal(t, "S: user: The harbour ferry leaves at noon.", stored[3].Summary, "a failed batch keeps S's mid")
	assert.Equal(t, embedded(t, stored[3].Summary), stored[3].Embedding)

	require.NoError(t, os.RemoveAll(dir))
	_, err = arm{}.Build(context.Background(), e, settingRW, dir)
	require.NoError(t, err)
	assert.Equal(t, 2, p.calls(), "a complete recording makes no call")
}

func TestBuildKey_KeepsSSummaryAndPutsTheLineInTheKeyOnly(t *testing.T) {
	p := &proxy{}
	e := env(t, p)
	dir := filepath.Join(e.Work, "builds", "RW", settingKey)
	built, err := arm{}.Build(context.Background(), e, settingKey, dir)
	require.NoError(t, err)
	assert.Equal(t, 4, p.calls())
	for _, route := range p.routes {
		assert.Contains(t, route, "/rw-key-b4/")
	}

	recs := recording(t, e, settingKey)
	require.Len(t, recs, 4)
	byFirst := map[int64]record{}
	for _, r := range recs {
		byFirst[r.First] = r
		assert.Empty(t, r.Summary, "RW-key writes no summary")
		for _, id := range r.Messages {
			assert.Less(t, id, r.First)
		}
		for _, m := range r.Mids {
			assert.Less(t, m.Last, r.First)
		}
	}
	stored := rows(t, filepath.Join(dir, "memory.db"))
	shared := rows(t, filepath.Join(e.Work, "shared", "b4", "memory.db"))
	for i, r := range stored {
		assert.Equal(t, shared[i].Summary, r.Summary, "every summary is S's")
		if i < 2 {
			assert.Equal(t, shared[i].Embedding, r.Embedding, "the base is untouched")
			continue
		}
		rec := byFirst[r.First]
		assert.Equal(t, rec.Line, r.Related)
		assert.Equal(t, embedded(t, keyText(r.Summary, rec.Line)), r.Embedding, "the key is the line, then S's summary")
	}
	assert.Equal(t, "Related to: Chloe Traike; earlier mids 1, 2, 3, 4.", byFirst[17].Line, "every earlier mid is among the 4 found")
	assert.NotEqual(t, shared[4].Embedding, stored[4].Embedding, "a line changes the key")
	assert.Equal(t, lineChars(byFirst), built.Notes["related_chars"], "the lines count as stored")

	m, err := arm{}.Open(context.Background(), e, settingKey, dir)
	require.NoError(t, err)
	defer m.Close()
	items, err := m.Search(context.Background(), "Chloe Traike", 6)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, it := range items {
		assert.NotContains(t, it.Text, "Related to", "the line is never shown")
		assert.NotContains(t, it.Text, "earlier mids")
		assert.True(t, strings.HasPrefix(it.Text, "S: "), "what is shown is S's summary")
	}
}

func lineChars(recs map[int64]record) int {
	n := 0
	for _, r := range recs {
		n += len([]rune(r.Line))
	}
	return n
}

func TestKeyText_PutsTheLineBeforeTheSummary(t *testing.T) {
	assert.Equal(t, "Related to: Chloe Traike.\n\nS: a summary.", keyText("S: a summary.", "Related to: Chloe Traike."))
	assert.Equal(t, "S: a summary.", keyText("S: a summary.", ""), "no line, the summary alone")
}
