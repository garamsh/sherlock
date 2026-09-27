// Command parity ranks the #856 replication corpus through the production
// recall at batch 4 (issue #858). It replays the recorded n = 4 syntheses
// through the production chat client and summarizer, embeds with the
// production OpenAI-compatible embedder at -embed-url, and scores each query
// by the replication's delivered-hit rule.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

type source struct {
	ID      int64  `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type query struct {
	ID       string  `json:"id"`
	Family   string  `json:"family"`
	Evidence []int64 `json:"evidence"`
	Text     string  `json:"text"`
}

type synthesis struct {
	Batch    int             `json:"batch"`
	Summary  string          `json:"summary"`
	Response json.RawMessage `json:"response"`
}

type row struct {
	ID        string     `json:"id"`
	Family    string     `json:"family"`
	Delivered [][2]int64 `json:"delivered"`
	Hit       *bool      `json:"hit"`
	B4Hit     *bool      `json:"b4_hit"`
}

var primary = []string{"exact", "paraphrase", "implicit", "correction"}

func main() {
	evidence := flag.String("evidence", "docs/research/mid-memory-rag/evidence/replication", "replication evidence directory")
	out := flag.String("out", "docs/research/mid-memory-rag/evidence/production-parity", "output directory")
	embedURL := flag.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL")
	embedModel := flag.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model name")
	flag.Parse()
	if err := run(*evidence, *out, *embedURL, *embedModel); err != nil {
		log.Fatal(err)
	}
}

func run(evidence, out, embedURL, embedModel string) error {
	ctx := context.Background()
	var corpus []source
	var queries []query
	if err := readJSON(filepath.Join(evidence, "corpus.json"), &corpus); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(evidence, "queries.json"), &queries); err != nil {
		return err
	}
	recorded, err := readSyntheses(filepath.Join(evidence, "synthesis-n4.jsonl"))
	if err != nil {
		return err
	}
	replay := &replayServer{recorded: recorded}
	server := httptest.NewServer(replay)
	defer server.Close()
	model, err := openai.New(openai.Config{BaseURL: server.URL, Model: "MiniMax-M2", APIKey: "replay"})
	if err != nil {
		return err
	}
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: embedURL, Model: embedModel})
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "parity-858-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), embedder)
	if err != nil {
		return err
	}
	defer store.Close()

	started := time.Now()
	summarizer := memory.ConversationSummary{Model: model}
	for _, message := range corpus {
		turn := []agent.SourceMessage{{Role: agent.Role(message.Role), Content: message.Content}}
		if err := store.RecordTurn(ctx, turn, summarizer); err != nil {
			return fmt.Errorf("record source %d: %w", message.ID, err)
		}
	}
	built := time.Since(started)
	mids, err := store.RecentMid(ctx, len(corpus))
	if err != nil {
		return err
	}
	intervals := map[string][2]int64{}
	differing := 0
	for _, mid := range mids {
		batch := int(mid.SourceLast / 4)
		if mid.SourceFirst != mid.SourceLast-3 || mid.SourceLast%4 != 0 || recorded[batch] == nil {
			return fmt.Errorf("mid %d covers %d-%d, not one recorded batch of 4", mid.ID, mid.SourceFirst, mid.SourceLast)
		}
		if mid.Summary != strings.TrimSpace(recorded[batch].Summary) {
			differing++
		}
		intervals[strconv.FormatInt(mid.ID, 10)] = [2]int64{mid.SourceFirst, mid.SourceLast}
	}
	if len(mids) != len(recorded) || replay.calls != len(recorded) {
		return fmt.Errorf("%d mids from %d synthesis calls; want %d", len(mids), replay.calls, len(recorded))
	}
	b4, err := readB4Hits(filepath.Join(evidence, "scores.json"))
	if err != nil {
		return err
	}

	started = time.Now()
	recall := memory.ConversationRecall{Store: store}
	var rows []row
	for _, q := range queries {
		recalled, err := recall.Recall(ctx, q.Text)
		if err != nil {
			return fmt.Errorf("recall %s: %w", q.ID, err)
		}
		r := row{ID: q.ID, Family: q.Family, B4Hit: b4[q.ID]}
		for _, m := range recalled.Memories {
			span, ok := intervals[m.ID]
			if !ok {
				return fmt.Errorf("recall %s delivered %q, which is not a mid", q.ID, m.ID)
			}
			r.Delivered = append(r.Delivered, span)
		}
		if len(q.Evidence) > 0 {
			hit := covers(r.Delivered, q.Evidence)
			r.Hit = &hit
		}
		rows = append(rows, r)
	}
	recalled := time.Since(started)
	return writeResults(out, rows, map[string]any{
		"mids":                                   len(mids),
		"synthesis_calls_replayed":               replay.calls,
		"summaries_differing_from_recorded_text": differing,
		"build_seconds":                          built.Seconds(),
		"recall_seconds_per_query":               recalled.Seconds() / float64(len(queries)),
		"embed_url":                              embedURL,
		"embed_model":                            embedModel,
	})
}

// replayServer answers each synthesis request with the recorded response of
// the next batch; Accumulate synthesizes batches in source order.
type replayServer struct {
	mu       sync.Mutex
	recorded map[int]*synthesis
	calls    int
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.calls++
	record := s.recorded[s.calls]
	s.mu.Unlock()
	if record == nil {
		http.Error(w, "no recorded synthesis for this batch", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(record.Response)
}

func covers(delivered [][2]int64, evidence []int64) bool {
	for _, span := range delivered {
		for _, id := range evidence {
			if span[0] <= id && id <= span[1] {
				return true
			}
		}
	}
	return false
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func readSyntheses(path string) (map[int]*synthesis, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	out := map[int]*synthesis{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var record synthesis
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		out[record.Batch] = &record
	}
	return out, scanner.Err()
}

func readB4Hits(path string) (map[string]*bool, error) {
	var scores struct {
		PerQuery []struct {
			ID   string `json:"id"`
			Arms map[string]struct {
				Hit *bool `json:"hit"`
			} `json:"arms"`
		} `json:"per_query"`
	}
	if err := readJSON(path, &scores); err != nil {
		return nil, err
	}
	out := map[string]*bool{}
	for _, q := range scores.PerQuery {
		out[q.ID] = q.Arms["B4"].Hit
	}
	return out, nil
}

// writeResults writes every row and the per-family table, set against B4.
func writeResults(out string, rows []row, run map[string]any) error {
	type tally struct {
		Hits       int `json:"hits"`
		Queries    int `json:"queries"`
		B4Hits     int `json:"b4_hits"`
		Both       int `json:"both"`
		ParityOnly int `json:"parity_only"`
		B4Only     int `json:"b4_only"`
	}
	families := map[string]*tally{}
	total := &tally{}
	for _, r := range rows {
		if r.Hit == nil {
			continue
		}
		t := families[r.Family]
		if t == nil {
			t = &tally{}
			families[r.Family] = t
		}
		targets := []*tally{t}
		for _, family := range primary {
			if r.Family == family {
				targets = append(targets, total)
			}
		}
		for _, t := range targets {
			t.Queries++
			hit, b4 := *r.Hit, r.B4Hit != nil && *r.B4Hit
			t.Hits += b2i(hit)
			t.B4Hits += b2i(b4)
			t.Both += b2i(hit && b4)
			t.ParityOnly += b2i(hit && !b4)
			t.B4Only += b2i(b4 && !hit)
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for name, v := range map[string]any{
		"rankings.json": rows,
		"scores.json":   map[string]any{"run": run, "primary": total, "families": families},
	} {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name), append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("primary %d of %d (B4 %d; both %d, parity only %d, B4 only %d)\n",
		total.Hits, total.Queries, total.B4Hits, total.Both, total.ParityOnly, total.B4Only)
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
