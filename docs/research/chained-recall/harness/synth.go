package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent/memory"
)

// synthesize asks the session model, through the proxy and the production
// summarizer, for the summary of every stream batch at batch size n, and
// appends each to evidence/synthesis/stream-b<n>.jsonl. The request is the
// one the production store sends: each message as "role: content", after
// the store's redaction. A run resumes after the batches already recorded.
// While the proxy is paused on the usage limit, workers wait and retry.
func synthesize(ctx context.Context, env *study.Env, n, workers int) error {
	stream := env.Corpus.Stream
	if len(stream)%n != 0 {
		return fmt.Errorf("the stream's %d messages do not fill batches of %d", len(stream), n)
	}
	path := filepath.Join(env.Dir, "evidence", "synthesis", fmt.Sprintf("stream-b%d.jsonl", n))
	done := map[int64]bool{}
	if _, err := os.Stat(path); err == nil {
		recorded, err := study.ReadJSONL[study.Synthesis](path)
		if err != nil {
			return err
		}
		for _, s := range recorded {
			done[s.First] = true
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	type job struct {
		index int
		batch []study.Message
	}
	jobs := make(chan job)
	var mu sync.Mutex
	var failed []string
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				s, err := synthesizeBatch(ctx, env, n, j.index, j.batch)
				mu.Lock()
				if err != nil {
					failed = append(failed, fmt.Sprintf("%d-%d: %v", j.batch[0].ID, j.batch[len(j.batch)-1].ID, err))
				} else {
					line, _ := json.Marshal(s)
					_, _ = out.Write(append(line, '\n'))
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < len(stream); i += n {
		if !done[stream[i].ID] {
			jobs <- job{index: i / n, batch: stream[i : i+n]}
		}
	}
	close(jobs)
	wg.Wait()
	if len(failed) > 0 {
		return fmt.Errorf("%d batches failed; re-run to retry them:\n%s", len(failed), strings.Join(failed, "\n"))
	}
	return nil
}

func synthesizeBatch(ctx context.Context, env *study.Env, n, index int, batch []study.Message) (study.Synthesis, error) {
	updates := make([]string, len(batch))
	for i, m := range batch {
		updates[i] = m.Role + ": " + study.Redact(m.Content)
	}
	model, err := env.Model(fmt.Sprintf("b%d-%04d", n, index+1), fmt.Sprintf("synth-b%d", n))
	if err != nil {
		return study.Synthesis{}, err
	}
	summarizer := memory.ConversationSummary{Model: model}
	var last error
	for attempt := 1; attempt <= 3; {
		waitWhilePaused(ctx, env.Proxy)
		summary, err := summarizer.Summarize(ctx, updates)
		if err == nil {
			return study.Synthesis{First: batch[0].ID, Last: batch[len(batch)-1].ID, Summary: summary}, nil
		}
		last = err
		if pausedNow(env.Proxy) {
			continue // a quota pause does not use up an attempt
		}
		attempt++
	}
	return study.Synthesis{}, last
}

// pausedNow asks the proxy whether it is paused on the usage limit.
func pausedNow(proxy string) bool {
	resp, err := http.Get(strings.TrimSuffix(proxy, "/") + "/budget")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var state struct {
		Paused bool `json:"paused"`
	}
	return json.NewDecoder(resp.Body).Decode(&state) == nil && state.Paused
}

// waitWhilePaused blocks while the proxy is paused, reading its local
// state once a minute; the proxy alone probes the provider.
func waitWhilePaused(ctx context.Context, proxy string) {
	logged := false
	for pausedNow(proxy) {
		if !logged {
			log.Print("proxy paused on the usage limit; waiting")
			logged = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}
