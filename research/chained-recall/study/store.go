package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// Synthesis is one recorded batch summary: the batch's inclusive source
// interval and the summary the session model returned for it.
type Synthesis struct {
	First   int64  `json:"first"`
	Last    int64  `json:"last"`
	Summary string `json:"summary"`
}

// Syntheses returns the recorded summary of every batch of the corpus at
// batch size n, in source order: the replication's recorded syntheses for
// the base (evidence/replication/synthesis-n<n>.jsonl) and the study's for
// the stream (evidence/synthesis/stream-b<n>.jsonl).
func (e *Env) Syntheses(n int) ([]Synthesis, error) {
	type recorded struct {
		Batch   int    `json:"batch"`
		Summary string `json:"summary"`
	}
	base, err := ReadJSONL[recorded](filepath.Join(ResearchDir(e.Dir), "mid-memory-rag", "evidence", "replication", fmt.Sprintf("synthesis-n%d.jsonl", n)))
	if err != nil {
		return nil, err
	}
	out := make([]Synthesis, len(base))
	for _, r := range base {
		if r.Batch < 1 || r.Batch > len(base) {
			return nil, fmt.Errorf("replication batch %d out of range", r.Batch)
		}
		out[r.Batch-1] = Synthesis{First: int64(r.Batch-1)*int64(n) + 1, Last: int64(r.Batch) * int64(n), Summary: r.Summary}
	}
	stream, err := ReadJSONL[Synthesis](filepath.Join(e.Dir, "evidence", "synthesis", fmt.Sprintf("stream-b%d.jsonl", n)))
	if err != nil {
		return nil, err
	}
	sort.Slice(stream, func(i, j int) bool { return stream[i].First < stream[j].First })
	out = append(out, stream...)
	total := int64(len(e.Corpus.Messages()))
	for i, s := range out {
		if s.First != int64(i)*int64(n)+1 || s.Last != s.First+int64(n)-1 {
			return nil, fmt.Errorf("synthesis %d covers %d-%d, not batch %d of %d", i+1, s.First, s.Last, i+1, n)
		}
	}
	if int64(len(out))*int64(n) != total {
		return nil, fmt.Errorf("%d syntheses of %d cover %d sources; the corpus has %d", len(out), n, len(out)*n, total)
	}
	return out, nil
}

// replaySummarizer answers the production store's synthesis requests, in
// order, with recorded summaries, checking each request is the batch it
// answers.
type replaySummarizer struct {
	syntheses []Synthesis
	next      int
}

func (r *replaySummarizer) Summarize(_ context.Context, updates []string) (string, error) {
	if r.next >= len(r.syntheses) {
		return "", errors.New("no recorded synthesis left")
	}
	s := r.syntheses[r.next]
	if int64(len(updates)) != s.Last-s.First+1 {
		return "", fmt.Errorf("batch %d: the store sent %d messages, the recording covers %d", r.next+1, len(updates), s.Last-s.First+1)
	}
	r.next++
	return s.Summary, nil
}

// BuildShared writes the shared store at batch n into dir. At batch 4 every
// message goes through the production store (RecordTurn, one message per
// turn), which batches, redacts, bounds, stores and embeds each recorded
// summary itself. The production store only batches by four, so at any
// other n the sources and mids are written directly, with the production
// redaction and bounds applied here, and the production store then embeds
// every mid (Accumulate). Either way Search and RecentMid are production
// code over the result.
func BuildShared(ctx context.Context, env *Env, n int, dir string) error {
	syntheses, err := env.Syntheses(n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "memory.db")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists", path)
	}
	messages := env.Corpus.Messages()
	if n == 4 {
		store, err := sqlite.OpenConversation(path, env.Embedder)
		if err != nil {
			return err
		}
		replay := &replaySummarizer{syntheses: syntheses}
		for _, m := range messages {
			if err := store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.Role(m.Role), Content: m.Content}}, replay); err != nil {
				store.Close()
				return fmt.Errorf("record source %d: %w", m.ID, err)
			}
		}
		if err := store.Close(); err != nil {
			return err
		}
		if replay.next != len(syntheses) {
			return fmt.Errorf("the store asked for %d syntheses; %d are recorded", replay.next, len(syntheses))
		}
		return verifyShared(path, syntheses)
	}
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return err
	}
	if err := store.Close(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return err
	}
	for _, m := range messages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_sources(id,role,external_id,content) VALUES (?,?,'',?)`, m.ID, m.Role, Redact(m.Content)); err != nil {
			tx.Rollback()
			db.Close()
			return err
		}
	}
	for _, s := range syntheses {
		summary := strings.TrimSpace(Redact(s.Summary))
		if summary == "" || len([]rune(summary)) > 2000 {
			tx.Rollback()
			db.Close()
			return fmt.Errorf("batch %d-%d: summary must hold 1 to 2000 characters", s.First, s.Last)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_mid(source_first,source_last,summary) VALUES (?,?,?)`, s.First, s.Last, summary); err != nil {
			tx.Rollback()
			db.Close()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	store, err = sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return err
	}
	// No full batch of four is pending, so Accumulate only embeds.
	if err := store.Accumulate(ctx, nil); err != nil {
		store.Close()
		return err
	}
	if err := store.Close(); err != nil {
		return err
	}
	return verifyShared(path, syntheses)
}

// verifyShared checks that the store holds one embedded mid per recorded
// batch, over exactly its interval.
func verifyShared(path string, syntheses []Synthesis) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT source_first, source_last, embedding IS NOT NULL FROM conversation_mid ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var first, last int64
		var embedded bool
		if err := rows.Scan(&first, &last, &embedded); err != nil {
			return err
		}
		if i >= len(syntheses) || first != syntheses[i].First || last != syntheses[i].Last || !embedded {
			return fmt.Errorf("mid %d covers %d-%d (embedded %v), not recorded batch %d", i+1, first, last, embedded, i+1)
		}
		i++
	}
	if i != len(syntheses) {
		return fmt.Errorf("%d mids for %d recorded batches", i, len(syntheses))
	}
	return rows.Err()
}

// MidStats counts a store's mids and the characters of their summaries.
func MidStats(path string) (mids, chars int, err error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT summary FROM conversation_mid`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return 0, 0, err
		}
		mids++
		chars += len([]rune(s))
	}
	return mids, chars, rows.Err()
}

// The production store's credential patterns (internal/memory/sqlite
// redactSecrets), copied because they are unexported; a test holds the copy
// to the production store's behaviour.
var (
	secretLabel = regexp.MustCompile(`(?i)(password|secret|api[_-]?key|token)\s*(?:[:=]|is)\s*\S+`)
	bearerToken = regexp.MustCompile(`(?i)bearer\s+\S+`)
	apiToken    = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
)

// Redact applies the production store's credential redaction.
func Redact(text string) string {
	text = secretLabel.ReplaceAllString(text, "[redacted credential]")
	text = bearerToken.ReplaceAllString(text, "[redacted credential]")
	return apiToken.ReplaceAllString(text, "[redacted credential]")
}
