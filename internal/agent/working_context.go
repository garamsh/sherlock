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

// WorkingContextTool curates the current-work record without ending a turn.
const WorkingContextTool = "working_context"

const workingContextSchema = `{
 "type":"object",
 "properties":{
  "status":{"type":["string","null"],"enum":["idle","active","completed","canceled",null],"description":"Null preserves lifecycle status."},
  "progress":{"type":["string","null"],"description":"Subordinate notes: decisions, verified progress, remaining work and uncertainty. Null preserves existing notes."},
  "arrival":{"type":["string","null"],"enum":["retain","replace","dismiss",null],"description":"Apply to the current arrival, without selecting or copying text. Null or retain preserves it as exact source. Replace starts explicitly authorized new work and retires previous source; use active status. Dismiss removes only a neutral current arrival, never prior source."},
  "resolved_attempts":{"type":"array","items":{"type":"object","properties":{
   "id":{"type":"integer"},"evidence":{"type":"string"}
  },"required":["id","evidence"],"additionalProperties":false},"description":"Retire only these attempt IDs, with observed evidence. Empty preserves all attempts."}
 },
 "required":["status","progress","arrival","resolved_attempts"],
 "additionalProperties":false
}`

func workingContextDescriptor() tool.Descriptor {
	return tool.Descriptor{Name: WorkingContextTool, Description: "Optionally curate current work. Source instructions govern progress; no transcription is required before tools. This call does not end the turn.", InputSchema: json.RawMessage(workingContextSchema)}
}

func (s *session) beginWork(ctx context.Context, task string) (bool, error) {
	if s.opts.WorkingContext == nil {
		return false, nil
	}
	work, err := s.opts.WorkingContext.LoadWorkingContext(ctx)
	if err != nil {
		return false, err
	}
	s.work = work
	initialSource := len(work.Source) == 0 && work.Arrival == nil
	if initialSource {
		work.Source = []string{task}
	} else {
		if work.Arrival != nil {
			work.Source = append(append([]string(nil), work.Source...), *work.Arrival)
		}
		work.Arrival = &task
	}
	return initialSource, s.saveWork(ctx, work)
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
	messages := append([]Message(nil), s.history...)
	return append(messages, Message{Role: RoleSystem, Content: workingContextContract + "\nCurrent-work record:\n" + string(data)}), nil
}

func (s *session) prepareCheckpoint(input json.RawMessage) (memory.WorkingContext, error) {
	if len(bytes.TrimSpace(input)) == 0 || bytes.TrimSpace(input)[0] != '{' {
		return memory.WorkingContext{}, fmt.Errorf("working context update must be an object")
	}
	var doc struct {
		Status   *memory.WorkStatus `json:"status"`
		Progress *string            `json:"progress"`
		Arrival  *string            `json:"arrival"`
		Resolved []struct {
			ID       int64  `json:"id"`
			Evidence string `json:"evidence"`
		} `json:"resolved_attempts"`
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return memory.WorkingContext{}, fmt.Errorf("decode working context update: %v", err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return memory.WorkingContext{}, fmt.Errorf("working context update has trailing data")
	}
	work := s.work
	work.Source = append([]string(nil), work.Source...)
	work.Arrival = nil
	work.Attempts = nil
	if doc.Status != nil {
		work.Account.Status = *doc.Status
	}
	if doc.Progress != nil {
		work.Account.Progress = *doc.Progress
	}
	if err := work.Account.Validate(); err != nil {
		return work, err
	}
	action := "retain"
	if doc.Arrival != nil {
		action = *doc.Arrival
	}
	switch action {
	case "retain":
		if s.work.Arrival != nil {
			work.Source = append(work.Source, *s.work.Arrival)
		}
	case "replace":
		if s.work.Arrival == nil || doc.Status == nil || *doc.Status != memory.WorkActive {
			return work, fmt.Errorf("replacement requires a current arrival and active status")
		}
		work.Source = []string{*s.work.Arrival}
		if doc.Progress == nil {
			work.Account.Progress = ""
		}
	case "dismiss":
		if s.work.Arrival == nil {
			return work, fmt.Errorf("no current arrival to dismiss; source cannot be dismissed")
		}
	default:
		return work, fmt.Errorf("invalid arrival action %q", action)
	}
	if s.work.Account.Status.Terminal() && work.Account.Status != s.work.Account.Status && action != "replace" {
		return work, fmt.Errorf("terminal work requires explicit replacement arrival to change status")
	}
	if s.work.Account.Status == memory.WorkActive && work.Account.Status == memory.WorkIdle && action != "replace" {
		return work, fmt.Errorf("active work cannot become idle without replacement")
	}
	resolved := map[int64]string{}
	for _, resolution := range doc.Resolved {
		if resolution.ID <= 0 || strings.TrimSpace(resolution.Evidence) == "" || resolved[resolution.ID] != "" {
			return work, fmt.Errorf("attempt resolution requires unique ID and evidence")
		}
		resolved[resolution.ID] = resolution.Evidence
	}
	for _, attempt := range s.work.Attempts {
		if evidence, ok := resolved[attempt.ID]; ok {
			work.Account.Progress += fmt.Sprintf("\nAttempt %d resolved: %s", attempt.ID, evidence)
			delete(resolved, attempt.ID)
		} else {
			work.Attempts = append(work.Attempts, attempt)
		}
	}
	if len(resolved) != 0 {
		return work, fmt.Errorf("resolution references an unknown or retired attempt")
	}
	if work.Account.Status == memory.WorkCompleted {
		for _, attempt := range work.Attempts {
			if attempt.Result == nil || attempt.Result.Outcome == memory.WorkFailed {
				return work, fmt.Errorf("completed work requires reconciliation of uncertain attempts")
			}
		}
	}
	return work, nil
}

func (s *session) checkpoint(ctx context.Context, call ToolCall) error {
	work, err := s.prepareCheckpoint(call.Input)
	if err != nil {
		return err
	}
	return s.saveWork(ctx, work)
}

func (s *session) recordAttempt(ctx context.Context, call ToolCall) error {
	work := s.work
	if work.Account.Status.Terminal() {
		return fmt.Errorf("terminal work requires explicit replacement arrival before operator calls")
	}
	if work.Account.Status == memory.WorkIdle {
		work.Account.Status = memory.WorkActive
	}
	work.Attempts = append(append([]memory.WorkAttempt(nil), work.Attempts...), memory.WorkAttempt{ID: work.Revision + 1, Name: call.Name, Input: call.Input})
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
