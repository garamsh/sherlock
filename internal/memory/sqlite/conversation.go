package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/garamsh/gagent/internal/agent"
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
 summary TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS conversation_mid_fts USING fts5(summary, content='conversation_mid', content_rowid='id', tokenize='porter unicode61');
CREATE TRIGGER IF NOT EXISTS conversation_mid_insert AFTER INSERT ON conversation_mid BEGIN
 INSERT INTO conversation_mid_fts(rowid, summary) VALUES (new.id, new.summary);
END;
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

// Search ranks matching words by relevance, using recency to break ties.
// The bounded limit applies before content enters model context.
func (s *Store) Search(ctx context.Context, terms []string, limit int) ([]agent.ConversationHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	tokens := agent.SearchTokens(terms)
	if len(tokens) == 0 {
		return nil, nil
	}
	for i, token := range tokens {
		tokens[i] = `"` + token + `"`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.summary,m.source_first,m.source_last FROM conversation_mid_fts f JOIN conversation_mid m ON m.id=f.rowid WHERE conversation_mid_fts MATCH ? ORDER BY bm25(conversation_mid_fts), m.id DESC LIMIT ?`, strings.Join(tokens, " OR "), limit)
	if err != nil {
		return nil, fmt.Errorf("search conversation: %v", err)
	}
	defer rows.Close()
	var out []agent.ConversationHit
	for rows.Next() {
		var hit agent.ConversationHit
		if err := rows.Scan(&hit.ID, &hit.Summary, &hit.SourceFirst, &hit.SourceLast); err != nil {
			return nil, fmt.Errorf("read conversation candidates: %v", err)
		}
		out = append(out, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read conversation candidates: %v", err)
	}
	return out, nil
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
// source-linked schema, so refusal leaves the old file unchanged.
func OpenConversation(path string) (*Store, error) {
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
	return Open(path)
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
	_, err = tx.Exec(conversationSchema)
	if err != nil {
		return err
	}
	return tx.Commit()
}
