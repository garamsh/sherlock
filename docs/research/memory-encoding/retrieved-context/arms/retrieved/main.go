// Command retrieved holds arms RW and RW-key of issue #902: each stream
// batch's mid is written with the earlier source messages and earlier
// mids a search at write finds for it. RW lets the summary use them;
// RW-key keeps S's summary and puts only a bounded "related to" line into
// the mid's embedding key. See ../../README.md and ../../preregistration.md.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "screen" {
		if err := screen(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	study.Main(New())
}

// New returns the arm; its two settings are RW and RW-key.
func New() study.Arm { return arm{} }

type arm struct{}

const (
	// settingRW is the owner's form: the summary may use what was found.
	settingRW = "b4-rw"
	// settingKey keeps S's summary; what was found reaches the key only.
	settingKey = "b4-rw-key"
)

func (arm) Name() string         { return "RW" }
func (arm) Settings() []string   { return []string{settingRW, settingKey} }
func (arm) Instructions() string { return "" }

// midRef names one retrieved mid and the sources it covers, so the
// recording shows that nothing at or after the batch was read.
type midRef struct {
	ID    int64 `json:"id"`
	First int64 `json:"first"`
	Last  int64 `json:"last"`
}

// record is one stream batch's write: what the search found, and what the
// call returned. Failed records why a batch has none: a failure no retry
// recovered, recorded by hand after two builds, keeps S's mid unchanged
// and is not sent again.
type record struct {
	First    int64    `json:"first"`
	Last     int64    `json:"last"`
	Messages []int64  `json:"messages"`
	Mids     []midRef `json:"mids"`
	Summary  string   `json:"summary,omitempty"`
	Related  *related `json:"related,omitempty"`
	Line     string   `json:"line,omitempty"`
	Attempts int      `json:"attempts,omitempty"`
	Failed   string   `json:"failed,omitempty"`
}

func recordOf(batch []study.Message, r retrieved) record {
	rec := record{First: batch[0].ID, Last: batch[len(batch)-1].ID, Messages: []int64{}, Mids: []midRef{}}
	for _, m := range r.Messages {
		rec.Messages = append(rec.Messages, m.ID)
	}
	for _, m := range r.Mids {
		rec.Mids = append(rec.Mids, midRef{ID: m.ID, First: m.First, Last: m.Last})
	}
	return rec
}

// recordingPath is where a setting's writes are recorded.
func recordingPath(env *study.Env, setting string) string {
	return filepath.Join(env.Dir, "arms", "retrieved", "evidence", setting+".jsonl")
}

func readRecording(path string) (map[int64]record, error) {
	out := map[int64]record{}
	if _, err := os.Stat(path); err != nil {
		return out, nil
	}
	rows, err := study.ReadJSONL[record](path)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.First] = r
	}
	return out, nil
}

// stored is a summary as the production store keeps it.
func stored(summary string) string { return strings.TrimSpace(study.Redact(summary)) }

// keyText is what an RW-key mid's vector is computed from: the related
// line, a blank line, then S's summary, the line first as C placed its
// context. The line is never shown.
func keyText(summary, line string) string {
	if line == "" {
		return summary
	}
	return line + "\n\n" + summary
}

// batches returns the stream in batches of four, with every message's
// place in the whole corpus.
func batches(env *study.Env) ([][]study.Message, []study.Message, error) {
	messages := env.Corpus.Messages()
	start := len(env.Corpus.Base)
	if (len(messages)-start)%4 != 0 {
		return nil, nil, fmt.Errorf("the stream's %d messages do not fill batches of 4", len(messages)-start)
	}
	var out [][]study.Message
	for i := start; i < len(messages); i += 4 {
		out = append(out, messages[i:i+4])
	}
	return out, messages, nil
}

// Build copies the shared batch-4 store, records the setting's write of
// every stream batch (calling the model only for batches not yet
// recorded), and applies the recording to the copy. The base's mids keep
// their recorded summaries (../../../validation/preregistration.md §Arms).
func (arm) Build(ctx context.Context, env *study.Env, s, dir string) (study.Built, error) {
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	mids, err := readMids(ctx, filepath.Join(shared, "memory.db"))
	if err != nil {
		return study.Built{}, err
	}
	streamBatches, messages, err := batches(env)
	if err != nil {
		return study.Built{}, err
	}
	vectors, err := messageVectors(ctx, env, messages)
	if err != nil {
		return study.Built{}, err
	}
	var recorded map[int64]record
	var keys map[int64][]float32
	switch s {
	case settingRW:
		recorded, keys, err = recordRW(ctx, env, streamBatches, vectors, mids)
	case settingKey:
		recorded, err = recordKey(ctx, env, streamBatches, vectors, mids)
	default:
		return study.Built{}, fmt.Errorf("unknown setting %q", s)
	}
	if err != nil {
		return study.Built{}, err
	}
	if len(recorded) != len(streamBatches) {
		return study.Built{}, fmt.Errorf("%d stream batches recorded; the stream has %d", len(recorded), len(streamBatches))
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	if err := apply(ctx, env, path, s, recorded, keys); err != nil {
		return study.Built{}, err
	}
	return stats(ctx, path, len(vectors))
}

// sharedMid is S's mid over a batch, from the shared store.
func sharedMid(mids []vec, first int64) (vec, bool) {
	for _, m := range mids {
		if m.First == first {
			return m, true
		}
	}
	return vec{}, false
}

// recordRW writes the stream in order, one batch at a time: each batch's
// search reads the base's mids and RW's own mids of every earlier batch,
// so a later batch sees what an earlier one wrote. It returns the
// recording and the vector of every RW summary, which is both what the
// later searches rank and the mid's stored key.
func recordRW(ctx context.Context, env *study.Env, streamBatches [][]study.Message, messages, mids []vec) (map[int64]record, map[int64][]float32, error) {
	path := recordingPath(env, settingRW)
	recorded, err := readRecording(path)
	if err != nil {
		return nil, nil, err
	}
	var pool []vec
	for _, m := range mids {
		if m.Last < streamBatches[0][0].ID {
			pool = append(pool, m)
		}
	}
	keys := map[int64][]float32{}
	var f *os.File
	for _, batch := range streamBatches {
		own, ok := sharedMid(mids, batch[0].ID)
		if !ok {
			return nil, nil, fmt.Errorf("no shared mid covers the batch at %d", batch[0].ID)
		}
		rec, done := recorded[batch[0].ID]
		if !done {
			r, err := retrieve(ctx, env.Embedder, batch, messages, pool)
			if err != nil {
				return nil, nil, err
			}
			rec = recordOf(batch, r)
			summary, attempts, err := callWithRetries(ctx, env, func(attempt int) (string, error) {
				model, err := env.Model(episode(batch), "rw-b4")
				if err != nil {
					return "", err
				}
				return synthesizeRW(ctx, model, r, asSent(batch), attempt)
			})
			if err != nil {
				return nil, nil, fmt.Errorf("batch %d-%d: %w; build again to retry it", rec.First, rec.Last, err)
			}
			rec.Summary, rec.Attempts = summary, attempts
			if f == nil {
				if f, err = appendTo(path); err != nil {
					return nil, nil, err
				}
				defer f.Close()
			}
			if err := writeRecord(f, rec); err != nil {
				return nil, nil, err
			}
			recorded[rec.First] = rec
		}
		if rec.Failed != "" {
			pool = append(pool, own)
			continue
		}
		text := stored(rec.Summary)
		v, err := env.Embedder.Embed(ctx, text)
		if err != nil {
			return nil, nil, fmt.Errorf("embed RW mid at %d: %w", rec.First, err)
		}
		keys[rec.First] = v
		pool = append(pool, vec{ID: own.ID, First: own.First, Last: own.Last, Text: text, Vector: v})
	}
	return recorded, keys, nil
}

// keyWorkers is how many RW-key requests go at once; the key is shared.
const keyWorkers = 2

// recordKey writes RW-key's related line for every stream batch. Its
// search reads S's mids, which RW-key keeps as its summaries, so no batch
// waits for another and two go at once.
func recordKey(ctx context.Context, env *study.Env, streamBatches [][]study.Message, messages, mids []vec) (map[int64]record, error) {
	path := recordingPath(env, settingKey)
	recorded, err := readRecording(path)
	if err != nil {
		return nil, err
	}
	var pending [][]study.Message
	for _, b := range streamBatches {
		if _, ok := recorded[b[0].ID]; !ok {
			pending = append(pending, b)
		}
	}
	if len(pending) == 0 {
		return recorded, nil
	}
	f, err := appendTo(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	jobs := make(chan []study.Message)
	var mu sync.Mutex
	var failed []string
	var wg sync.WaitGroup
	for range keyWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range jobs {
				rec, err := keyBatch(ctx, env, batch, messages, mids)
				mu.Lock()
				if err == nil {
					err = writeRecord(f, rec)
				}
				if err != nil {
					failed = append(failed, fmt.Sprintf("%d-%d: %v", batch[0].ID, batch[len(batch)-1].ID, err))
				} else {
					recorded[rec.First] = rec
				}
				mu.Unlock()
			}
		}()
	}
	for _, b := range pending {
		jobs <- b
	}
	close(jobs)
	wg.Wait()
	if len(failed) > 0 {
		return nil, fmt.Errorf("%d batches failed; build again to retry them:\n%s", len(failed), strings.Join(failed, "\n"))
	}
	return recorded, nil
}

func keyBatch(ctx context.Context, env *study.Env, batch []study.Message, messages, mids []vec) (record, error) {
	r, err := retrieve(ctx, env.Embedder, batch, messages, mids)
	if err != nil {
		return record{}, err
	}
	rec := recordOf(batch, r)
	var got related
	_, attempts, err := callWithRetries(ctx, env, func(attempt int) (string, error) {
		model, err := env.Model(episode(batch), "rw-key-b4")
		if err != nil {
			return "", err
		}
		got, err = synthesizeRelated(ctx, model, r, asSent(batch), attempt)
		return "", err
	})
	if err != nil {
		return record{}, err
	}
	rec.Related, rec.Line, rec.Attempts = &got, relatedLine(got), attempts
	return rec, nil
}

// episode names a batch in the ledger as the shared synthesis does.
func episode(batch []study.Message) string {
	return fmt.Sprintf("b4-%05d", (batch[0].ID-1)/4+1)
}

// callWithRetries tries a batch's call three times; a pause on the usage
// limit uses up no attempt.
func callWithRetries(ctx context.Context, env *study.Env, call func(attempt int) (string, error)) (string, int, error) {
	var last error
	for attempt := 1; attempt <= 3; {
		study.WaitWhilePaused(ctx, env.Proxy)
		out, err := call(attempt)
		if err == nil {
			return out, attempt, nil
		}
		last = err
		if study.Paused(env.Proxy) {
			continue
		}
		attempt++
	}
	return "", 3, last
}

func appendTo(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func writeRecord(f *os.File, rec record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// apply writes the recording into the copied store. RW replaces each
// stream mid's summary and its vector with RW's; RW-key keeps S's summary,
// stores the related line in its own column and recomputes the vector from
// both. A failed batch keeps S's mid.
func apply(ctx context.Context, env *study.Env, path, s string, recorded map[int64]record, keys map[int64][]float32) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `ALTER TABLE conversation_mid ADD COLUMN related TEXT`); err != nil {
		return err
	}
	for first, rec := range recorded {
		if rec.Failed != "" {
			continue
		}
		if err := applyOne(ctx, env, tx, s, rec, keys[first]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func applyOne(ctx context.Context, env *study.Env, tx *sql.Tx, s string, rec record, key []float32) error {
	var res sql.Result
	var err error
	if s == settingRW {
		res, err = tx.ExecContext(ctx, `UPDATE conversation_mid SET summary=?, embedding=? WHERE source_first=? AND source_last=?`,
			stored(rec.Summary), encodeVector(key), rec.First, rec.Last)
	} else {
		var summary string
		if err := tx.QueryRowContext(ctx, `SELECT summary FROM conversation_mid WHERE source_first=? AND source_last=?`, rec.First, rec.Last).Scan(&summary); err != nil {
			return fmt.Errorf("no mid covers %d-%d in the shared store: %w", rec.First, rec.Last, err)
		}
		v, err := env.Embedder.Embed(ctx, keyText(summary, rec.Line))
		if err != nil {
			return fmt.Errorf("embed RW-key mid at %d: %w", rec.First, err)
		}
		res, err = tx.ExecContext(ctx, `UPDATE conversation_mid SET related=?, embedding=? WHERE source_first=? AND source_last=?`,
			rec.Line, encodeVector(v), rec.First, rec.Last)
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("no mid covers %d-%d in the shared store", rec.First, rec.Last)
	}
	return nil
}

// stats counts what the setting stores: every summary and every related
// line, one vector per mid, and the vector of every source message that
// the search at write ranks, which the shipped store does not keep.
func stats(ctx context.Context, path string, sourceVectors int) (study.Built, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return study.Built{}, err
	}
	defer db.Close()
	var mids, chars, lines, lineChars int
	err = db.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(length(summary)),0), count(nullif(related,'')), coalesce(sum(length(related)),0) FROM conversation_mid`).
		Scan(&mids, &chars, &lines, &lineChars)
	if err != nil {
		return study.Built{}, err
	}
	return study.Built{
		StoredChars: chars + lineChars,
		Vectors:     mids + sourceVectors,
		Units:       map[string]int{"mid": mids},
		Notes:       map[string]any{"related_lines": lines, "related_chars": lineChars, "source_vectors": sourceVectors},
	}, nil
}

// Open is the shipped recall and search over the setting's store: RW's
// summaries, or S's summaries under RW-key's keys. The related line is
// never read.
func (arm) Open(ctx context.Context, env *study.Env, s, dir string) (study.Memory, error) {
	if s != settingRW && s != settingKey {
		return nil, fmt.Errorf("unknown setting %q", s)
	}
	return study.ShippedArm{ArmName: "RW", Batches: []string{"b4"}}.Open(ctx, env, "b4", dir)
}
