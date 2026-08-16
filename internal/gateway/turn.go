package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/garamsh/gagent/internal/agent"
)

// Turn runs one message through a fresh runtime and reports what the
// agent produced, for a caller that is not serving: the one-shot mode
// (ADR 0007's ingress has no reader when the process exits with the
// turn).
//
// Out receives the turn's response and the messages the turn delivered,
// because a caller with no outbox watcher would otherwise see the reply
// only as a file it has to go looking for. The file outbox stays the
// durable record; Out is the echo.
type Turn struct {
	// Factory builds the runtime the turn runs on.
	Factory *Factory
	// AgentID is the agent this turn belongs to.
	AgentID string
	// OutboxDir is the directory the message_send tool delivers into,
	// which the turn diffs to find what it sent.
	OutboxDir string
	// Out receives the response and the delivered messages.
	Out io.Writer
	// Logger reports failures that do not stop the turn, such as a
	// runtime that would not close.
	Logger *slog.Logger
}

// Run runs one turn and returns the failure the caller's exit status
// should carry.
//
// The turn's output is written before a failed tool call is reported,
// so a caller reading Out sees what the agent produced either way. A
// failed tool call does not end the turn — the model is told and gets
// another iteration, which is the loop working — so a clean return is
// the only signal a caller that stopped watching would otherwise have,
// and it would be wrong.
func (t Turn) Run(ctx context.Context, msg string) error {
	before := outboxSnapshot(t.OutboxDir)

	failed := &failedToolCalls{}
	rt, err := t.Factory.New(ctx, t.AgentID, failed)
	if err != nil {
		return err
	}
	defer func() {
		if err := rt.Close(context.Background()); err != nil {
			t.Logger.Error("runtime close failed", "op", "shutdown", "error", err)
		}
	}()

	response, err := rt.Session.Run(ctx, msg)
	if err != nil {
		return err
	}
	fmt.Fprintln(t.Out, response)
	echoOutbox(t.Out, t.OutboxDir, before)

	if len(failed.names) > 0 {
		return fmt.Errorf("one-shot turn: %d tool call(s) failed: %s",
			len(failed.names), strings.Join(failed.names, ", "))
	}
	return nil
}

// failedToolCalls collects the names of a turn's failed tool calls so
// the caller can report them. It is the one-shot counterpart of Broker:
// both implement agent.EventSink for a consumer of the loop's events,
// and both live with that consumer rather than with the interface.
//
// A served turn hands the session the monitoring sink instead, and its
// failed tool call reaches the model and the event stream. A one-shot
// run has no such reader.
//
// The session emits from the goroutine that called Run, and a turn
// calls Run once, so the collection needs no lock.
type failedToolCalls struct {
	names []string
}

// Emit implements agent.EventSink.
func (f *failedToolCalls) Emit(_ context.Context, e agent.Event) {
	if e.Kind == agent.EventToolCall && e.ToolFailed {
		f.names = append(f.names, e.ToolName)
	}
}
