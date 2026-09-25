package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

const conversationSchema = `
CREATE TABLE IF NOT EXISTS conversation_sources (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 role TEXT NOT NULL CHECK(role IN ('user','assistant')),
 external_id TEXT,
 content TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS conversation_external_id ON conversation_sources(external_id) WHERE external_id <> '';
CREATE TABLE IF NOT EXISTS conversation_mid (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 source_first INTEGER NOT NULL UNIQUE REFERENCES conversation_sources(id),
 source_last INTEGER NOT NULL UNIQUE REFERENCES conversation_sources(id),
 summary TEXT NOT NULL,
 embedding BLOB
);
DROP TRIGGER IF EXISTS conversation_mid_insert;
DROP TABLE IF EXISTS conversation_mid_fts;
`

var ErrSourceNotFound = errors.New("conversation source not found")

// RecordTurn archives completed messages before retryable summary maintenance.
func (s *Store) RecordTurn(ctx context.Context, sources []agent.SourceMessage, summarizer agent.ConversationSummarizer) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin conversation: %v", err)
	}
	defer tx.Rollback()
	matched, identified := 0, 0
	for _, part := range sources {
		if part.ExternalID == "" {
			continue
		}
		identified++
		var role, content string
		err := tx.QueryRowContext(ctx, `SELECT role,content FROM conversation_sources WHERE external_id=?`, part.ExternalID).Scan(&role, &content)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check source retry: %v", err)
		}
		if role != string(part.Role) || content != redactSecrets(part.Content) {
			return errors.New("source ID reused with different content")
		}
		matched++
	}
	if matched > 0 && matched != identified {
		return errors.New("turn overlaps previously committed source IDs; retry the original batch")
	}
	if matched == 0 {
		for _, part := range sources {
			if part.Role != agent.RoleUser && part.Role != agent.RoleAssistant {
				continue
			}
			content := redactSecrets(part.Content)
			if strings.TrimSpace(content) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_sources(role,external_id,content) VALUES (?,?,?)`, part.Role, part.ExternalID, content); err != nil {
				return fmt.Errorf("record source: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sources: %v", err)
	}
	return s.Accumulate(ctx, summarizer)
}

func summarize(ctx context.Context, summarizer agent.ConversationSummarizer, updates []string) (string, error) {
	if summarizer == nil {
		return "", errors.New("conversation requires a summarizer")
	}
	summary, err := summarizer.Summarize(ctx, updates)
	if err != nil {
		return "", fmt.Errorf("synthesize conversation: %v", err)
	}
	summary = strings.TrimSpace(redactSecrets(summary))
	if summary == "" || len([]rune(summary)) > 2000 {
		return "", errors.New("conversation summary must contain 1 to 2000 characters")
	}
	return summary, nil
}

func (s *Store) Recent(ctx context.Context, limit int) ([]agent.SourceMessage, error) {
	// SQLite treats a negative LIMIT as unbounded for disabled compaction.
	if limit == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,external_id,role,content FROM (SELECT id,external_id,role,content FROM conversation_sources ORDER BY id DESC LIMIT ?) ORDER BY id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.SourceMessage
	for rows.Next() {
		var m agent.SourceMessage
		if err := rows.Scan(&m.ID, &m.ExternalID, &m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Search ranks embedded mid episodes by cosine similarity to the query, newest
// first among equals. The bounded limit applies before content enters model context.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]agent.ConversationHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,summary,source_first,source_last,embedding FROM conversation_mid WHERE embedding IS NOT NULL ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("search conversation: %v", err)
	}
	var hits []agent.ConversationHit
	var vectors [][]byte
	for rows.Next() {
		var hit agent.ConversationHit
		var vector []byte
		if err := rows.Scan(&hit.ID, &hit.Summary, &hit.SourceFirst, &hit.SourceLast, &vector); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read conversation candidates: %v", err)
		}
		hits, vectors = append(hits, hit), append(vectors, vector)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read conversation candidates: %v", err)
	}
	if len(hits) == 0 {
		return nil, nil
	}
	target, err := s.embed(ctx, query)
	if err != nil {
		return nil, err
	}
	scores := make(map[int64]float64, len(hits))
	for i, hit := range hits {
		if len(vectors[i]) != 4*len(target) {
			return nil, fmt.Errorf("mid %d embedding has %d dimensions and the query %d: the embedding model changed", hit.ID, len(vectors[i])/4, len(target))
		}
		scores[hit.ID] = cosine(target, vectors[i])
	}
	sort.SliceStable(hits, func(i, j int) bool { return scores[hits[i].ID] > scores[hits[j].ID] })
	return hits[:min(limit, len(hits))], nil
}

func (s *Store) embed(ctx context.Context, text string) ([]float32, error) {
	if s.embedder == nil {
		return nil, errors.New("conversation requires an embedder")
	}
	vector, err := s.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed conversation: %v", err)
	}
	return vector, nil
}

func encodeVector(vector []float32) []byte {
	out := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

// cosine compares a vector with an encoded one of the same length; a zero
// vector is similar to nothing.
func cosine(a []float32, encoded []byte) float64 {
	var dot, na, nb float64
	for i, x := range a {
		y := float64(math.Float32frombits(binary.LittleEndian.Uint32(encoded[4*i:])))
		dot, na, nb = dot+float64(x)*y, na+float64(x)*float64(x), nb+y*y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// RecentMid returns newest episodes independently of lexical relevance.
func (s *Store) RecentMid(ctx context.Context, limit int) ([]agent.ConversationHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,summary,source_first,source_last FROM conversation_mid ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read conversation candidates: %v", err)
	}
	defer rows.Close()
	var out []agent.ConversationHit
	for rows.Next() {
		var m agent.ConversationHit
		if err := rows.Scan(&m.ID, &m.Summary, &m.SourceFirst, &m.SourceLast); err != nil {
			return nil, fmt.Errorf("read conversation candidates: %v", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read conversation candidates: %v", err)
	}
	return out, nil
}

func (s *Store) Source(ctx context.Context, id int64) (agent.SourceMessage, error) {
	var m agent.SourceMessage
	err := s.db.QueryRowContext(ctx, `SELECT id,external_id,role,content FROM conversation_sources WHERE id=?`, id).Scan(&m.ID, &m.ExternalID, &m.Role, &m.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrSourceNotFound
	}
	return m, err
}

// OpenConversation checks existing addressed rows read-only before opening the
// source-linked schema, so refusal leaves the old file unchanged. The embedder
// ranks episodes for Search and embeds each new one.
func OpenConversation(path string, embedder agent.Embedder) (*Store, error) {
	if _, err := os.Stat(path); err == nil {
		old, err := sql.Open("sqlite", path+"?mode=ro")
		if err != nil {
			return nil, err
		}
		defer old.Close()
		for _, table := range []string{"memories_active", "memories_archive"} {
			var exists int
			if err := old.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
				return nil, err
			}
			if exists == 0 {
				continue
			}
			var count int
			if err := old.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, fmt.Errorf("%q: %w", path, ErrAddressedStore)
			}
		}
		var oldSchema, populated int
		if err := old.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_short') WHERE name='source_id'`).Scan(&oldSchema); err != nil {
			return nil, fmt.Errorf("inspect conversation schema: %v", err)
		}
		if oldSchema > 0 {
			if err := old.QueryRow(`SELECT count(*) FROM conversation_sources`).Scan(&populated); err != nil {
				return nil, fmt.Errorf("inspect conversation sources: %v", err)
			}
			if populated > 0 {
				return nil, fmt.Errorf("%q: extractive conversation schema; preserve this database and its WAL, use the previous binary to inspect it, and select a new memory-path for episodic memory (docs/architecture/memory.md, Existing databases)", path)
			}
		}

	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	store, err := Open(path)
	if err != nil {
		return nil, err
	}
	store.embedder = embedder
	return store, nil
}

var secretLabel = regexp.MustCompile(`(?i)(password|secret|api[_-]?key|token)\s*(?:[:=]|is)\s*\S+`)
var bearerToken = regexp.MustCompile(`(?i)bearer\s+\S+`)
var apiToken = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)

func redactSecrets(text string) string {
	text = secretLabel.ReplaceAllString(text, "[redacted credential]")
	text = bearerToken.ReplaceAllString(text, "[redacted credential]")
	return apiToken.ReplaceAllString(text, "[redacted credential]")
}

func prepareConversation(db *sql.DB) error {
	var old int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_short') WHERE name='source_id'`).Scan(&old); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if old > 0 {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM conversation_sources`).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errors.New("extractive conversation schema; open with the previous binary or select a new memory-path; see docs/architecture/memory.md")
		}
		if _, err := tx.Exec(`DROP TRIGGER IF EXISTS conversation_mid_insert; DROP TABLE IF EXISTS conversation_mid_fts; DROP TABLE conversation_mid; DROP TABLE conversation_short`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(conversationSchema); err != nil {
		return err
	}
	var embedded int
	if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_mid') WHERE name='embedding'`).Scan(&embedded); err != nil {
		return err
	}
	// A file from before embeddings gains the column; Accumulate fills it.
	if embedded == 0 {
		if _, err := tx.Exec(`ALTER TABLE conversation_mid ADD COLUMN embedding BLOB`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
