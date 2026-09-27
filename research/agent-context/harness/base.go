package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// runBase builds the base store every episode starts from: the #856
// replication's 4,800 messages, one per RecordTurn, with the recorded
// batch-4 syntheses replayed through the production client and summarizer,
// as #858's parity harness does. No paid call is made.
func runBase(args []string) error {
	fs := flag.NewFlagSet("base", flag.ExitOnError)
	evidence := fs.String("evidence", "research/mid-memory-rag/evidence/replication", "replication evidence directory")
	out := fs.String("out", "", "base store to write")
	embedURL := fs.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model name")
	_ = fs.Parse(args)
	if *out == "" {
		return errors.New("base needs -out")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s exists", *out)
	}
	ctx := context.Background()
	var corpus []struct {
		ID      int64  `json:"id"`
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	data, err := os.ReadFile(*evidence + "/corpus.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		return err
	}
	recorded, err := readSyntheses(*evidence + "/synthesis-n4.jsonl")
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
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: *embedURL, Model: *embedModel})
	if err != nil {
		return err
	}
	store, err := sqlite.OpenConversation(*out, embedder)
	if err != nil {
		return err
	}
	summarizer := memory.ConversationSummary{Model: model}
	for _, m := range corpus {
		if err := store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.Role(m.Role), Content: m.Content}}, summarizer); err != nil {
			store.Close()
			return fmt.Errorf("record source %d: %w", m.ID, err)
		}
	}
	mids, err := store.RecentMid(ctx, len(corpus))
	if err != nil {
		store.Close()
		return err
	}
	if len(mids) != len(recorded) || replay.calls != len(recorded) {
		store.Close()
		return fmt.Errorf("%d mids from %d replayed syntheses; want %d", len(mids), replay.calls, len(recorded))
	}
	if err := store.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(*out + suffix); err == nil {
			return fmt.Errorf("%s%s remains after close; the base is not one file", *out, suffix)
		}
	}
	fmt.Printf("base: %d messages, %d mids\n", len(corpus), len(mids))
	return nil
}

type synthesis struct {
	Batch    int             `json:"batch"`
	Response json.RawMessage `json:"response"`
}

// replayServer answers each synthesis request with the recorded response
// of the next batch; the store synthesizes batches in source order.
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
