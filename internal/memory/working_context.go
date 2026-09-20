package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseWorkingContext checks the durable document before it reaches reasoning.
func ParseWorkingContext(data []byte) (WorkingContext, error) {
	var work WorkingContext
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&work); err != nil {
		return work, fmt.Errorf("decode working context: %v", err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return work, fmt.Errorf("working context has trailing data")
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return work, fmt.Errorf("working context fields: %v", err)
	}
	for _, name := range []string{"account", "inputs", "attempts"} {
		if _, ok := document[name]; !ok {
			return work, fmt.Errorf("working context requires %s", name)
		}
	}
	var account map[string]json.RawMessage
	if err := json.Unmarshal(document["account"], &account); err != nil {
		return work, fmt.Errorf("working account fields: %v", err)
	}
	for _, name := range []string{"status", "goal", "constraints", "decisions", "progress", "remaining", "uncertainty"} {
		value, ok := account[name]
		if !ok || bytes.Equal(value, []byte("null")) {
			return work, fmt.Errorf("working account requires %s", name)
		}
	}
	if err := work.Account.Validate(); err != nil {
		return work, err
	}
	for _, attempt := range work.Attempts {
		if attempt.Result != nil && attempt.Result.Outcome != WorkReturned && attempt.Result.Outcome != WorkFailed {
			return work, fmt.Errorf("invalid working context result outcome")
		}
		if strings.TrimSpace(attempt.Name) == "" || !json.Valid(attempt.Input) {
			return work, fmt.Errorf("invalid working context attempt")
		}
	}
	return work, nil
}

// Validate checks an account at the model or storage boundary.
func (a WorkAccount) Validate() error {
	switch a.Status {
	case WorkActive:
		if strings.TrimSpace(a.Remaining) == "" {
			return fmt.Errorf("active work requires remaining work")
		}
	case WorkCompleted, WorkCanceled:
		if strings.TrimSpace(a.Remaining) != "" {
			return fmt.Errorf("terminal work cannot carry remaining work")
		}
	case WorkIdle:
		if a.Goal != "" || a.Remaining != "" {
			return fmt.Errorf("idle work cannot carry a goal or remaining work")
		}
		return nil
	default:
		return fmt.Errorf("invalid working context status %q", a.Status)
	}
	if strings.TrimSpace(a.Goal) == "" {
		return fmt.Errorf("working context requires a goal")
	}
	return nil
}
