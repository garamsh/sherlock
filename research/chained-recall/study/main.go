package study

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/garamsh/sherlock/internal/agent/openai"
)

// OracleTop is how many items an arm's Search may return for a hop's own
// statement for the hop to count as reachable (the oracle control).
const OracleTop = 5

// Main runs an arm's command:
//
//	<arm> build   -work <dir> [-setting b4] [-proxy URL]   # Build every setting, or one
//	<arm> offline -work <dir> [-setting b4]                # record what each setting delivers
//	<arm> live -work <dir> -setting b4 -proxy URL          # one live turn per probe
//
// Every command reads corpus.json from -dir, or from -corpus when the
// corpus is another study's (#900 reads #890's), embeds with the TEI server at
// -embed-url, and writes under -dir/evidence/offline/<arm>/<setting>/:
// built.json from build, probes.jsonl and oracle.jsonl from offline.
func Main(arm Arm) {
	if err := run(context.Background(), arm, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, arm Arm, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: <arm> build|offline|live [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
	dir := fs.String("dir", "research/chained-recall", "study directory")
	corpusDir := fs.String("corpus", "", "directory holding corpus.json, when another study's; empty reads -dir")
	work := fs.String("work", "", "scratch root outside the repository")
	proxy := fs.String("proxy", "", "study proxy base URL, for arms that call the model")
	embedURL := fs.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL (local TEI)")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model")
	setting := fs.String("setting", "", "one setting; empty runs every setting")
	workers := fs.Int("workers", 2, "live probes at once")
	onlyList := fs.String("only", "", "comma-separated live probe IDs; empty runs all")
	_ = fs.Parse(args[1:])
	if *work == "" {
		return errors.New("-work is required")
	}
	if *corpusDir == "" {
		*corpusDir = *dir
	}
	corpus, err := LoadCorpus(*corpusDir)
	if err != nil {
		return err
	}
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: *embedURL, Model: *embedModel})
	if err != nil {
		return err
	}
	env := &Env{Corpus: corpus, Embedder: embedder, Dir: *dir, Work: *work, Proxy: *proxy, Arm: arm.Name()}
	settings := arm.Settings()
	if *setting != "" {
		if !slices.Contains(settings, *setting) {
			return fmt.Errorf("arm %s has no setting %q (it has %v)", arm.Name(), *setting, settings)
		}
		settings = []string{*setting}
	}
	for _, s := range settings {
		var err error
		switch args[0] {
		case "build":
			err = build(ctx, env, arm, s)
		case "offline":
			err = Offline(ctx, env, arm, s)
		case "live":
			if env.Proxy == "" {
				return errNoProxy
			}
			only := map[string]bool{}
			for _, id := range strings.Split(*onlyList, ",") {
				if id = strings.TrimSpace(id); id != "" {
					only[id] = true
				}
			}
			err = Live(ctx, env, arm, s, *workers, only)
		default:
			err = fmt.Errorf("unknown command %q", args[0])
		}
		if err != nil {
			return fmt.Errorf("%s %s: %w", arm.Name(), s, err)
		}
	}
	return nil
}

// BuildDir is where an arm's build for a setting lives.
func (e *Env) BuildDir(arm, setting string) string {
	return filepath.Join(e.Work, "builds", arm, setting)
}

// EvidenceDir is where an arm's offline evidence for a setting lives.
func (e *Env) EvidenceDir(arm, setting string) string {
	return filepath.Join(e.Dir, "evidence", "offline", arm, setting)
}

func build(ctx context.Context, env *Env, arm Arm, setting string) error {
	dir := env.BuildDir(arm.Name(), setting)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s exists; remove it to rebuild", dir)
	}
	started := time.Now()
	built, err := arm.Build(ctx, env, setting, dir)
	if err != nil {
		return err
	}
	source := env.Corpus.SourceChars()
	return writeJSON(filepath.Join(env.EvidenceDir(arm.Name(), setting), "built.json"), map[string]any{
		"arm": arm.Name(), "setting": setting, "built": built,
		"source_chars":            source,
		"stored_chars_per_source": float64(built.StoredChars) / float64(source),
		"build_seconds":           time.Since(started).Seconds(),
	})
}

// Probe is one offline record: what a memory delivered for one query.
type Probe struct {
	Probe        string         `json:"probe"`
	Family       string         `json:"family"`
	Hop          int            `json:"hop,omitempty"`
	Arm          string         `json:"arm"`
	Setting      string         `json:"setting"`
	Items        []Item         `json:"items"`
	ContextChars int            `json:"context_chars"`
	Trace        map[string]any `json:"trace,omitempty"`
	Seconds      float64        `json:"seconds"`
}

// Offline records, for one setting, what the arm's Recall delivers for
// every chain query and guard query (probes.jsonl), and what its Search
// returns, at most OracleTop, for each hop's own statement (oracle.jsonl).
func Offline(ctx context.Context, env *Env, arm Arm, setting string) error {
	memory, err := arm.Open(ctx, env, setting, env.BuildDir(arm.Name(), setting))
	if err != nil {
		return err
	}
	defer memory.Close()
	out := env.EvidenceDir(arm.Name(), setting)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	probes, err := os.Create(filepath.Join(out, "probes.jsonl"))
	if err != nil {
		return err
	}
	defer probes.Close()
	oracle, err := os.Create(filepath.Join(out, "oracle.jsonl"))
	if err != nil {
		return err
	}
	defer oracle.Close()
	record := func(w io.Writer, p Probe, call func() ([]Item, error)) error {
		started := time.Now()
		items, err := call()
		if err != nil {
			return fmt.Errorf("%s: %w", p.Probe, err)
		}
		p.Arm, p.Setting, p.Items, p.ContextChars = arm.Name(), setting, items, ContextChars(items)
		p.Seconds = time.Since(started).Seconds()
		if t, ok := memory.(Tracer); ok {
			p.Trace = t.LastTrace()
		}
		return writeLine(w, p)
	}
	for _, c := range env.Corpus.Chains {
		if err := record(probes, Probe{Probe: c.ID, Family: c.Family}, func() ([]Item, error) { return memory.Recall(ctx, c.Query) }); err != nil {
			return err
		}
		for i, h := range c.Hops {
			if err := record(oracle, Probe{Probe: c.ID, Family: c.Family, Hop: i + 1}, func() ([]Item, error) { return memory.Search(ctx, h.Statement, OracleTop) }); err != nil {
				return err
			}
		}
	}
	for _, g := range env.Corpus.Guards {
		if err := record(probes, Probe{Probe: g.ID, Family: g.Family}, func() ([]Item, error) { return memory.Recall(ctx, g.Query) }); err != nil {
			return err
		}
	}
	return nil
}

func writeLine(w io.Writer, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
