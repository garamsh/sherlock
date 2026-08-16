package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// arityRule is how many values one address holds. It is the whole of what
// separates the three attribute frames: the addressing, the write path and
// the read path are identical in all three.
//
// The seam withholds arity, so a frame has to decide it. Nothing else about
// an attribute-addressed structure is in question — an address settles
// identity outright, with no similarity anywhere in the write path — which
// is why these three arms differ in this and in nothing else.
type arityRule string

const (
	// aritySingle holds one value at an address: a write replaces what is
	// there. The end that updates exactly and cannot hold two values that
	// are true at once.
	aritySingle arityRule = "single"
	// arityMulti holds every value ever written to an address. The other
	// end: nothing is ever lost and nothing is ever replaced.
	arityMulti arityRule = "multi"
	// arityLearned holds every value and decides at read time how many to
	// return, from the attribute's own addresses. A rule that decides later
	// cannot have destroyed anything earlier, so this frame stores what
	// multi stores and returns what it has decided to return.
	arityLearned arityRule = "learned"
	// arityCued reads the statement: a value joins the ones already at its
	// address when the sentence writing it marks an addition, and replaces
	// them otherwise. The cues are a list, and what that costs is the point
	// of measuring it.
	arityCued arityRule = "cued"
	// arityDeclared takes the operation the write states instead of deciding
	// anything. It is a ceiling rather than a design: it measures what any
	// structure reaches when the write operation supplies arity, which is
	// where #453 concluded it belongs, and it is what the structures that
	// have to decide for themselves are read against.
	arityDeclared arityRule = "declared"
)

// additionCues are the phrases that mark a statement as adding a value
// rather than replacing one. They are the words English uses to conjoin,
// written out as the list they are.
//
// Nothing generates this list. Every further way of saying the same thing
// is another entry, and the frame has no principle that would produce the
// next one — which is why the workload's narrowing statements, which mark
// an addition with a qualifier instead of a conjunction, are read as
// replacements. Adding qualifiers to the list would answer them and answer
// nothing else, and that repair is what disqualifies the rule rather than
// what saves it.
var additionCues = []string{"as well", "too", "also", "in addition", "besides"}

// heldValue is one value at an address, with the last time it was written.
type heldValue struct {
	value   string
	written time.Time
}

// attributeFrame holds a fact at (subject, attribute). Writing to an
// address is how a value is replaced, so identity is settled by the address
// and similarity appears nowhere: the mechanism `memory-retrieval/` refuted
// is not weakened here, it is absent.
//
// It keeps no text. What it holds is the address and the value, which is
// why its stored size is a fraction of a store that keeps a sentence, an
// embedding and a keyword index for the same fact.
type attributeFrame struct {
	rule   arityRule
	values map[Address][]heldValue
}

// NewAttributeFrame returns the attribute-addressed frame under one arity
// rule.
func NewAttributeFrame(rule arityRule) Frame {
	return &attributeFrame{rule: rule, values: map[Address][]heldValue{}}
}

// Observe writes the value to its address. A turn carrying no extracted
// fact is not written: an addressed structure has nowhere to put a sentence
// it cannot address, and what that costs is the text arm's whole result.
func (f *attributeFrame) Observe(_ context.Context, t Turn) error {
	for _, written := range t.Written {
		f.write(written, t.Text, t.At)
	}
	return nil
}

// write applies one of a message's assertions.
func (f *attributeFrame) write(written Written, text string, at time.Time) {
	values := f.values[written.Address]
	if f.replaces(written, text) {
		values = values[:0]
	}
	for i, held := range values {
		if held.value == written.Value {
			values[i].written = at
			f.values[written.Address] = values
			return
		}
	}
	f.values[written.Address] = append(values, heldValue{value: written.Value, written: at})
}

// Context returns what the frame holds at the address asked about, and
// nothing else. An addressed structure answers a question by going to one
// place, so what it injects is a line or two rather than a filled budget.
//
// A question naming a subject and no attribute is answered from every
// address under that subject, which is the only thing an addressed store
// can do with one: it is a partial address, and the store has no other way
// in.
func (f *attributeFrame) Context(_ context.Context, t Turn) (Injected, error) {
	if t.Ask == nil {
		return Injected{}, nil
	}
	if t.Ask.Attribute == "" {
		return f.subject(t.Ask.Subject), nil
	}
	values := f.values[*t.Ask]
	scanned := len(values)
	if len(values) == 0 {
		return Injected{}, nil
	}
	if !f.returnsEveryValue(t.Ask.Attribute) {
		values = []heldValue{newest(values)}
	}
	if f.rule == arityLearned {
		// The learned rule reads every address under the attribute to
		// decide how many values this one returns, so answering one
		// question touches the whole store.
		scanned += len(f.values)
	}
	lines := make([]string, 0, len(values))
	for _, held := range values {
		lines = append(lines, line(*t.Ask, held.value))
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: scanned}, nil
}

// subject answers the multi-address question from every address under one
// subject. Nothing bounds what comes back: a store holding an address and a
// value has no ranking to order the addresses by and no budget to stop at,
// so it returns them all.
//
// What it reads is the subject's own values rather than every address it
// holds, which is what a store keyed on the subject first would touch. The
// flat map here would read more, and counting that would report a property
// of this implementation instead of one of the structure.
func (f *attributeFrame) subject(subject string) Injected {
	var (
		lines   []string
		scanned int
	)
	for _, attribute := range attributesUnder(f.values, subject) {
		address := Address{Subject: subject, Attribute: attribute}
		values := f.values[address]
		scanned += len(values)
		if f.rule == arityLearned {
			// The learned rule reads every address under the attribute to
			// decide how many values this one returns, and it takes that
			// decision once per address the answer spans.
			scanned += len(f.values)
		}
		if !f.returnsEveryValue(attribute) {
			values = []heldValue{newest(values)}
		}
		for _, held := range values {
			lines = append(lines, line(address, held.value))
		}
	}
	if len(lines) == 0 {
		return Injected{}
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: scanned}
}

// Size counts one unit per value held and measures the bytes the frame
// would emit for it. It keeps no sentence, no vector and no index, so what
// it stores is what it holds.
func (f *attributeFrame) Size(context.Context) (Size, error) {
	out := Size{}
	for address, values := range f.values {
		for _, held := range values {
			out.Units++
			out.ContentBytes += int64(len(line(address, held.value)))
		}
	}
	out.StoredBytes = out.ContentBytes
	return out, nil
}

// Held is every value at every address, in address order where the map
// gives one and in write order within an address. Nothing downstream reads
// the order, and sorting it would cost more than it says.
func (f *attributeFrame) Held(context.Context) ([]string, error) {
	var out []string
	for address, values := range f.values {
		for _, held := range values {
			out = append(out, line(address, held.value))
		}
	}
	return out, nil
}

// Forget drops the values whose line it is given.
func (f *attributeFrame) Forget(_ context.Context, held []string) error {
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	for address, values := range f.values {
		kept := values[:0]
		for _, one := range values {
			if !victims[line(address, one.value)] {
				kept = append(kept, one)
			}
		}
		if len(kept) == 0 {
			delete(f.values, address)
			continue
		}
		f.values[address] = kept
	}
	return nil
}

func (f *attributeFrame) Close() error { return nil }

// replaces reports whether this write ends the values already at the
// address. Only the two rules that decide arity at write time answer yes:
// single always, cued when the statement marks no addition.
func (f *attributeFrame) replaces(written Written, text string) bool {
	switch f.rule {
	case aritySingle:
		return true
	case arityCued:
		return !marksAddition(text)
	case arityDeclared:
		// The workload states the operation on every assertion of the arm
		// in which this frame writes at all, so "not stated" never reaches
		// here.
		return written.Declared != OperationJoin
	default:
		return false
	}
}

// marksAddition reports whether a statement carries one of the cues. The
// match is the whole-token one the credit rule uses, so "too" is not found
// inside "took".
func marksAddition(text string) bool {
	for _, cue := range additionCues {
		if Mentions(text, cue) {
			return true
		}
	}
	return false
}

// returnsEveryValue reports whether the address answers with all of its
// values or with the newest alone.
func (f *attributeFrame) returnsEveryValue(attribute string) bool {
	if f.rule != arityLearned {
		return true
	}
	return f.multiValued(attribute)
}

// multiValued is the learned rule: an attribute holds several values once
// any address under it has been written more than one. It reads the
// attribute's own addresses rather than a list of attributes written into
// the frame, which is what makes it a rule instead of a clause per case.
//
// It reads every address rather than stopping at the first that answers,
// so what it touches is the same on every run. Stopping early would make
// the count depend on the order a map is walked in.
func (f *attributeFrame) multiValued(attribute string) bool {
	var found bool
	for address, values := range f.values {
		if address.Attribute == attribute && len(values) > 1 {
			found = true
		}
	}
	return found
}

// newest returns the value written last. Values at one address are written
// on different turns, so the comparison is total.
func newest(values []heldValue) heldValue {
	out := values[0]
	for _, held := range values[1:] {
		if !held.written.Before(out.written) {
			out = held
		}
	}
	return out
}

// line is one held value as the frame emits it, and is the unit its size is
// counted in so the two cannot drift apart.
func line(address Address, value string) string {
	return fmt.Sprintf("%s %s %s", address.Subject, address.Attribute, value)
}

// attributesUnder is the attributes one subject holds something under, in
// name order. The order is alphabetical because an addressed store has no
// other: it holds no score, so any order it emits in carries no claim about
// which address matters, and a fixed one keeps a run reproducible.
func attributesUnder[V any](values map[Address]V, subject string) []string {
	var out []string
	for address := range values {
		if address.Subject == subject {
			out = append(out, address.Attribute)
		}
	}
	sort.Strings(out)
	return out
}
