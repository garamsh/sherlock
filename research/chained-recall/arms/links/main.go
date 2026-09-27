// Command links is arm L of the #874 chained-recall study: each mid is
// linked to the entities its synthesis names, and recall adds the mids a
// Personalized PageRank walk reaches from the entities the query names.
// See README.md beside it.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

func main() { study.Main(New()) }

// New returns arm L.
func New() study.Arm { return arm{} }

type arm struct{}

func (arm) Name() string { return "L" }

// Settings: entities extracted by rule from the recorded summaries at batch
// 4 and 8, and entities written by the extended synthesis call at batch 4.
// b4-model-a is L+A (#885): b4-model with walk-delivered mids labelled as
// associated and the Abstention instruction. #888's read-time variants of
// b4-model are b4-model-g (L+G: walk mids pass a gate), b4-model-t (L-T:
// the walk only as memory_associate) and b4-model-v (L+V: a linked fact
// only after memory_inspect read its source). #896's structural variants
// are b4-model-ptr (L-ptr: walk mids shown as pointers only),
// b4-model-attr (L-attr: walk mids shown with their subject beside the
// query's entities), b4-model-chk (L+chk: a grounding check the runtime
// runs when recall or search delivered walk mids) and b4-model-key-ptr
// (C-key+L-ptr: L-ptr over C-key's store, arms/context's b4-w8-key in the
// memory-encoding validation study).
func (arm) Settings() []string {
	return []string{"b4", "b8", "b4-model", "b4-model-a", "b4-model-g", "b4-model-t", "b4-model-v",
		"b4-model-ptr", "b4-model-attr", "b4-model-chk", "b4-model-key-ptr"}
}

func (arm) Instructions() string { return "" }

// Abstention is L+A's one contract instruction. Production would carry it
// in the platform contract's §Source-linked memory; the study appends it to
// the ego, as M's instruction was.
const Abstention = "Answer a fact only from a memory or source that states that fact about the thing the task asks about. A memory labelled associated was reached through a named entity, not matched to the question: treat it as a lead to search or inspect, never as the answer itself."

// OnCall is L-T's one contract instruction, pointing the model at the call
// that now carries the walk.
const OnCall = "When the task depends on someone or something a memory names and the answer is not in view, call memory_associate with that name to find the episodes linked to it."

// Verify is L+V's one contract instruction.
const Verify = "A memory marked linked was reached through a name it shares with the task, not by matching the question. Before you state a fact taken from a linked memory, call memory_inspect on the source that states it in this turn, and state only what that source says."

// InstructionsFor gives L+A, L-T and L+V their instruction and every other
// setting none.
func (arm) InstructionsFor(setting string) string {
	switch {
	case associates(setting):
		return Abstention
	case setting == "b4-model-t":
		return OnCall
	case setting == "b4-model-v":
		return Verify
	}
	return ""
}

// associates reports whether a setting is L+A.
func associates(setting string) bool { return setting == "b4-model-a" }

func parseSetting(setting string) (batch int, fromModel bool, err error) {
	switch setting {
	case "b4":
		return 4, false, nil
	case "b8":
		return 8, false, nil
	case "b4-model", "b4-model-a", "b4-model-g", "b4-model-t", "b4-model-v",
		"b4-model-ptr", "b4-model-attr", "b4-model-chk", "b4-model-key-ptr":
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

// keyed is C-key+L-ptr's setting, whose store is C-key's rather than the
// shared one.
const keyed = "b4-model-key-ptr"

// keyStore is C-key's build (arms/context, setting b4-w8-key, in the
// memory-encoding validation study): the shared store with every stream
// mid embedded from C's recorded context before S's summary. C-key+L-ptr
// copies it in place of the shared store, so C-key is built first.
func keyStore(env *study.Env) (string, error) {
	dir := env.BuildDir("C", "b4-w8-key")
	if _, err := os.Stat(filepath.Join(dir, "memory.db")); err != nil {
		return "", fmt.Errorf("store of arms/context setting b4-w8-key: %w (build it first)", err)
	}
	return dir, nil
}

// Build copies the shared store at the setting's batch, or C-key's store
// for C-key+L-ptr, and writes each mid's entities beside it.
func (arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	batch, fromModel, err := parseSetting(setting)
	if err != nil {
		return study.Built{}, err
	}
	var shared string
	if setting == keyed {
		shared, err = keyStore(env)
	} else {
		shared, err = env.SharedStore(batch)
	}
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
	out := built(mids, entities, fromModel)
	if setting == keyed {
		contexts, err := contextChars(ctx, path)
		if err != nil {
			return study.Built{}, err
		}
		out.StoredChars += contexts
		out.Notes["context_chars"] = contexts
	}
	return out, nil
}

// contextChars counts the context characters C-key's store keeps for its
// embedding keys.
func contextChars(ctx context.Context, path string) (int, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRowContext(ctx, `SELECT coalesce(sum(length(context)),0) FROM conversation_mid`).Scan(&n)
	return n, err
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
	m := &memory{store: store, graph: newGraph(mids, entities), associate: associates(setting), gated: setting == "b4-model-g",
		pointer: setting == "b4-model-ptr" || setting == keyed, attribute: setting == "b4-model-attr"}
	switch setting {
	case "b4-model-t":
		return associating{m}, nil
	case "b4-model-chk":
		return openChecked(env, m, setting, dir)
	}
	return m, nil
}
