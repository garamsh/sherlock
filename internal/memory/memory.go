package memory

import (
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
