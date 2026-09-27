// Command prev is arm P of the #890 validation study: each stream batch's
// synthesis call also sees earlier episodes and says which of them the
// batch continues, and recall fills the shipped historical slots in turn
// from the similar mids and the linked neighbours of mids already
// delivered. See README.md beside it.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

func main() { study.Main(New()) }

// New returns arm P.
func New() study.Arm { return arm{} }

type arm struct{}

func (arm) Name() string { return "P" }

// Settings: b4-prev gives each batch the previous mid; b4-k5 gives it the
// five earlier mids most similar to its text.
func (arm) Settings() []string   { return []string{"b4-prev", "b4-k5"} }
func (arm) Instructions() string { return "" }

// candidates is how many earlier mids b4-k5 gives a batch.
const candidates = 5

// linkSchema is the table P adds to the production store: which earlier
// mid each mid's synthesis said it continues.
const linkSchema = `CREATE TABLE mid_link (
 mid INTEGER NOT NULL REFERENCES conversation_mid(id),
 linked INTEGER NOT NULL REFERENCES conversation_mid(id),
 PRIMARY KEY (mid, linked),
 CHECK (linked < mid)
)`

func checkSetting(setting string) error {
	if setting != "b4-prev" && setting != "b4-k5" {
		return fmt.Errorf("unknown setting %q", setting)
	}
	return nil
}

// Build copies the shared batch-4 store, works out the earlier mids each
// stream batch is given, records the synthesis call's answer for each, and
// writes the links beside the mids. The base is not synthesised again
// (../../preregistration.md §Arms), so its mids link to nothing.
func (arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	if err := checkSetting(setting); err != nil {
		return study.Built{}, err
	}
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	mids, err := readMids(ctx, path)
	if err != nil {
		return study.Built{}, err
	}
	requests, err := requestsFor(ctx, env, setting, path, mids)
	if err != nil {
		return study.Built{}, err
	}
	recorded, err := recordLinks(ctx, env, setting, requests)
	if err != nil {
		return study.Built{}, err
	}
	byFirst := map[int64]int64{}
	chars := 0
	for _, m := range mids {
		byFirst[m.SourceFirst] = m.ID
		chars += len([]rune(m.Summary))
	}
	links := map[int64][]int64{}
	linked, failed := 0, 0
	for _, b := range recorded {
		links[byFirst[b.First]] = b.Links
		if len(b.Links) > 0 {
			linked++
		}
		if b.Failed != "" {
			failed++
		}
	}
	written, err := writeLinks(ctx, path, links)
	if err != nil {
		return study.Built{}, err
	}
	return study.Built{
		StoredChars: chars,
		Vectors:     len(mids),
		Units:       map[string]int{"mid": len(mids), "link": written},
		Notes: map[string]any{
			"stream_batches": len(requests), "batches_recorded": len(recorded), "batches_failed": failed,
			"mids_continuing": linked, "share_continuing": float64(linked) / float64(max(len(requests), 1)),
		},
	}, nil
}

// requestsFor gives every stream batch its earlier mids, each with its
// recorded summary: at b4-prev the mid just before the batch's own, at
// b4-k5 the five mids before it most similar to the batch's text, by the
// production store's ranking.
func requestsFor(ctx context.Context, env *study.Env, setting, path string, mids []agent.ConversationHit) ([]request, error) {
	byFirst := map[int64]int{}
	for i, m := range mids {
		byFirst[m.SourceFirst] = i
	}
	var store *sqlite.Store
	if setting == "b4-k5" {
		var err error
		if store, err = sqlite.OpenConversation(path, env.Embedder); err != nil {
			return nil, err
		}
		defer store.Close()
	}
	stream := env.Corpus.Stream
	if len(stream)%4 != 0 {
		return nil, fmt.Errorf("the stream's %d messages do not fill batches of 4", len(stream))
	}
	var out []request
	for i := 0; i < len(stream); i += 4 {
		batch := stream[i : i+4]
		at, ok := byFirst[batch[0].ID]
		if !ok || at == 0 {
			return nil, fmt.Errorf("no mid over %d-%d with one before it", batch[0].ID, batch[3].ID)
		}
		own := mids[at].ID
		var given []agent.ConversationHit
		if store == nil {
			given = mids[at-1 : at]
		} else {
			texts := make([]string, len(batch))
			for j, m := range batch {
				texts[j] = m.Role + ": " + study.Redact(m.Content)
			}
			ranked, err := store.Search(ctx, strings.Join(texts, "\n"), len(mids))
			if err != nil {
				return nil, err
			}
			for _, h := range ranked {
				if h.ID < own && len(given) < candidates {
					given = append(given, h)
				}
			}
		}
		r := request{batch: batch}
		for _, h := range given {
			r.given = append(r.given, earlier{ID: h.ID, Label: referenceLabel, Summary: h.Summary})
		}
		out = append(out, r)
	}
	return out, nil
}

func readMids(ctx context.Context, path string) ([]agent.ConversationHit, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id,summary,source_first,source_last FROM conversation_mid ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.ConversationHit
	for rows.Next() {
		var m agent.ConversationHit
		if err := rows.Scan(&m.ID, &m.Summary, &m.SourceFirst, &m.SourceLast); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// writeLinks adds the link table to the store at path and writes every
// link, returning how many it wrote.
func writeLinks(ctx context.Context, path string, links map[int64][]int64) (int, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, linkSchema); err != nil {
		return 0, err
	}
	n := 0
	for mid, list := range links {
		for _, to := range list {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mid_link(mid,linked) VALUES (?,?)`, mid, to); err != nil {
				return 0, fmt.Errorf("link %d to %d: %w", mid, to, err)
			}
			n++
		}
	}
	return n, tx.Commit()
}

// readLinks reads every link as neighbours in both directions.
func readLinks(ctx context.Context, path string) (map[int64][]int64, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT mid,linked FROM mid_link`)
	if err != nil {
		return nil, fmt.Errorf("read links: %w", err)
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var mid, linked int64
		if err := rows.Scan(&mid, &linked); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], linked)
		out[linked] = append(out[linked], mid)
	}
	return out, rows.Err()
}

// Open reads the links and opens the production store under them.
func (arm) Open(ctx context.Context, env *study.Env, setting, dir string) (study.Memory, error) {
	if err := checkSetting(setting); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "memory.db")
	links, err := readLinks(ctx, path)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return nil, err
	}
	return &memory{store: store, links: links}, nil
}
