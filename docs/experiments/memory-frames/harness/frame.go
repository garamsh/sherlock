package main

import (
	"context"
	"fmt"
	"time"
)

// Address is a subject and one of its attributes: where a fact about that
// subject is held by a structure that addresses facts rather than searching
// for them.
type Address struct {
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
}

// Fact is one assertion in extracted form: an address and a value. It
// carries nothing else on purpose.
//
// It does not say whether the attribute holds one value or several. Arity
// is the question the coexisting classes exist to ask, and a design handed
// it would have been handed the answer. It does not say what an assertion
// replaces either: deciding that a new value supersedes an old one, or
// stands beside it, is what separates one structure from another.
type Fact struct {
	Address
	Value string `json:"value"`
}

// Operation is what a write does to the values already at its address: end
// them, or stand beside them. It is the arity decision stated rather than
// inferred.
type Operation string

const (
	// OperationReplace ends the values already at the address.
	OperationReplace Operation = "replace"
	// OperationJoin leaves them where they are and adds one.
	OperationJoin Operation = "join"
)

// Turn is one observation a frame receives. Text is what was said.
//
// Fact and Ask are the same turn already extracted, present only in the
// oracle arm: Fact on a turn that asserts something, Ask on a turn that
// questions. A frame may read them, the text, or neither.
//
// Declared is not extraction. It is the write operation, which is where
// #453 concluded arity belongs — nothing recovers from the triple what the
// sentence carried, so a model-driven writeback that reads the sentence is
// the only thing that can state it. It arrives beside Fact in the same arm,
// every frame receives it, and only the ceiling reads it: a frame that does
// is not answering the arity question but assuming it answered, which is
// what makes that arm a ceiling rather than a design.
//
// Ask carries the address and never the value, for the reason Fact carries
// no arity: it says where to look and not what is there. It exists because
// a structure that addresses facts has to turn a question into an address,
// and with no model in the run it would otherwise have to parse the
// question — which would make the comparison a comparison of parsers on the
// read side, having removed exactly that from the write side.
type Turn struct {
	Index    int
	Day      int
	At       time.Time
	Text     string
	Fact     *Fact
	Ask      *Address
	Declared Operation
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

// Injected is what a frame puts in the model's context, and how much of
// what it holds it read to produce it.
//
// Scanned counts the units a structure touched to answer one question, in
// the unit its own Size reports: an addressed store reads one address, a
// projection over a log reads every event, and a store that ranks its index
// reads all of it. It counts what the structure touches and not how long
// that takes — this record measures no latency, and a count is not a proxy
// for one.
type Injected struct {
	Text    string
	Scanned int
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
	// this turn, and how much of what it holds it read to produce it.
	Context(ctx context.Context, t Turn) (Injected, error)
	// Size reports what the frame holds.
	Size(ctx context.Context) (Size, error)
	// Close releases whatever the frame opened.
	Close() error
}

// The frames measured. shipped is internal/memory as it runs in production;
// document keeps no store at all and is what proves the seam does not
// encode the shipped design's shape; the three attribute frames hold a fact
// at its address and differ only in how many values one address holds.
const (
	frameShipped           = "shipped"
	frameDocument          = "document"
	frameAttributeSingle   = "attribute-single"
	frameAttributeMulti    = "attribute-multi"
	frameAttributeLearned  = "attribute-learned"
	frameAttributeCued     = "attribute-cued"
	frameAttributeDeclared = "attribute-declared"
	frameInterval          = "interval"
	frameLog               = "log"
)

// frameNames is the order every result file and table reports the frames
// in.
var frameNames = []string{
	frameShipped, frameDocument,
	frameAttributeSingle, frameAttributeMulti, frameAttributeLearned, frameAttributeCued,
	frameAttributeDeclared, frameInterval, frameLog,
}

// NewFrame builds a frame by name. dir is a directory the frame may keep
// state in; a frame that keeps none ignores it.
func NewFrame(name, dir string, counter *EmbedCounter) (Frame, error) {
	switch name {
	case frameShipped:
		return NewShippedFrame(dir, counter)
	case frameDocument:
		return NewDocumentFrame(), nil
	case frameAttributeSingle:
		return NewAttributeFrame(aritySingle), nil
	case frameAttributeMulti:
		return NewAttributeFrame(arityMulti), nil
	case frameAttributeLearned:
		return NewAttributeFrame(arityLearned), nil
	case frameAttributeCued:
		return NewAttributeFrame(arityCued), nil
	case frameAttributeDeclared:
		return NewAttributeFrame(arityDeclared), nil
	case frameInterval:
		return NewIntervalFrame(), nil
	case frameLog:
		return NewLogFrame(), nil
	default:
		return nil, fmt.Errorf("unknown frame %q (want one of %v)", name, frameNames)
	}
}
