package main

import (
	"context"
	"fmt"
	"time"
)

// Address is a subject and one of its attributes: where a fact about that
// subject is held by a structure that addresses facts rather than searching
// for them.
//
// Attribute is a topic noun for the frames that address by topic and the
// question a fact answers for the frames that address by question, and
// Qualifier is the restriction that question carries where the sentence put
// one on it. `question.go` forms both and states what it may read to do it;
// every other frame receives an empty Qualifier and is the structure it was.
type Address struct {
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
	Qualifier string `json:"qualifier,omitempty"`
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

// Written is one assertion as a write states it: the fact in extracted
// form, and what the write does with the values already at its address.
//
// One message can assert several things, so a turn carries a list of these
// rather than one of each. Arity stays off Fact — a fact still says nothing
// about how many values its attribute holds — and sits here, on the write,
// which is where this record concluded it belongs.
type Written struct {
	Fact
	Declared Operation
}

// Turn is one observation a frame receives. Text is what was said.
//
// Written and Ask are the same turn already extracted, present only in the
// arms that extract: Written on a turn that asserts something, Ask on a
// turn that questions. A frame may read them, the text, or neither.
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
	Index   int
	Day     int
	At      time.Time
	Text    string
	Written []Written
	Ask     *Address
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
	// Size reports how much the frame holds.
	Size(ctx context.Context) (Size, error)
	// Forget drops what it is given, which is a subset of what Held
	// returned. A structure under a capacity has to give something up, and
	// what it gives up is decided outside it: no frame carries a rule for
	// that, the same way none carries one for arity.
	Forget(ctx context.Context, held []string) error
	// Held reports what the frame holds, as the text it would emit for it.
	// Size counts; this identifies, which is what separates the part of a
	// store a read has reached from the part no read ever has.
	Held(ctx context.Context) ([]string, error)
	// Close releases whatever the frame opened.
	Close() error
}

// The frames measured. shipped is internal/memory as it runs in production;
// document keeps no store at all and is what proves the seam does not
// encode the shipped design's shape; the attribute frames hold a fact at
// its address and differ only in how many values one address holds; the
// question frames hold it at the question it answers instead. The last five
// are published designs ported behind the same seam, each named after the
// paper it comes from.
const (
	frameShipped           = "shipped"
	frameDocument          = "document"
	frameAttributeSingle   = "attribute-single"
	frameAttributeMulti    = "attribute-multi"
	frameAttributeLearned  = "attribute-learned"
	frameAttributeCued     = "attribute-cued"
	frameAttributeDeclared = "attribute-declared"
	frameQuestion          = "question"
	frameQuestionDeclared  = "question-declared"
	frameInterval          = "interval"
	frameIntervalDeclared  = "interval-declared"
	frameLog               = "log"
	frameGenerativeAgents  = "generative-agents"
	frameMemoryBank        = "memorybank"
	frameReflection        = "reflection"
	frameMem0              = "mem0"
	frameMem0Nearest       = "mem0-nearest"
)

// frameNames is the order every result file and table reports the frames
// in. The designs invented here come first and the published ones after
// them, in the order they were published.
var frameNames = []string{
	frameShipped, frameDocument,
	frameAttributeSingle, frameAttributeMulti, frameAttributeLearned, frameAttributeCued,
	frameAttributeDeclared, frameQuestion, frameQuestionDeclared,
	frameInterval, frameIntervalDeclared, frameLog,
	frameGenerativeAgents, frameReflection, frameMemoryBank, frameMem0, frameMem0Nearest,
}

// NewFrame builds a frame by name. dir is a directory the frame may keep
// state in; a frame that keeps none ignores it.
func NewFrame(name, dir string, counter *EmbedCounter, budget int) (Frame, error) {
	switch name {
	case frameShipped:
		return NewShippedFrame(dir, counter, budget)
	case frameDocument:
		return NewDocumentFrame(budget), nil
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
	case frameQuestion:
		return NewQuestionFrame(arityQuestion), nil
	case frameQuestionDeclared:
		return NewQuestionFrame(arityDeclared), nil
	case frameInterval:
		return NewIntervalFrame(aritySingle), nil
	case frameIntervalDeclared:
		return NewIntervalFrame(arityDeclared), nil
	case frameLog:
		return NewLogFrame(), nil
	case frameGenerativeAgents:
		return NewStreamFrame(ruleGenerativeAgents, counter, budget), nil
	case frameReflection:
		return NewStreamFrame(ruleReflection, counter, budget), nil
	case frameMemoryBank:
		return NewStreamFrame(ruleMemoryBank, counter, budget), nil
	case frameMem0:
		return NewMem0Frame(counter), nil
	case frameMem0Nearest:
		return NewMem0NearestFrame(counter), nil
	default:
		return nil, fmt.Errorf("unknown frame %q (want one of %v)", name, frameNames)
	}
}
