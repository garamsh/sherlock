package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// shippedSimilar is the shipped recall's similar list (maxRecalled in
// internal/agent/memory/conversation.go), which sets S's context budget.
const shippedSimilar = 5

// levels is an opened H memory: every mid and group with its vector, held
// in memory, over the production store of the copied batch-4 store.
type levels struct {
	store    *sqlite.Store
	embedder agent.Embedder
	units    []unit
	trace    map[string]any
}

type unit struct {
	item   study.Item
	vector []float32
}

// Open reads the mids and groups Build wrote into dir.
func (arm) Open(ctx context.Context, env *study.Env, setting, dir string) (study.Memory, error) {
	path := filepath.Join(dir, "memory.db")
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var units []unit
	for _, q := range []struct{ level, query string }{
		{"mid", `SELECT id, source_first, source_last, summary, embedding FROM conversation_mid WHERE embedding IS NOT NULL`},
		{"group", `SELECT id, source_first, source_last, summary, embedding FROM level_group`},
	} {
		rows, err := db.QueryContext(ctx, q.query)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", q.level, err)
		}
		for rows.Next() {
			u := unit{item: study.Item{Level: q.level, Kind: "historical"}}
			var encoded []byte
			if err := rows.Scan(&u.item.ID, &u.item.SourceFirst, &u.item.SourceLast, &u.item.Text, &encoded); err != nil {
				rows.Close()
				return nil, err
			}
			if u.vector, err = decodeVector(encoded); err != nil {
				rows.Close()
				return nil, fmt.Errorf("%s %d: %w", q.level, u.item.ID, err)
			}
			units = append(units, u)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	// Newest first, so a tie in similarity goes to the newer unit, as in
	// the production search.
	sort.SliceStable(units, func(i, j int) bool { return units[i].item.SourceLast > units[j].item.SourceLast })
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return nil, err
	}
	return &levels{store: store, embedder: env.Embedder, units: units}, nil
}

// rank orders every mid and group by cosine similarity to the query in
// one collapsed list.
func (l *levels) rank(ctx context.Context, query string) ([]study.Item, error) {
	target, err := l.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	scores := make([]float64, len(l.units))
	order := make([]int, len(l.units))
	for i, u := range l.units {
		if len(u.vector) != len(target) {
			return nil, fmt.Errorf("%s %d has %d dimensions and the query %d", u.item.Level, u.item.ID, len(u.vector), len(target))
		}
		scores[i], order[i] = cosine(target, u.vector), i
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
	out := make([]study.Item, len(order))
	for i, k := range order {
		out[i] = l.units[k].item
	}
	return out, nil
}

// Recall delivers the two newest mids, as the shipped recall does, then
// the collapsed ranking in order while the items fit the characters S's
// recall (the two newest and five similar mids of the same store) delivers
// for the same task. The first item that does not fit ends the list, as
// RAPTOR's collapsed tree ends at its token budget.
func (l *levels) Recall(ctx context.Context, task string) ([]study.Item, error) {
	recent, err := l.store.RecentMid(ctx, 2)
	if err != nil {
		return nil, err
	}
	similar, err := l.store.Search(ctx, task, shippedSimilar+len(recent))
	if err != nil {
		return nil, err
	}
	items := make([]study.Item, 0, len(recent))
	isRecent := map[int64]bool{}
	for _, h := range recent {
		isRecent[h.ID] = true
		items = append(items, study.Item{ID: h.ID, Level: "mid", Kind: "recent", SourceFirst: h.SourceFirst, SourceLast: h.SourceLast, Text: h.Summary})
	}
	shipped := append([]study.Item(nil), items...)
	for _, h := range similar {
		if len(shipped) == shippedSimilar+len(recent) {
			break
		}
		if !isRecent[h.ID] {
			shipped = append(shipped, study.Item{ID: h.ID, Level: "mid", Kind: "historical", SourceFirst: h.SourceFirst, SourceLast: h.SourceLast, Text: h.Summary})
		}
	}
	budget := study.ContextChars(shipped)
	ranked, err := l.rank(ctx, task)
	if err != nil {
		return nil, err
	}
	for _, it := range ranked {
		if it.Level == "mid" && isRecent[it.ID] {
			continue
		}
		if study.ContextChars(append(items, it)) > budget {
			break
		}
		items = append(items, it)
	}
	l.trace = traceOf(items)
	l.trace["budget"] = budget
	return items, nil
}

// Search is the collapsed ranking's top limit, mids and groups alike.
func (l *levels) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	ranked, err := l.rank(ctx, query)
	if err != nil {
		return nil, err
	}
	items := ranked[:min(max(limit, 0), len(ranked))]
	l.trace = traceOf(items)
	return items, nil
}

// traceOf counts the delivered items per level, the control that groups
// reach the list.
func traceOf(items []study.Item) map[string]any {
	groups := 0
	for _, it := range items {
		if it.Level == "group" {
			groups++
		}
	}
	return map[string]any{"groups": groups, "mids": len(items) - groups}
}

func (l *levels) LastTrace() map[string]any       { return l.trace }
func (l *levels) Store() agent.ConversationMemory { return l.store }
func (l *levels) Close() error                    { return l.store.Close() }

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot, na, nb = dot+float64(a[i])*float64(b[i]), na+float64(a[i])*float64(a[i]), nb+float64(b[i])*float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
