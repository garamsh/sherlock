package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool"
)

// WorkingContextTool replaces the one current-work account without ending a turn.
const WorkingContextTool = "working_context"

const workingContextSchema = `{
 "type":"object",
 "properties":{
  "status":{"type":"string","enum":["idle","active","completed","canceled"]},
  "goal":{"type":"string"},
  "constraints":{"type":"string"},
  "decisions":{"type":"string"},
  "progress":{"type":"string"},
  "remaining":{"type":"string"},
  "uncertainty":{"type":"string"},
  "reconciliation":{"type":"string","description":"Evidence from surviving artifacts resolving ALL unknown attempts, or empty to keep them unresolved."}
 },
 "required":["status","goal","constraints","decisions","progress","remaining","uncertainty","reconciliation"],
 "additionalProperties":false
}`

func workingContextDescriptor() tool.Descriptor {
	return tool.Descriptor{Name: WorkingContextTool, Description: "Checkpoint the complete current work account before effects and after observations. This call does not end the turn.", InputSchema: json.RawMessage(workingContextSchema)}
}

func (s *session) beginWork(ctx context.Context, task string) error {
	if s.opts.WorkingContext == nil {
		return nil
	}
	work, err := s.opts.WorkingContext.LoadWorkingContext(ctx)
	if err != nil {
		return err
	}
	s.work = work
	s.workReady = false
	work.Inputs = append(append([]string(nil), work.Inputs...), task)
	return s.saveWork(ctx, work)
}

func (s *session) saveWork(ctx context.Context, work memory.WorkingContext) error {
	revision, err := s.opts.WorkingContext.SaveWorkingContext(ctx, work)
	if err != nil {
		return err
	}
	work.Revision = revision
	s.work = work
	return nil
}

func (s *session) reasoningContext() ([]Message, error) {
	if s.opts.WorkingContext == nil {
		return s.history, nil
	}
	data, err := json.Marshal(s.work)
	if err != nil {
		return nil, fmt.Errorf("encode reasoning context: %v", err)
	}
	phase := "Checkpoint saved; continue acting or finish."
	if !s.workReady {
		phase = "Checkpoint required before operator tools; memory_write may finish directly with the final account."
	}
	messages := append([]Message(nil), s.history...)
	return append(messages, Message{Role: RoleSystem, Content: workingContextContract + "\n" + phase + "\nCurrent-work record:\n" + string(data)}), nil
}

func (s *session) prepareCheckpoint(input json.RawMessage) (memory.WorkingContext, error) {
	var doc struct {
		memory.WorkAccount
		Reconciliation string `json:"reconciliation"`
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return memory.WorkingContext{}, fmt.Errorf("decode working context checkpoint: %v", err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return memory.WorkingContext{}, fmt.Errorf("checkpoint has trailing data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return memory.WorkingContext{}, fmt.Errorf("checkpoint fields: %v", err)
	}
	for _, name := range []string{"status", "goal", "constraints", "decisions", "progress", "remaining", "uncertainty", "reconciliation"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(value, []byte("null")) {
			return memory.WorkingContext{}, fmt.Errorf("checkpoint requires %s", name)
		}
	}
	if err := doc.Validate(); err != nil {
		return memory.WorkingContext{}, err
	}
	work := s.work
	work.Account = doc.WorkAccount
	work.Inputs = nil
	work.Attempts = nil
	if strings.TrimSpace(doc.Reconciliation) == "" {
		for _, attempt := range s.work.Attempts {
			if attempt.Result == nil || attempt.Result.Outcome == memory.WorkFailed {
				work.Attempts = append(work.Attempts, attempt)
			}
		}
	} else {
		work.Account.Decisions += "\nReconciliation: " + doc.Reconciliation
	}
	if work.Account.Status == memory.WorkCompleted && len(work.Attempts) > 0 {
		return memory.WorkingContext{}, fmt.Errorf("completed work requires reconciliation of uncertain attempts")
	}
	return work, nil
}

func (s *session) checkpoint(ctx context.Context, call ToolCall) error {
	work, err := s.prepareCheckpoint(call.Input)
	if err != nil {
		return err
	}
	if err := s.saveWork(ctx, work); err != nil {
		return err
	}
	s.workReady = true
	return nil
}

func (s *session) recordAttempt(ctx context.Context, call ToolCall) error {
	work := s.work
	work.Attempts = append(append([]memory.WorkAttempt(nil), work.Attempts...), memory.WorkAttempt{Name: call.Name, Input: call.Input})
	return s.saveWork(ctx, work)
}

func (s *session) recordResult(ctx context.Context, result string, failed bool) error {
	work := s.work
	work.Attempts = append([]memory.WorkAttempt(nil), work.Attempts...)
	outcome := memory.WorkReturned
	if failed {
		outcome = memory.WorkFailed
	}
	work.Attempts[len(work.Attempts)-1].Result = &memory.WorkResult{Outcome: outcome, Content: result}
	return s.saveWork(ctx, work)
}
