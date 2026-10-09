package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
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
 content TEXT NOT NULL,
 sender TEXT NOT NULL DEFAULT '',
 issuer TEXT NOT NULL DEFAULT '',
 poster TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS conversation_mid (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 source_first INTEGER NOT NULL UNIQUE REFERENCES conversation_sources(id),
 source_last INTEGER NOT NULL UNIQUE REFERENCES conversation_sources(id),
 summary TEXT NOT NULL,
 embedding BLOB,
 linked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS conversation_mid_entity (
 mid INTEGER NOT NULL REFERENCES conversation_mid(id),
 position INTEGER NOT NULL,
 name TEXT NOT NULL,
 PRIMARY KEY (mid, position)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS conversation_note (
 id INTEGER PRIMARY KEY CHECK(id = 1),
 content TEXT NOT NULL
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
		err := tx.QueryRowContext(ctx, `SELECT role,content FROM conversation_sources WHERE issuer=? AND sender=? AND external_id=?`, part.Issuer, part.Sender, part.ExternalID).Scan(&role, &content)
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
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_sources(role,issuer,sender,poster,external_id,content) VALUES (?,?,?,?,?,?)`, part.Role, part.Issuer, part.Sender, part.Poster, part.ExternalID, content); err != nil {
				return fmt.Errorf("record source: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sources: %v", err)
	}
	return s.Accumulate(ctx, summarizer)
}

// synthesize asks the summarizer for one episode over the sources, each
// sent as "role: content", and bounds its redacted summary.
func synthesize(ctx context.Context, summarizer agent.ConversationSummarizer, sources []agent.SourceMessage) (agent.Synthesis, error) {
	if summarizer == nil {
		return agent.Synthesis{}, errors.New("conversation requires a summarizer")
	}
	updates := make([]string, 0, len(sources))
	for _, source := range sources {
		updates = append(updates, string(source.Role)+": "+source.Content)
	}
	synthesis, err := summarizer.Summarize(ctx, updates)
	if err != nil {
		return agent.Synthesis{}, fmt.Errorf("synthesize conversation: %v", err)
	}
	synthesis.Summary = strings.TrimSpace(redactSecrets(synthesis.Summary))
	if synthesis.Summary == "" || len([]rune(synthesis.Summary)) > 2000 {
		return agent.Synthesis{}, errors.New("conversation summary must contain 1 to 2000 characters")
	}
	return synthesis, nil
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
	defer rows.Close()
	var hits []agent.ConversationHit
	var vectors [][]byte
	for rows.Next() {
		var hit agent.ConversationHit
		var vector []byte
		if err := rows.Scan(&hit.ID, &hit.Summary, &hit.SourceFirst, &hit.SourceLast, &vector); err != nil {
			return nil, fmt.Errorf("read conversation candidates: %v", err)
		}
		hits, vectors = append(hits, hit), append(vectors, vector)
	}
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

// RecentMid returns newest episodes independently of similarity.
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

// Note returns the agent's note, empty when none has been saved.
func (s *Store) Note(ctx context.Context) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM conversation_note WHERE id=1`).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read note: %v", err)
	}
	return content, nil
}

// SetNote replaces the agent's note. Credentials are redacted before it is
// saved, as they are from sources.
func (s *Store) SetNote(ctx context.Context, content string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO conversation_note(id,content) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET content=excluded.content`, redactSecrets(content)); err != nil {
		return fmt.Errorf("save note: %v", err)
	}
	return nil
}

func (s *Store) Source(ctx context.Context, id int64) (agent.SourceMessage, error) {
	var m agent.SourceMessage
	err := s.db.QueryRowContext(ctx, `SELECT id,external_id,role,content FROM conversation_sources WHERE id=?`, id).Scan(&m.ID, &m.ExternalID, &m.Role, &m.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrSourceNotFound
	}
	return m, err
}

// HoldsInbound reports whether a committed source holds the message ID
// within its scope — the verified issuer and subject of a signed message,
// or the empty issuer and transport principal of an unsigned one — and,
// when it does, whether it holds it with content, compared as stored:
// credentials redacted.
func (s *Store) HoldsInbound(ctx context.Context, issuer, sender, id, content string) (held, same bool, err error) {
	var stored string
	err = s.db.QueryRowContext(ctx, `SELECT content FROM conversation_sources WHERE issuer=? AND sender=? AND external_id=?`, issuer, sender, id).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("read inbound source: %v", err)
	}
	return true, stored == redactSecrets(content), nil
}

// PostedInbound reports whether a committed source holds a message the
// transport principal poster posted under id: an unsigned message is its
// own sender's, and a signed one carries its poster (ADR 0026). A row from
// before signed senders has no poster and is unsigned, so its sender
// answers for it.
func (s *Store) PostedInbound(ctx context.Context, poster, id string) (bool, error) {
	var held int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_sources
WHERE external_id=? AND ((issuer='' AND sender=?) OR (issuer<>'' AND poster=?))`, id, poster, poster).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("read posted source: %v", err)
	}
	return held > 0, nil
}

// OpenConversation takes the writer lock, then checks existing addressed rows
// read-only before opening the source-linked schema, so refusal leaves the old
// file unchanged. The embedder
// ranks episodes for Search and embeds each new one.
func OpenConversation(path string, embedder agent.Embedder) (*Store, error) {
	return openConversation(path, embedder, nil)
}

// OpenInitializedConversation is OpenConversation for a store that must
// already exist: under the writer lock, and before anything migrates or
// catches up, it returns ErrStoreUninitialized for a memory.db that is
// absent, empty or holds no Sherlock schema, creating and changing
// nothing (ADR 0025).
func OpenInitializedConversation(path string, embedder agent.Embedder) (*Store, error) {
	// The lock file lives beside the store, so a store whose directory is
	// absent is absent too, and no lock can be taken for it.
	if _, err := os.Stat(filepath.Dir(path)); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%q is absent: %w", path, ErrStoreUninitialized)
	}
	return openConversation(path, embedder, requireInitialized)
}

// ErrStoreUninitialized reports a memory.db that is absent, empty or
// holds no Sherlock schema, which OpenInitializedConversation refuses.
var ErrStoreUninitialized = errors.New("memory store is not initialized")

// requireInitialized returns ErrStoreUninitialized unless the file at path
// holds a Sherlock schema. It reads the file only through a read-only open.
func requireInitialized(path string) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%q is absent: %w", path, ErrStoreUninitialized)
	} else if err != nil {
		return fmt.Errorf("inspect %q: %v", path, err)
	}
	// Only a file: URI keeps mode=ro; the driver drops the query of a
	// plain path and opens it read-write.
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro")
	if err != nil {
		return fmt.Errorf("inspect %q: %v", path, err)
	}
	defer db.Close()
	// A store an addressed-only binary created carries only the addressed
	// table; every later one carries the source archive.
	var tables int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'
		AND name IN ('memories_active', 'conversation_sources')`).Scan(&tables)
	if err != nil {
		return fmt.Errorf("inspect %q: %v", path, err)
	}
	// An empty file reads as a database holding no table.
	if tables == 0 {
		return fmt.Errorf("%q holds no Sherlock schema: %w", path, ErrStoreUninitialized)
	}
	return nil
}

// openConversation is OpenConversation with check, when set, run on the
// path once the writer lock is held and before the file is otherwise read.
func openConversation(path string, embedder agent.Embedder, check func(path string) error) (store *Store, err error) {
	// The lock precedes the read-only inspection below, which is an open
	// of the file too.
	lock, err := lockWriter(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			lock.Close()
		}
	}()
	if check != nil {
		if err := check(path); err != nil {
			return nil, err
		}
	}
	if err := restrictStore(path); err != nil {
		return nil, err
	}
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
	store, err = open(path, lock)
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
	_, err = tx.Exec(conversationSchema)
	if err != nil {
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
	var linked int
	if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_mid') WHERE name='linked'`).Scan(&linked); err != nil {
		return err
	}
	// A file from before entities gains the column unset; Link fills it.
	if linked == 0 {
		if _, err := tx.Exec(`ALTER TABLE conversation_mid ADD COLUMN linked INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	var scoped int
	if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_sources') WHERE name='sender'`).Scan(&scoped); err != nil {
		return err
	}
	// A file from before sender scoping files its IDs under the empty
	// sender, the one a plain-HTTP request has.
	if scoped == 0 {
		if _, err := tx.Exec(`ALTER TABLE conversation_sources ADD COLUMN sender TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// A file from before signed senders (ADR 0026) gains the issuer and
	// the poster empty, so every existing row reads as a transport
	// principal's; no row is rewritten.
	for _, column := range []string{"issuer", "poster"} {
		var present int
		if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('conversation_sources') WHERE name=?`, column).Scan(&present); err != nil {
			return err
		}
		if present == 0 {
			if _, err := tx.Exec(`ALTER TABLE conversation_sources ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS conversation_external_id;
DROP INDEX IF EXISTS conversation_sender_external_id;
CREATE UNIQUE INDEX IF NOT EXISTS conversation_scope_external_id ON conversation_sources(issuer,sender,external_id) WHERE external_id <> ''`); err != nil {
		return err
	}
	return tx.Commit()
}
