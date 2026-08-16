package main

import (
	"context"
	"fmt"
	"time"
)

// Fact is one assertion in extracted form: a subject, an attribute and a
// value. It carries nothing else on purpose.
//
// It does not say whether the attribute holds one value or several. Arity
// is the question the coexisting classes exist to ask, and a design handed
// it would have been handed the answer. It does not say what an assertion
// replaces either: deciding that a new value supersedes an old one, or
// stands beside it, is what separates one structure from another.
type Fact struct {
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
}

// Turn is one observation a frame receives. Text is what was said. Fact is
// the same assertion already extracted, present only in the oracle arm and
// only on a turn that carries one; a frame may read either, both, or
// neither.
type Turn struct {
	Index int
	Day   int
	At    time.Time
	Text  string
	Fact  *Fact
}

// Size is what a frame holds. Units counts whatever the frame treats as one
// thing it holds, which is a row for a store of rows and the document
// itself for a frame that keeps one. ContentBytes is the text it holds;
// StoredBytes is what that costs where it is kept, which for an indexed
// store is larger.
type Size struct {
	Units        int   `json:"units"`
	ContentBytes int64 `json:"content_bytes"`
	StoredBytes  int64 `json:"stored_bytes"`
}

// Frame is one memory structure under measurement: a turn goes in, and what
// the structure would put in the model's context at any later turn comes
// out.
//
// The seam is this small so that a structure unlike the one this project
// ships can sit behind it. Context returns text rather than rows, because a
// frame that keeps a single rewritten document has no rows to return and
// scoring it on rows would decide the comparison before it ran. Nothing
// here mentions a query, a limit, a score or a similarity: a frame that
// performs no retrieval at all answers Context by returning what it holds.
type Frame interface {
	// Observe takes one turn. What the frame does with it — store a row,
	// rewrite a document, append to a log, nothing at all — is the frame's
	// own business.
	Observe(ctx context.Context, t Turn) error
	// Context returns what the frame would put in the model's context at
	// this turn.
	Context(ctx context.Context, t Turn) (string, error)
	// Size reports what the frame holds.
	Size(ctx context.Context) (Size, error)
	// Close releases whatever the frame opened.
	Close() error
}

// The frames measured. shipped is internal/memory as it runs in production;
// document keeps no store at all and is what proves the seam does not
// encode the shipped design's shape.
const (
	frameShipped  = "shipped"
	frameDocument = "document"
)

// frameNames is the order every result file and table reports the frames
// in.
var frameNames = []string{frameShipped, frameDocument}

// NewFrame builds a frame by name. dir is a directory the frame may keep
// state in; a frame that keeps none ignores it.
func NewFrame(name, dir string, counter *EmbedCounter) (Frame, error) {
	switch name {
	case frameShipped:
		return NewShippedFrame(dir, counter)
	case frameDocument:
		return NewDocumentFrame(), nil
	default:
		return nil, fmt.Errorf("unknown frame %q (want %q or %q)", name, frameShipped, frameDocument)
	}
}
