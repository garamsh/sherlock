// Command agent is arm R of the #900 validation study: before the act
// loop, a recall step of at most six model calls searches and inspects the
// store, and the acting model receives only the facts whose cited sources
// that step read (../../../README.md §R).
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

func main() { study.Main(New()) }

// New returns arm R.
func New() study.Arm { return arm{shipped: study.ShippedArm{ArmName: "R", Batches: []string{"b4"}}} }

// Setting is R's one setting.
const Setting = "b4-agent"

type arm struct{ shipped study.ShippedArm }

func (arm) Name() string         { return "R" }
func (arm) Settings() []string   { return []string{Setting} }
func (arm) Instructions() string { return "" }

// Build copies the shared batch-4 store: R stores nothing of its own.
func (a arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	if setting != Setting {
		return study.Built{}, fmt.Errorf("arm R has no setting %q", setting)
	}
	return a.shipped.Build(ctx, env, "b4", dir)
}

// Open reads the store. Its recall calls the model, once per probe under
// the probe's ID and the phase "agent", so it runs only live, with -proxy.
func (arm) Open(_ context.Context, env *study.Env, _ string, dir string) (study.Memory, error) {
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	if err != nil {
		return nil, err
	}
	probes := map[string]string{}
	for _, p := range env.Corpus.LiveProbes() {
		probes[p.Query] = p.ID
	}
	return &memory{
		ShippedMemory: study.ShippedMemory{Conversation: store},
		newModel:      func(episode string) (agent.Model, error) { return env.Model(episode, "agent") },
		probes:        probes,
	}, nil
}
