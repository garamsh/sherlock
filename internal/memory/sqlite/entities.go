package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

// word is one token: letters and digits, joined inside by hyphens or
// apostrophes ("REF-FEPE732", "don't").
var word = regexp.MustCompile(`[\p{L}\p{N}]+(?:[-'’][\p{L}\p{N}]+)*`)

// stopwords never begin an entity name, and a name made of them alone
// ("The", "User") is none.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the this that these those every each all any some both no not
		our their his her its my your we they he she it i you me us them
		user assistant what when where which who whom whose why how
		and or but if then so also as at by for from in into of on onto to under with without
		is are was were be been has have had do does did will would can could should may might must
		please thanks thank yes ok okay got noted understood kept sure great good hi hello here there`) {
		stopwords[w] = true
	}
}

// tokens lowercases text into words, dropping a trailing possessive.
func tokens(text string) []string {
	var out []string
	for _, w := range word.FindAllString(text, -1) {
		out = append(out, stripPossessive(strings.ToLower(w)))
	}
	return out
}

func stripPossessive(w string) string {
	for _, suffix := range []string{"'s", "’s"} {
		if strings.HasSuffix(w, suffix) && len(w) > len(suffix) {
			return strings.TrimSuffix(w, suffix)
		}
	}
	return w
}

// entityNames is the key each entity is stored and matched under: its
// words, lowercased, without a possessive or leading stopwords. Names that
// hold no other word, and repeats, are dropped; the order is kept, because
// the first name is whom the episode is about.
func entityNames(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range raw {
		words := tokens(redactSecrets(name))
		for len(words) > 0 && stopwords[words[0]] {
			words = words[1:]
		}
		key := strings.Join(words, " ")
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// linkEntities stores a mid's entity names and marks it linked, so a mid
// whose synthesis named none is not asked again.
func linkEntities(ctx context.Context, tx *sql.Tx, mid int64, raw []string) error {
	for position, name := range entityNames(raw) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_mid_entity(mid,position,name) VALUES (?,?,?)`, mid, position, name); err != nil {
			return fmt.Errorf("link episode entities: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_mid SET linked=1 WHERE id=?`, mid); err != nil {
		return fmt.Errorf("link episode entities: %v", err)
	}
	return nil
}

// Link names the entities of the oldest mid episode stored without them,
// one written before entities were: it sends the episode's sources through
// the synthesis call new batches take and keeps the stored summary. A store
// with no such episode makes no call.
func (s *Store) Link(ctx context.Context, summarizer agent.ConversationSummarizer) error {
	var mid, first, last int64
	err := s.db.QueryRowContext(ctx, `SELECT id,source_first,source_last FROM conversation_mid WHERE linked=0 ORDER BY id LIMIT 1`).Scan(&mid, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read unlinked episode: %v", err)
	}
	sources, err := s.sourcesBetween(ctx, first, last)
	if err != nil {
		return err
	}
	synthesis, err := synthesize(ctx, summarizer, sources)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin episode link: %v", err)
	}
	defer tx.Rollback()
	var linked int
	if err := tx.QueryRowContext(ctx, `SELECT linked FROM conversation_mid WHERE id=?`, mid).Scan(&linked); err != nil {
		return fmt.Errorf("read unlinked episode: %v", err)
	}
	// A concurrent pass may have linked it while the model answered.
	if linked == 1 {
		return nil
	}
	if err := linkEntities(ctx, tx, mid, synthesis.Entities); err != nil {
		return err
	}
	return tx.Commit()
}

// midEntities returns every linked mid with the names its synthesis wrote,
// in mid order and each mid's names in the order written.
func (s *Store) midEntities(ctx context.Context) ([]agent.ConversationHit, map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.summary,m.source_first,m.source_last,e.name FROM conversation_mid m JOIN conversation_mid_entity e ON e.mid=m.id ORDER BY m.id,e.position`)
	if err != nil {
		return nil, nil, fmt.Errorf("read episode entities: %v", err)
	}
	defer rows.Close()
	var mids []agent.ConversationHit
	names := map[int64][]string{}
	for rows.Next() {
		var mid agent.ConversationHit
		var name string
		if err := rows.Scan(&mid.ID, &mid.Summary, &mid.SourceFirst, &mid.SourceLast, &name); err != nil {
			return nil, nil, fmt.Errorf("read episode entities: %v", err)
		}
		if len(mids) == 0 || mids[len(mids)-1].ID != mid.ID {
			mids = append(mids, mid)
		}
		names[mid.ID] = append(names[mid.ID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read episode entities: %v", err)
	}
	return mids, names, nil
}
