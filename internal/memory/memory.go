package memory

import (
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when no active memory exists for an ID.
	ErrNotFound = errors.New("memory not found")
	// ErrNotEpisodic is returned when an operation restricted to episodic
	// memories receives a semantic one.
	ErrNotEpisodic = errors.New("memory is not episodic")
	// ErrInvalidInput is returned for inputs that would corrupt the store.
	ErrInvalidInput = errors.New("invalid memory input")
)

// Type distinguishes the two kinds of long-term memory.
type Type string

const (
	// TypeEpisodic marks memories of specific events or turns.
	TypeEpisodic Type = "episodic"
	// TypeSemantic marks durable knowledge, often compressed from
	// episodic memories.
	TypeSemantic Type = "semantic"
)

// Memory is one stored entry. SupersededBy is 0 and ValidTo is the zero
// time while the memory is active (searchable); both are set when the
// memory moves to the archive.
type Memory struct {
	ID              int64
	Content         string
	Embedding       []float32
	Type            Type
	ImportanceScore float64
	AccessCount     int64
	LastAccessed    time.Time
	Timestamp       time.Time
	Source          string
	SupersededBy    int64
	ValidTo         time.Time
}

// ScoredMemory is a Memory with its ranking score from a search.
type ScoredMemory struct {
	Memory
	Score float64
}
