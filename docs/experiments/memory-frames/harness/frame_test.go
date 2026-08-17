package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func turnAt(index int, text string) Turn {
	return Turn{Index: index, Day: index, At: start.AddDate(0, 0, index), Text: text}
}

func TestNewFrame_RejectsAnUnknownName(t *testing.T) {
	_, err := NewFrame("nothing-by-that-name", t.TempDir(), NewEmbedCounter(), 0)
	assert.Error(t, err)
}

// The seam has to hold a structure that keeps no rows, or it has encoded
// the shape of the one this project ships. The document frame is that
// structure: one bounded text, no identity, no scoring, no query.
func TestDocumentFrame_KeepsOneBoundedDocumentAndNoRows(t *testing.T) {
	frame := NewDocumentFrame(0)
	ctx := context.Background()

	for i := 0; i < 400; i++ {
		require.NoError(t, frame.Observe(ctx, turnAt(i, fmt.Sprintf("turn number %d of the conversation.", i))))
	}
	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, size.Units, "the frame holds one document however many turns it saw")
	assert.LessOrEqual(t, size.ContentBytes, int64(documentLimit))
	assert.Equal(t, size.ContentBytes, size.StoredBytes, "nothing is indexed, so held and stored are the same")

	injected, err := frame.Context(ctx, turnAt(400, "what happened?"))
	require.NoError(t, err)
	assert.Contains(t, injected.Text, "turn number 399", "the newest turn is in the document")
	assert.NotContains(t, injected.Text, "turn number 0 ", "the oldest turn has been dropped")
	assert.LessOrEqual(t, len(injected.Text), documentLimit)
}

func TestDocumentFrame_HoldsNothingBeforeItObservesAnything(t *testing.T) {
	frame := NewDocumentFrame(0)
	size, err := frame.Size(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Size{}, size)
}

func TestShippedFrame_ReturnsWhatItObserved(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	address := Address{Subject: "Alma", Attribute: "project"}
	fact := Fact{Address: address, Value: "amber-quarry"}
	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project.", Written: []Written{{Fact: fact}}}))

	injected, err := frame.Context(ctx, Turn{Index: 1, Day: 0, At: start.Add(time.Hour),
		Text: "Which project does Alma lead?", Ask: &address})
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"))
	assert.Equal(t, 1, injected.Scanned,
		"an addressed store reads the address asked, not the index")
}

// The store holds an address and a value, so a turn asserting nothing is
// not written: there is nowhere to put a sentence it cannot address
// (ADR 0018). What the sentence store kept as an episodic observation and
// let the retention window expire, this one never holds at all.
func TestShippedFrame_HoldsOnlyWhatWasAddressed(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	address := Address{Subject: "Alma", Attribute: "project"}
	fact := Fact{Address: address, Value: "amber-quarry"}
	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project.", Written: []Written{{Fact: fact}}}))
	require.NoError(t, frame.Observe(ctx, Turn{Index: 1, Day: 0, At: start.Add(time.Minute),
		Text: "Otto asked about the printer again."}))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, size.Units, "the addressed fact is held; the turn asserting nothing is not")

	require.NoError(t, frame.Observe(ctx, Turn{Index: 2, Day: 90, At: start.AddDate(0, 0, 90),
		Text: "Otto asked about the kettle again."}))
	injected, err := frame.Context(ctx, Turn{Index: 3, Day: 90, At: start.AddDate(0, 0, 90),
		Text: "Which project does Alma lead?", Ask: &address})
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"),
		"a fact survives the retention pass a decade later")
}

// A write declaring a replacement supersedes what the address held; a
// write declaring a join leaves it. That is the whole of how the store
// decides identity — nothing compares the two values (ADR 0018).
func TestShippedFrame_ReplaceSupersedesAndJoinCoexists(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	address := Address{Subject: "Alma", Attribute: "project"}
	write := func(index int, value string, op Operation) {
		require.NoError(t, frame.Observe(ctx, Turn{Index: index, Day: index, At: start.AddDate(0, 0, index),
			Written: []Written{{Fact: Fact{Address: address, Value: value}, Declared: op}}}))
	}
	write(0, "amber-quarry", OperationJoin)
	write(1, "basalt-run", OperationJoin)

	held, err := frame.Held(ctx)
	require.NoError(t, err)
	assert.Len(t, held, 2, "a join leaves what the address holds and adds one")

	write(2, "cinder-lake", OperationReplace)
	held, err = frame.Held(ctx)
	require.NoError(t, err)
	require.Len(t, held, 1, "a replacement supersedes what the address held")
	assert.True(t, Mentions(held[0], "cinder-lake"))
}

func TestShippedFrame_ReportsMoreStoredThanHeld(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		require.NoError(t, frame.Observe(ctx, Turn{Index: i, Day: i, At: start.AddDate(0, 0, i),
			Written: []Written{{Fact: Fact{
				Address: Address{Subject: "Alma", Attribute: fmt.Sprintf("project-%d", i)},
				Value:   fmt.Sprintf("amber-%d", i)}}}}))
	}
	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Positive(t, size.Units)
	assert.Positive(t, size.ContentBytes)
	assert.Greater(t, size.StoredBytes, size.ContentBytes,
		"a row costs the address index and the page it sits on besides its text")
}

// The counter is what every frame that embeds is wired through. The
// shipped store is not one of them since #582 — it computes where a fact
// is instead of scoring what it looks like — so what it counts is the
// ported designs that model similarity.
func TestEmbedCounter_CountsTheCallsAndTheDistinctTexts(t *testing.T) {
	counter := NewEmbedCounter()
	embedder := counter.Wrap(NewHashingEmbedder())
	ctx := context.Background()

	_, err := embedder.Embed(ctx, "Alma leads the amber-quarry project.")
	require.NoError(t, err)
	_, err = embedder.Embed(ctx, "Alma leads the amber-quarry project.")
	require.NoError(t, err)

	calls, distinct, tokens := counter.Totals()
	assert.Equal(t, 2, calls)
	assert.Equal(t, 1, distinct, "the same text is only ever paid for once")
	assert.Positive(t, tokens)
	assert.Greater(t, calls, distinct)
}

// Ask reaches every frame, and a frame that does not address facts ignores
// it. That is each design's own choice — the shipped read path and the
// three memory streams' both take a task statement — rather than a handicap
// the harness imposes, and it is why the figures this record already
// carried did not move when the read side of the seam was extended.
//
// It is also why these frames post one row under every question form: a
// structure that never reads an address cannot be told which form supplied
// one.
func TestFrames_ThatDoNotAddressFactsIgnoreTheAddressAsked(t *testing.T) {
	ctx := context.Background()
	address := Address{Subject: "Alma", Attribute: "project"}
	for _, name := range []string{frameDocument,
		frameGenerativeAgents, frameReflection, frameMemoryBank} {
		t.Run(name, func(t *testing.T) {
			frame, err := NewFrame(name, t.TempDir(), NewEmbedCounter(), 0)
			require.NoError(t, err)
			defer frame.Close()

			fact := Fact{Address: address, Value: "amber-quarry"}
			require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
				Text: "Alma leads the amber-quarry project.", Written: []Written{{Fact: fact}}}))

			question := Turn{Index: 1, Day: 1, At: start.AddDate(0, 0, 1),
				Text: "Which project does Alma lead?"}
			without, err := frame.Context(ctx, question)
			require.NoError(t, err)
			question.Ask = &address
			with, err := frame.Context(ctx, question)
			require.NoError(t, err)

			assert.Equal(t, without, with)
		})
	}
}
