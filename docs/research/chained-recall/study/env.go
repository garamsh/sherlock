package study

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/openai"
)

// Env is what the study hands an arm.
type Env struct {
	Corpus *Corpus
	// Embedder is the local text-embeddings-inference server serving
	// BAAI/bge-base-en-v1.5; the only embedder any arm uses.
	Embedder agent.Embedder
	// Dir is the study directory: docs/research/chained-recall, or a
	// corpus beneath it such as #885's confirm/.
	Dir string
	// Work is the study's scratch root, outside the repository: shared
	// stores under shared/, arm builds under builds/<arm>/<setting>/.
	Work string
	// Proxy is the study proxy's base URL (harness proxy); every model
	// call goes through it.
	Proxy string
	// Arm is the name of the arm the runner was started for.
	Arm string
}

// Model is the session model MiniMax-M2 through the study proxy, which
// ledgers every call under this arm, the episode and the phase. An arm
// names its paid phases by phase ("synth-g4", "entities") so the report
// can state tokens per phase.
func (e *Env) Model(episode, phase string) (agent.Model, error) {
	if e.Proxy == "" {
		return nil, fmt.Errorf("no -proxy given; model calls go through the study proxy")
	}
	return openai.New(openai.Config{
		BaseURL: fmt.Sprintf("%s/%s/%s/%s/v1", strings.TrimSuffix(e.Proxy, "/"), e.Arm, episode, phase),
		Model:   "MiniMax-M2", APIKey: "study", Timeout: 5 * time.Minute,
	})
}

// SharedStore is the directory of the shared store at a batch size: the
// base and the stream recorded through the production store, each batch's
// summary from the recorded syntheses, embedded by Embedder. It holds
// memory.db. Batch 4 and 8 exist; build them with harness build. An arm
// copies the directory before writing (CopyDir).
func (e *Env) SharedStore(batch int) (string, error) {
	dir := filepath.Join(e.Work, "shared", fmt.Sprintf("b%d", batch))
	if _, err := os.Stat(filepath.Join(dir, "memory.db")); err != nil {
		return "", fmt.Errorf("shared store at batch %d: %w (build it with harness build -batch %d)", batch, err, batch)
	}
	return dir, nil
}

// ResearchDir is docs/research as a study directory reaches it: the
// nearest ancestor of dir that holds the replication (mid-memory-rag/).
// The #874 corpus is one level below it and #885's two, and both read the
// replication's base and the shipped tool manifests from there.
func ResearchDir(dir string) string {
	d := filepath.Clean(dir)
	for {
		parent := filepath.Dir(d)
		if _, err := os.Stat(filepath.Join(parent, "mid-memory-rag")); err == nil {
			return parent
		}
		if parent == d {
			return filepath.Join(dir, "..")
		}
		d = parent
	}
}

// CopyDir copies the regular files of a store directory into another,
// leaving out SQLite's sidecar files and refusing a directory whose
// write-ahead log still holds frames.
func CopyDir(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		// A reader leaves an empty write-ahead log and its index behind;
		// only a log holding frames means the main file is not the whole
		// database.
		if strings.HasSuffix(e.Name(), "-wal") {
			info, err := e.Info()
			if err != nil {
				return err
			}
			if info.Size() > 0 {
				return fmt.Errorf("%s holds a non-empty %s; close the store before copying", from, e.Name())
			}
			continue
		}
		if strings.HasSuffix(e.Name(), "-shm") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
