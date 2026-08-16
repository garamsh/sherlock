package main

import (
	"context"
	"strings"

	"github.com/garamsh/gagent/internal/memory"
)

// documentLimit is the document's size in bytes. It is the shipped recall
// token budget converted at the shipped characters-per-token estimate, so
// the two frames put contexts of the same size in front of the model and a
// recall difference between them is not a budget difference.
const documentLimit = memory.DefaultRecallTokenBudget * memory.CharsPerToken

// documentFrame keeps no store: one bounded document, rewritten each turn,
// holding the most recent turns' text and dropping the oldest to stay
// inside its limit. Retrieval does not happen — Context returns the
// document whatever the turn asks.
//
// It is deliberately the most trivial structure that answers the seam, and
// it is here for two reasons. It proves the seam does not encode the shipped
// design's shape: a frame with no rows, no identity, no scoring and no
// query sits behind the same interface. And it is the floor every design
// with a store is read against — a structure that does not beat a recency
// window is not buying anything with its store.
type documentFrame struct {
	limit int
	text  string
}

// NewDocumentFrame returns the bounded-document frame.
func NewDocumentFrame() Frame { return &documentFrame{limit: documentLimit} }

// Observe appends the turn and trims whole lines off the front until the
// document is inside its limit. It reads no extracted fact: the arms differ
// only in what a frame chooses to read, and this one reads nothing but the
// text.
func (f *documentFrame) Observe(_ context.Context, t Turn) error {
	if f.text == "" {
		f.text = t.Text
	} else {
		f.text += "\n" + t.Text
	}
	for len(f.text) > f.limit {
		cut := strings.IndexByte(f.text, '\n')
		if cut < 0 {
			f.text = f.text[len(f.text)-f.limit:]
			break
		}
		f.text = f.text[cut+1:]
	}
	return nil
}

// Context returns the document.
func (f *documentFrame) Context(_ context.Context, _ Turn) (string, error) {
	return f.text, nil
}

// Size reports the one document the frame holds. Nothing is indexed, so
// what it stores is what it holds.
func (f *documentFrame) Size(_ context.Context) (Size, error) {
	units := 0
	if f.text != "" {
		units = 1
	}
	return Size{Units: units, ContentBytes: int64(len(f.text)), StoredBytes: int64(len(f.text))}, nil
}

func (f *documentFrame) Close() error { return nil }
