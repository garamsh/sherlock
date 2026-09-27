package main

import (
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent/openai"
)

// messageVectors embeds every source message once and keeps the vectors
// in the scratch root, so a resumed build does not embed them again. A
// cache that does not hold exactly these messages is recomputed.
func messageVectors(ctx context.Context, env *study.Env, messages []study.Message) ([]vec, error) {
	path := filepath.Join(env.Work, "retrieved", "messages.gob")
	if f, err := os.Open(path); err == nil {
		var cached []vec
		err := gob.NewDecoder(f).Decode(&cached)
		f.Close()
		if err == nil && sameMessages(cached, messages) {
			return cached, nil
		}
	}
	out, err := embedMessages(ctx, env.Embedder, messages)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return out, gob.NewEncoder(f).Encode(out)
}

func sameMessages(cached []vec, messages []study.Message) bool {
	if len(cached) != len(messages) {
		return false
	}
	for i, m := range messages {
		if cached[i].ID != m.ID || cached[i].Text != asSent([]study.Message{m})[0] {
			return false
		}
	}
	return true
}

// screen records, with no model call, the search at write that RW-key
// makes for every stream batch: its mids are S's, so its retrieval is
// fixed before any call. RW's search over source messages is the same;
// its search over mids differs once its own summaries differ from S's.
// score.py screen reads the result as the ceiling on what the retrieval
// can link (preregistration.md §Collection).
//
//	retrieved screen -dir <study> -work <scratch> -embed-url <TEI>
func screen(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("screen", flag.ExitOnError)
	dir := fs.String("dir", "", "study directory")
	work := fs.String("work", "", "scratch root outside the repository")
	embedURL := fs.String("embed-url", "", "OpenAI-compatible embeddings base URL (local TEI)")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model")
	_ = fs.Parse(args)
	if *dir == "" || *work == "" || *embedURL == "" {
		return errors.New("screen needs -dir, -work and -embed-url")
	}
	corpus, err := study.LoadCorpus(*dir)
	if err != nil {
		return err
	}
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: *embedURL, Model: *embedModel})
	if err != nil {
		return err
	}
	env := &study.Env{Corpus: corpus, Embedder: embedder, Dir: *dir, Work: *work, Arm: "RW"}
	return writeScreen(ctx, env)
}

func writeScreen(ctx context.Context, env *study.Env) error {
	shared, err := env.SharedStore(4)
	if err != nil {
		return err
	}
	mids, err := readMids(ctx, filepath.Join(shared, "memory.db"))
	if err != nil {
		return err
	}
	streamBatches, messages, err := batches(env)
	if err != nil {
		return err
	}
	vectors, err := messageVectors(ctx, env, messages)
	if err != nil {
		return err
	}
	path := filepath.Join(env.Dir, "arms", "retrieved", "evidence", "screen-b4.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, batch := range streamBatches {
		r, err := retrieve(ctx, env.Embedder, batch, vectors, mids)
		if err != nil {
			return err
		}
		if err := writeRecord(f, recordOf(batch, r)); err != nil {
			return fmt.Errorf("write screen: %w", err)
		}
	}
	return nil
}
