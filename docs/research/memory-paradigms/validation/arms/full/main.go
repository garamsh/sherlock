// Command full is arm W of the #900 validation study: the shipped recall
// with its live window raised to the newest messages under a 180,000-token
// history budget, the full-context ceiling of ../../../README.md §W.
package main

import (
	"context"
	"fmt"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
)

func main() { study.Main(New()) }

// New returns arm W.
func New() study.Arm { return arm{shipped: study.ShippedArm{ArmName: "W", Batches: []string{"b4"}}} }

// Setting is W's one setting.
const Setting = "w180k"

// WindowMessages is how many of #890's newest messages fit a 180,000-token
// history in MiniMax-M2's chat template: ../../tokens.json, corpus
// "#890 encoding validation (46ca289)", window.messages. Its oldest is
// source 2,039.
const WindowMessages = 6890

type arm struct{ shipped study.ShippedArm }

func (arm) Name() string         { return "W" }
func (arm) Settings() []string   { return []string{Setting} }
func (arm) Instructions() string { return "" }

// WindowFor is W's mechanism: the live session restores and keeps the
// history under the budget instead of the corpus's 32 messages.
func (arm) WindowFor(string) int { return WindowMessages }

func (a arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	if setting != Setting {
		return study.Built{}, fmt.Errorf("arm W has no setting %q", setting)
	}
	return a.shipped.Build(ctx, env, "b4", dir)
}

func (a arm) Open(ctx context.Context, env *study.Env, _ string, dir string) (study.Memory, error) {
	return a.shipped.Open(ctx, env, "b4", dir)
}
