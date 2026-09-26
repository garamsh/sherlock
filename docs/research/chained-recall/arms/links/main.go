// Command links is arm L of the #874 chained-recall study: each mid is
// linked to the entities its synthesis names, and recall adds the mids a
// Personalized PageRank walk reaches from the entities the query names.
// See README.md beside it.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

func main() { study.Main(New()) }

// New returns arm L.
func New() study.Arm { return arm{} }

type arm struct{}

func (arm) Name() string { return "L" }

// Settings: entities extracted by rule from the recorded summaries at batch
// 4 and 8, and entities written by the extended synthesis call at batch 4.
func (arm) Settings() []string { return []string{"b4", "b8", "b4-model"} }

func (arm) Instructions() string { return "" }

func parseSetting(setting string) (batch int, fromModel bool, err error) {
	switch setting {
	case "b4":
		return 4, false, nil
	case "b8":
		return 8, false, nil
	case "b4-model":
		return 4, true, nil
	}
	return 0, false, fmt.Errorf("unknown setting %q", setting)
}

// entitySchema is the table L adds to the production store: which entity
// names each mid's synthesis wrote.
const entitySchema = `CREATE TABLE mid_entity (
 mid INTEGER NOT NULL REFERENCES conversation_mid(id),
 name TEXT NOT NULL,
 PRIMARY KEY (mid, name)
)`

// Build copies the shared store at the setting's batch and writes each
// mid's entities beside it.
func (arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	batch, fromModel, err := parseSetting(setting)
	if err != nil {
		return study.Built{}, err
	}
	shared, err := env.SharedStore(batch)
	if err != nil {
		return study.Built{}, err
	}
	var modelNames map[int64][]string
	if fromModel {
		if modelNames, err = recordEntities(ctx, env, batch); err != nil {
			return study.Built{}, err
		}
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	mids, err := readMids(ctx, path)
	if err != nil {
		return study.Built{}, err
	}
	entities := map[int64][]string{}
	for _, m := range mids {
		if fromModel {
			entities[m.ID] = names(modelNames[m.SourceFirst])
		} else {
			entities[m.ID] = ruleEntities(m.Summary)
		}
	}
	if err := writeEntities(ctx, path, entities); err != nil {
		return study.Built{}, err
	}
	return built(mids, entities, fromModel), nil
}

// built counts what L stores: every mid summary and every entity name per
// mention, one vector per mid.
func built(mids []agent.ConversationHit, entities map[int64][]string, fromModel bool) study.Built {
	chars, links, linked := 0, 0, 0
	distinct := map[string]bool{}
	for _, m := range mids {
		chars += len([]rune(m.Summary))
		if len(entities[m.ID]) > 0 {
			linked++
		}
		for _, name := range entities[m.ID] {
			chars += len([]rune(name))
			links++
			distinct[name] = true
		}
	}
	source := "rule"
	if fromModel {
		source = "model"
	}
	return study.Built{
		StoredChars: chars,
		Vectors:     len(mids),
		Units:       map[string]int{"mid": len(mids), "entity": len(distinct), "link": links},
		Notes: map[string]any{
			"entities_from":       source,
			"mids_with_entities":  linked,
			"share_with_entities": float64(linked) / float64(max(len(mids), 1)),
		},
	}
}

func readMids(ctx context.Context, path string) ([]agent.ConversationHit, error) {
	db, err := sql.Open("sqlite", path)
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

func writeEntities(ctx context.Context, path string, entities map[int64][]string) error {
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
	if _, err := tx.ExecContext(ctx, entitySchema); err != nil {
		return err
	}
	for mid, list := range entities {
		for _, name := range list {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mid_entity(mid,name) VALUES (?,?)`, mid, name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func readEntities(ctx context.Context, path string) (map[int64][]string, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT mid,name FROM mid_entity ORDER BY mid,rowid`)
	if err != nil {
		return nil, fmt.Errorf("read entities: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var mid int64
		var name string
		if err := rows.Scan(&mid, &name); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], name)
	}
	return out, rows.Err()
}

// Open reads the mids and their entities into the graph and opens the
// production store under them.
func (arm) Open(ctx context.Context, env *study.Env, setting, dir string) (study.Memory, error) {
	if _, _, err := parseSetting(setting); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "memory.db")
	mids, err := readMids(ctx, path)
	if err != nil {
		return nil, err
	}
	entities, err := readEntities(ctx, path)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return nil, err
	}
	return &memory{store: store, graph: newGraph(mids, entities)}, nil
}
