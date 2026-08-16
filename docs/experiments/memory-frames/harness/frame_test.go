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
	_, err := NewFrame("nothing-by-that-name", t.TempDir(), NewEmbedCounter())
	assert.Error(t, err)
}

// The seam has to hold a structure that keeps no rows, or it has encoded
// the shape of the one this project ships. The document frame is that
// structure: one bounded text, no identity, no scoring, no query.
func TestDocumentFrame_KeepsOneBoundedDocumentAndNoRows(t *testing.T) {
	frame := NewDocumentFrame()
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
	assert.Contains(t, injected, "turn number 399", "the newest turn is in the document")
	assert.NotContains(t, injected, "turn number 0 ", "the oldest turn has been dropped")
	assert.LessOrEqual(t, len(injected), documentLimit)
}

func TestDocumentFrame_HoldsNothingBeforeItObservesAnything(t *testing.T) {
	frame := NewDocumentFrame()
	size, err := frame.Size(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Size{}, size)
}

func TestShippedFrame_ReturnsWhatItObserved(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir(), NewEmbedCounter())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	fact := Fact{Address: Address{Subject: "Alma", Attribute: "project"}, Value: "amber-quarry"}
	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project.", Fact: &fact}))

	injected, err := frame.Context(ctx, Turn{Index: 1, Day: 0, At: start.Add(time.Hour),
		Text: "Which project does Alma lead?"})
	require.NoError(t, err)
	assert.True(t, Mentions(injected, "amber-quarry"))
}

// A turn carrying no extracted fact is an episodic observation, and the
// shipped tiered retention drops one once it is older than the retention
// window. A turn carrying a fact is a semantic add and is kept
// indefinitely. Both come from internal/memory's own constants, and the
// difference between them is what the two arms bracket.
func TestShippedFrame_KeepsAFactAndLetsAnObservationExpire(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir(), NewEmbedCounter())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	fact := Fact{Address: Address{Subject: "Alma", Attribute: "project"}, Value: "amber-quarry"}
	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project.", Fact: &fact}))
	require.NoError(t, frame.Observe(ctx, Turn{Index: 1, Day: 0, At: start.Add(time.Minute),
		Text: "Otto asked about the printer again."}))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, size.Units, "both are active on the day they were said")

	require.NoError(t, frame.Observe(ctx, Turn{Index: 2, Day: 90, At: start.AddDate(0, 0, 90),
		Text: "Otto asked about the kettle again."}))
	size, err = frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, size.Units, "the fact and the newest observation are active; the expired one is not")

	injected, err := frame.Context(ctx, Turn{Index: 3, Day: 90, At: start.AddDate(0, 0, 90),
		Text: "Which project does Alma lead?"})
	require.NoError(t, err)
	assert.True(t, Mentions(injected, "amber-quarry"), "a fact survives the retention pass")
}

func TestShippedFrame_ReportsMoreStoredThanHeld(t *testing.T) {
	frame, err := NewShippedFrame(t.TempDir(), NewEmbedCounter())
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		require.NoError(t, frame.Observe(ctx, turnAt(i, fmt.Sprintf("Alma leads the amber-%d project.", i))))
	}
	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Positive(t, size.Units)
	assert.Positive(t, size.ContentBytes)
	assert.Greater(t, size.StoredBytes, size.ContentBytes,
		"a row costs an embedding and an index besides its text")
}

func TestEmbedCounter_CountsTheCallsAndTheDistinctTexts(t *testing.T) {
	counter := NewEmbedCounter()
	frame, err := NewShippedFrame(t.TempDir(), counter)
	require.NoError(t, err)
	defer frame.Close()
	ctx := context.Background()

	require.NoError(t, frame.Observe(ctx, turnAt(0, "Alma leads the amber-quarry project.")))
	require.NoError(t, frame.Observe(ctx, turnAt(1, "Alma leads the amber-quarry project.")))

	calls, distinct, tokens := counter.Totals()
	assert.Positive(t, calls)
	assert.Equal(t, 1, distinct, "the same text is only ever paid for once")
	assert.Positive(t, tokens)
	assert.Greater(t, calls, distinct)
}

// Ask reaches every frame, and a frame that does not address facts ignores
// it. That is the shipped design's own choice — its read path takes a task
// statement — rather than a handicap the harness imposes, and it is why the
// figures this record already carried did not move when the read side of
// the seam was extended.
func TestFrames_ThatDoNotAddressFactsIgnoreTheAddressAsked(t *testing.T) {
	ctx := context.Background()
	address := Address{Subject: "Alma", Attribute: "project"}
	for _, name := range []string{frameShipped, frameDocument} {
		t.Run(name, func(t *testing.T) {
			frame, err := NewFrame(name, t.TempDir(), NewEmbedCounter())
			require.NoError(t, err)
			defer frame.Close()

			fact := Fact{Address: address, Value: "amber-quarry"}
			require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
				Text: "Alma leads the amber-quarry project.", Fact: &fact}))

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
