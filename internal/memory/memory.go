package memory

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when no active memory exists for an ID.
	ErrNotFound = errors.New("memory not found")
	// ErrInvalidInput is returned for inputs that would corrupt the store.
	ErrInvalidInput = errors.New("invalid memory input")
)

// Address is where a fact is held (ADR 0018): the subject it is about,
// the question it answers, and whatever restriction the sentence puts on
// that question. Restriction is empty where the sentence places none.
//
// The address is what settles which memory a write is for. No field of it
// is compared for similarity against anything, and no threshold reads it.
type Address struct {
	Subject     string
	Question    string
	Restriction string
}

// Operation is what a value does to the values already at its address
// (ADR 0018). There is no third: a write either replaces what is held or
// joins it.
type Operation string

const (
	// OpReplace supersedes the values already at the address.
	OpReplace Operation = "replace"
	// OpJoin leaves them where they are and adds one.
	OpJoin Operation = "join"
)

// Memory is one value held at one address. SupersededBy is 0 and ValidTo
// is the zero time while the memory is active (readable); both are set
// when the memory moves to the archive.
type Memory struct {
	ID              int64
	Address         Address
	Value           string
	ImportanceScore float64
	AccessCount     int64
	LastAccessed    time.Time
	Timestamp       time.Time
	Source          string
	SupersededBy    int64
	ValidTo         time.Time
}

// WorkStatus distinguishes current authorization from a terminal work account.
type WorkStatus string

const (
	// WorkActive permits continuation of unfinished work.
	WorkActive WorkStatus = "active"
	// WorkCompleted records achieved work without authorizing replay.
	WorkCompleted WorkStatus = "completed"
	// WorkCanceled records withdrawn authorization.
	WorkCanceled WorkStatus = "canceled"
	// WorkIdle records that no work has been authorized.
	WorkIdle WorkStatus = "idle"
)

// WorkAccount is the model's complete account of the one current work item.
type WorkAccount struct {
	Status      WorkStatus `json:"status"`
	Goal        string     `json:"goal"`
	Constraints string     `json:"constraints"`
	Decisions   string     `json:"decisions"`
	Progress    string     `json:"progress"`
	Remaining   string     `json:"remaining"`
	Uncertainty string     `json:"uncertainty"`
}

// WorkAttempt records an invocation whose effect may outlive its caller.
// A nil result means no result was durably observed, never that nothing ran.
type WorkAttempt struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Result *WorkResult     `json:"result"`
}

// WorkingContext is the single durable current-work authority.
// Inputs and attempts remain until a checkpoint incorporates them.
type WorkingContext struct {
	Revision int64         `json:"-"`
	Account  WorkAccount   `json:"account"`
	Inputs   []string      `json:"inputs"`
	Attempts []WorkAttempt `json:"attempts"`
}

// ErrStaleWorkingContext refuses a writer whose durable revision changed.
var ErrStaleWorkingContext = errors.New("stale working context")

// WorkResult is an observed return, not proof that an arbitrary effect completed.
type WorkResult struct {
	Outcome WorkOutcome `json:"outcome"`
	Content string      `json:"content"`
}

// WorkOutcome distinguishes a returned value from a possibly partial failure.
type WorkOutcome string

const (
	// WorkReturned means the client returned a value.
	WorkReturned WorkOutcome = "returned"
	// WorkFailed means the client returned an error and effects need reconciliation.
	WorkFailed WorkOutcome = "failed"
)
