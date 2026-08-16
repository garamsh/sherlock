package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/garamsh/gagent/internal/tool"
)

// ScriptedStep is one canned model reply: either tool-call requests or a
// final response whose Content is the document ResponseSchema describes,
// mirroring the real model contract (ADR 0004).
type ScriptedStep struct {
	// Content is the raw response text: the schema's JSON document.
	Content string
	// ToolCalls requests tool execution; empty means this is a final
	// response. IDs may be left empty — the model assigns call-<n>.
	ToolCalls []ToolCall
	// Err, when non-nil, is returned instead of a response.
	Err error
}

// ScriptedModel is the deterministic mock model (ADR 0004): each Chat call
// consumes the next scripted step in order, so tests script exact
// multi-turn scenarios. Exhausting the script is an error, never a silent
// fallback. Safe for sequential use by one session.
type ScriptedModel struct {
	mu    sync.Mutex
	steps []ScriptedStep
	next  int
	calls int
}

// NewScriptedModel returns a mock model that replays steps in order.
func NewScriptedModel(steps ...ScriptedStep) *ScriptedModel {
	return &ScriptedModel{steps: steps}
}

// Chat returns the next scripted step, or ErrScriptExhausted when the
// script is consumed.
func (m *ScriptedModel) Chat(_ context.Context, _ []Message, _ []tool.Descriptor) (Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.next >= len(m.steps) {
		return Response{}, fmt.Errorf("call %d: %w", m.calls+1, ErrScriptExhausted)
	}
	step := m.steps[m.next]
	m.next++
	m.calls++
	if step.Err != nil {
		return Response{}, step.Err
	}
	for i := range step.ToolCalls {
		if step.ToolCalls[i].ID == "" {
			step.ToolCalls[i].ID = fmt.Sprintf("call-%d", m.calls)
		}
	}
	return Response{Content: step.Content, ToolCalls: step.ToolCalls}, nil
}
