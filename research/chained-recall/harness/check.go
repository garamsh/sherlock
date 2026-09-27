package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

type checked struct {
	ID     string `json:"id"`
	Family string `json:"family"`
	// Raw is the rank, by the production similarity search with the raw
	// query, of the mid holding each hop's statement; Oracle is its rank
	// for the hop's own statement.
	Raw    []int `json:"raw"`
	Oracle []int `json:"oracle"`
}

// check is the chain check on a shared store, after synthesis: the
// production similarity ranking with the raw query must not place any hop
// 2 or hop 3 mid within ScreenTop, while each hop's own statement ranks its
// mid within the oracle depth.
func check(ctx context.Context, env *study.Env, n int) error {
	dir, err := env.SharedStore(n)
	if err != nil {
		return err
	}
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	if err != nil {
		return err
	}
	defer store.Close()
	rank := func(text string, source int64) (int, error) {
		hits, err := store.Search(ctx, text, 1<<20)
		if err != nil {
			return 0, err
		}
		for i, h := range hits {
			if h.SourceFirst <= source && source <= h.SourceLast {
				return i + 1, nil
			}
		}
		return 0, fmt.Errorf("no mid holds source %d", source)
	}
	var rows []checked
	reached, hops, oracle, total := 0, 0, 0, 0
	for _, c := range env.Corpus.Chains {
		row := checked{ID: c.ID, Family: c.Family}
		for i, h := range c.Hops {
			r, err := rank(c.Query, h.Source)
			if err != nil {
				return err
			}
			o, err := rank(h.Statement, h.Source)
			if err != nil {
				return err
			}
			row.Raw, row.Oracle = append(row.Raw, r), append(row.Oracle, o)
			total++
			if o <= study.OracleTop {
				oracle++
			}
			if i > 0 {
				hops++
				if r <= ScreenTop {
					reached++
				}
			}
		}
		rows = append(rows, row)
	}
	summary := map[string]any{
		"batch": n, "top": ScreenTop, "oracle_top": study.OracleTop,
		"later_hops": hops, "later_hops_within_top": reached,
		"hops": total, "hops_oracle_reached": oracle,
	}
	fmt.Println(summary)
	data, err := json.MarshalIndent(map[string]any{"summary": summary, "chains": rows}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(env.Dir, "evidence", fmt.Sprintf("check-b%d.json", n)), append(data, '\n'), 0o644)
}
