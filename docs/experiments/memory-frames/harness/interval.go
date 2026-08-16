package main

import (
	"context"
	"strings"
	"time"
)

// validity is one value at an address and the span it was true for. To is
// the zero time while the value is still true.
type validity struct {
	value string
	from  time.Time
	to    time.Time
}

// intervalFrame writes a value with a start and never removes one. A
// replacement closes the open interval instead of deleting the row, so a
// value that stops being true is still there with the span it was true for.
//
// It inherits the arity question and does not answer it: closing is
// replacing and not closing is coexisting, which is the same decision the
// attribute arms had to take. This frame closes, because a frame that never
// closed would have nothing closed, and separating what is true now from
// what was ever true — the only thing this structure does that an addressed
// store cannot — would not exist.
//
// What it can be asked is the difference. Context answers as of the moment
// of the question, so the same structure that answers "what is true now"
// answers "what was true then" by being asked then.
// It runs under two of the arity rules the attribute frames run under, and
// under nothing else. Closing on every write is the self-deciding end;
// closing when the write says to is the same store taking the declaration,
// which is how a wrongly-cleared value comes to be sitting in a closed span
// rather than gone.
type intervalFrame struct {
	rule   arityRule
	values map[Address][]validity
}

// NewIntervalFrame returns the validity-interval frame under one arity
// rule.
func NewIntervalFrame(rule arityRule) Frame {
	return &intervalFrame{rule: rule, values: map[Address][]validity{}}
}

// Observe closes whatever is open at the address and opens a span for the
// new value. A value written again after it was closed opens a second span
// rather than reviving the first: it was true, then it was not, and then it
// was again, and the record says so.
func (f *intervalFrame) Observe(_ context.Context, t Turn) error {
	for _, written := range t.Written {
		spans := f.values[written.Address]
		if f.closes(written) {
			for i := range spans {
				if spans[i].to.IsZero() {
					spans[i].to = t.At
				}
			}
		}
		f.values[written.Address] = append(spans, validity{value: written.Value, from: t.At})
	}
	return nil
}

// Context returns the values whose span covers the moment the question is
// asked. A question naming a subject and no attribute is answered from
// every address under that subject, as of the same moment.
func (f *intervalFrame) Context(_ context.Context, t Turn) (Injected, error) {
	if t.Ask == nil {
		return Injected{}, nil
	}
	if t.Ask.Attribute == "" {
		return f.subject(t.Ask.Subject, t.At), nil
	}
	spans := f.values[*t.Ask]
	var lines []string
	for _, span := range spans {
		if span.covers(t.At) {
			lines = append(lines, line(*t.Ask, span.value))
		}
	}
	if len(spans) == 0 {
		return Injected{}, nil
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: len(spans)}, nil
}

// subject answers the multi-address question from every address under one
// subject, as of the moment it is asked. Nothing bounds what comes back:
// a store of spans has no ranking over its addresses and no budget to stop
// at, so it returns every span that covers the moment.
func (f *intervalFrame) subject(subject string, at time.Time) Injected {
	var (
		lines   []string
		scanned int
	)
	for _, address := range addressesUnder(f.values, subject) {
		spans := f.values[address]
		scanned += len(spans)
		for _, span := range spans {
			if span.covers(at) {
				lines = append(lines, line(address, span.value))
			}
		}
	}
	if len(lines) == 0 {
		return Injected{}
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: scanned}
}

// Size counts one unit per span. A value that was true twice is two spans,
// which is what the structure holds and what it costs.
func (f *intervalFrame) Size(context.Context) (Size, error) {
	out := Size{}
	for address, spans := range f.values {
		for _, span := range spans {
			out.Units++
			out.ContentBytes += int64(len(line(address, span.value)))
		}
	}
	out.StoredBytes = out.ContentBytes
	return out, nil
}

// Held is every span, open or closed. A closed span is held and unreachable
// by a question about the present, which is the distinction this round is
// measuring.
func (f *intervalFrame) Held(context.Context) ([]string, error) {
	var out []string
	for address, spans := range f.values {
		for _, span := range spans {
			out = append(out, line(address, span.value))
		}
	}
	return out, nil
}

// Forget drops the spans whose line it is given. Nothing about holding a
// closed span protects it from a bound.
func (f *intervalFrame) Forget(_ context.Context, held []string) error {
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	for address, spans := range f.values {
		kept := spans[:0]
		for _, span := range spans {
			if !victims[line(address, span.value)] {
				kept = append(kept, span)
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

func (f *intervalFrame) Close() error { return nil }

// closes reports whether this write ends the spans open at the address.
// The self-deciding rule closes on every write; the declared rule closes
// when the write says it replaces.
func (f *intervalFrame) closes(written Written) bool {
	if f.rule == arityDeclared {
		return written.Declared != OperationJoin
	}
	return true
}

// Values is every value the store holds at an address, open span or closed.
// It is what the store kept rather than what a question reaches, and the
// difference between the two is this round's subject.
func (f *intervalFrame) Values(address Address) []string {
	var out []string
	for _, span := range f.values[address] {
		if !contains(out, span.value) {
			out = append(out, span.value)
		}
	}
	return out
}

// covers reports whether the span was true at the given moment. A span
// starts at its write and ends when the write that closed it landed, so the
// value that replaced it is the one true at that instant.
func (v validity) covers(at time.Time) bool {
	if v.from.After(at) {
		return false
	}
	return v.to.IsZero() || v.to.After(at)
}
