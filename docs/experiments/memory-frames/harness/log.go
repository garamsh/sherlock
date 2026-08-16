package main

import (
	"context"
	"strings"
	"time"
)

// event is one observation, appended and never changed.
type event struct {
	address Address
	value   string
	at      time.Time
}

// projection turns the events at one address into the values the memory
// holds there. It is where this structure takes the arity decision, and it
// is a parameter rather than a body because that is the structure's one
// distinguishing property: the log is lossless, so the decision is
// reversible and a different projection over the same events is a different
// memory.
type projection func(events []event) []string

// logFrame keeps every observation as an event and mutates nothing. The
// memory is not stored; it is computed from the log when it is asked for.
//
// Its arity decision lives in the projection and is therefore the only one
// here that can be taken again: no event is lost, so a projection that
// decides differently produces a different memory from the same log without
// anything having been re-observed. What that costs is that a decade of
// events is kept and read.
type logFrame struct {
	events  []event
	project projection
}

// NewLogFrame returns the append-only frame under the projection that keeps
// the newest value at an address, which is the arity rule the interval and
// attribute-single frames also run.
func NewLogFrame() Frame {
	return &logFrame{project: projectNewest}
}

// Observe appends. Nothing else happens on a write.
func (f *logFrame) Observe(_ context.Context, t Turn) error {
	for _, written := range t.Written {
		f.events = append(f.events, event{address: written.Address, value: written.Value, at: t.At})
	}
	return nil
}

// Context computes the projection at the address asked about. The whole log
// is read, because a derived view that was stored rather than derived would
// be the addressed store with a log beside it, which is a different
// structure.
func (f *logFrame) Context(_ context.Context, t Turn) (Injected, error) {
	if t.Ask == nil {
		return Injected{}, nil
	}
	var at []event
	for _, e := range f.events {
		if e.address == *t.Ask {
			at = append(at, e)
		}
	}
	values := f.project(at)
	lines := make([]string, 0, len(values))
	for _, value := range values {
		lines = append(lines, line(*t.Ask, value))
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: len(f.events)}, nil
}

// Size counts one unit per event. The log holds what was said, not what is
// true, so a value asserted twice is two events.
func (f *logFrame) Size(context.Context) (Size, error) {
	out := Size{Units: len(f.events)}
	for _, e := range f.events {
		out.ContentBytes += int64(len(line(e.address, e.value)))
	}
	out.StoredBytes = out.ContentBytes
	return out, nil
}

// Held is every event. The memory is derived from them, so what the log
// holds is what it could ever derive.
func (f *logFrame) Held(context.Context) ([]string, error) {
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, line(e.address, e.value))
	}
	return out, nil
}

// Forget drops the events whose line it is given. A log that forgets is no
// longer lossless, which is what a bound costs this structure in
// particular.
func (f *logFrame) Forget(_ context.Context, held []string) error {
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	kept := f.events[:0]
	for _, e := range f.events {
		if !victims[line(e.address, e.value)] {
			kept = append(kept, e)
		}
	}
	f.events = kept
	return nil
}

func (f *logFrame) Close() error { return nil }

// projectNewest keeps the value of the latest event at the address, which
// makes every write a replacement.
func projectNewest(events []event) []string {
	var (
		value string
		at    time.Time
		found bool
	)
	for _, e := range events {
		if !found || !e.at.Before(at) {
			value, at, found = e.value, e.at, true
		}
	}
	if !found {
		return nil
	}
	return []string{value}
}

// projectEvery keeps every value the address was ever written, which makes
// no write a replacement. It is the same log read the other way, and it is
// what "the decision is reversible" means.
func projectEvery(events []event) []string {
	var values []string
	for _, e := range events {
		if !contains(values, e.value) {
			values = append(values, e.value)
		}
	}
	return values
}
