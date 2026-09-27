package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent/memory"
)

// groupSchema is the second level: one row per complete window of G mids,
// over the union of their source intervals.
const groupSchema = `CREATE TABLE level_group(id INTEGER PRIMARY KEY, source_first INTEGER NOT NULL, source_last INTEGER NOT NULL, summary TEXT NOT NULL, embedding BLOB NOT NULL)`

// Build copies the batch-4 shared store into dir and adds one group
// episode per complete window of G mids: the window's summary, from the
// recorded group syntheses or one model call, and its embedding.
func (arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	g, err := groupSize(setting)
	if err != nil {
		return study.Built{}, err
	}
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	mids, err := readMids(filepath.Join(shared, "memory.db"))
	if err != nil {
		return study.Built{}, err
	}
	groups, calls, err := synthesizeGroups(ctx, env, setting, g, mids)
	if err != nil {
		return study.Built{}, err
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "memory.db"))
	if err != nil {
		return study.Built{}, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, groupSchema); err != nil {
		return study.Built{}, err
	}
	stored := 0
	for _, m := range mids {
		stored += len([]rune(m.Text))
	}
	for i, s := range groups {
		summary := strings.TrimSpace(study.Redact(s.Summary))
		if summary == "" || len([]rune(summary)) > 2000 {
			return study.Built{}, fmt.Errorf("group %d-%d: summary must hold 1 to 2000 characters", s.First, s.Last)
		}
		vector, err := env.Embedder.Embed(ctx, summary)
		if err != nil {
			return study.Built{}, fmt.Errorf("embed group %d-%d: %w", s.First, s.Last, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO level_group(id,source_first,source_last,summary,embedding) VALUES (?,?,?,?,?)`, i+1, s.First, s.Last, summary, encodeVector(vector)); err != nil {
			return study.Built{}, err
		}
		stored += len([]rune(summary))
	}
	return study.Built{
		StoredChars: stored,
		Vectors:     len(mids) + len(groups),
		Units:       map[string]int{"mid": len(mids), "group": len(groups)},
		Notes: map[string]any{
			"group_size": g, "windows": len(mids) / g, "groups": len(groups),
			"ungrouped_mids": len(mids) % g, "synthesis_calls_this_build": calls,
		},
	}, nil
}

// readMids reads every mid of a store in source order.
func readMids(path string) ([]study.Item, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, source_first, source_last, summary FROM conversation_mid ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []study.Item
	for rows.Next() {
		it := study.Item{Level: "mid"}
		if err := rows.Scan(&it.ID, &it.SourceFirst, &it.SourceLast, &it.Text); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// recordPath is where a setting's group syntheses are recorded, so a
// rebuild replays them instead of calling the model again.
func recordPath(env *study.Env, setting string) string {
	return filepath.Join(env.Dir, "arms", "levels", "evidence", "groups-"+setting+".jsonl")
}

// synthesizeGroups returns the summary of every complete window of g mids,
// in source order. A window already recorded is read back; any other is
// one call of the production summarizer through the study proxy, whose
// updates are the window's mid summaries, and is recorded as it lands. The
// first failed call stops the run, so a paused key is not hammered; a
// re-run resumes after the recorded windows. It also returns how many
// calls this run made.
func synthesizeGroups(ctx context.Context, env *study.Env, setting string, g int, mids []study.Item) ([]study.Synthesis, int, error) {
	path := recordPath(env, setting)
	recorded := map[int64]study.Synthesis{}
	if _, err := os.Stat(path); err == nil {
		lines, err := study.ReadJSONL[study.Synthesis](path)
		if err != nil {
			return nil, 0, err
		}
		for _, s := range lines {
			recorded[s.First] = s
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, 0, err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, 0, err
	}
	defer out.Close()
	groups := make([]study.Synthesis, 0, len(mids)/g)
	calls := 0
	for w := 0; w+g <= len(mids); w += g {
		window := mids[w : w+g]
		first, last := window[0].SourceFirst, window[g-1].SourceLast
		if s, ok := recorded[first]; ok {
			if s.Last != last {
				return nil, calls, fmt.Errorf("recorded group %d-%d does not match window %d-%d", s.First, s.Last, first, last)
			}
			groups = append(groups, s)
			continue
		}
		updates := make([]string, g)
		for i, m := range window {
			updates[i] = fmt.Sprintf("summary of messages %d-%d: %s", m.SourceFirst, m.SourceLast, strings.TrimSpace(m.Text))
		}
		model, err := env.Model(fmt.Sprintf("%s-%04d", setting, w/g+1), "synth-"+setting)
		if err != nil {
			return nil, calls, err
		}
		calls++
		summary, err := memory.ConversationSummary{Model: model}.Summarize(ctx, updates)
		if err != nil {
			return nil, calls, fmt.Errorf("group %d-%d: %w (re-run to resume after the recorded groups)", first, last, err)
		}
		s := study.Synthesis{First: first, Last: last, Summary: summary}
		line, err := json.Marshal(s)
		if err != nil {
			return nil, calls, err
		}
		if _, err := out.Write(append(line, '\n')); err != nil {
			return nil, calls, err
		}
		groups = append(groups, s)
	}
	return groups, calls, nil
}

func encodeVector(vector []float32) []byte {
	out := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(encoded []byte) ([]float32, error) {
	if len(encoded)%4 != 0 {
		return nil, errors.New("embedding is not a float32 vector")
	}
	out := make([]float32, len(encoded)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[4*i:]))
	}
	return out, nil
}
