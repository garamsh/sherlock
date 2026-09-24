package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/garamsh/gagent/internal/agent"
)

const conversationSchema = `
CREATE TABLE IF NOT EXISTS conversation_sources (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 role TEXT NOT NULL CHECK(role IN ('user','assistant')),
 external_id TEXT,
 content TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_short (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 source_id INTEGER NOT NULL UNIQUE REFERENCES conversation_sources(id),
 summary TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_mid (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 short_id INTEGER NOT NULL UNIQUE REFERENCES conversation_short(id),
 summary TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS conversation_mid_fts USING fts5(summary, content='conversation_mid', content_rowid='id', tokenize='porter unicode61');
CREATE TRIGGER IF NOT EXISTS conversation_mid_insert AFTER INSERT ON conversation_mid BEGIN
 INSERT INTO conversation_mid_fts(rowid, summary) VALUES (new.id, new.summary);
END;
`

var ErrSourceNotFound = errors.New("conversation source not found")

// RecordTurn commits source messages and their two linked summaries together.
// A failed transaction cannot leave a processed source or an advanced cursor.
func (s *Store) RecordTurn(ctx context.Context, sources []agent.SourceMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin conversation: %w", err)
	}
	defer tx.Rollback()
	for _, part := range sources {
		if part.Role != agent.RoleUser && part.Role != agent.RoleAssistant {
			continue
		}
		content := redactSecrets(part.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO conversation_sources(role,external_id,content) VALUES (?,?,?)`, part.Role, part.ExternalID, content)
		if err != nil {
			return fmt.Errorf("record source: %w", err)
		}
		sourceID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		summary := boundedSummary(content, 400)
		result, err = tx.ExecContext(ctx, `INSERT INTO conversation_short(source_id,summary) VALUES (?,?)`, sourceID, summary)
		if err != nil {
			return fmt.Errorf("record short summary: %w", err)
		}
		shortID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO conversation_mid(short_id,summary) VALUES (?,?)`, shortID, boundedSummary(summary, 220))
		if err != nil {
			return fmt.Errorf("record mid memory: %w", err)
		}
	}
	return tx.Commit()
}

func boundedSummary(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
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

// Search matches words supplied by the read step and orders recent matches
// first. The bounded limit applies before content enters model context.
func (s *Store) Search(ctx context.Context, terms []string, limit int) ([]agent.ConversationHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	var tokens []string
	seen := map[string]bool{}
	for _, term := range terms {
		for _, word := range strings.FieldsFunc(strings.ToLower(term), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			if len([]rune(word)) < 2 || seen[word] || searchStopWords[word] {
				continue
			}
			seen[word] = true
			tokens = append(tokens, `"`+word+`"`)
		}
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.summary,m.short_id FROM conversation_mid_fts f JOIN conversation_mid m ON m.id=f.rowid WHERE conversation_mid_fts MATCH ? ORDER BY m.id DESC, bm25(conversation_mid_fts) LIMIT ?`, strings.Join(tokens, " OR "), limit)
	if err != nil {
		return nil, fmt.Errorf("search conversation: %w", err)
	}
	defer rows.Close()
	var out []agent.ConversationHit
	for rows.Next() {
		var hit agent.ConversationHit
		if err := rows.Scan(&hit.ID, &hit.Summary, &hit.ShortID); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

func (s *Store) Short(ctx context.Context, id int64) (agent.ShortMemory, error) {
	var m agent.ShortMemory
	err := s.db.QueryRowContext(ctx, `SELECT id,summary,source_id FROM conversation_short WHERE id=?`, id).Scan(&m.ID, &m.Summary, &m.SourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrSourceNotFound
	}
	return m, err
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

var searchStopWords = map[string]bool{"the": true, "and": true, "for": true, "was": true, "what": true, "when": true, "where": true, "who": true, "which": true, "this": true, "that": true, "with": true, "from": true, "about": true, "does": true, "did": true, "have": true, "has": true, "are": true, "you": true, "your": true, "user": true, "assistant": true}
