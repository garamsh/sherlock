package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The design's two decisions are separate, and this is where they separate:
// the declaration says whether the write ends what is there, and the
// retrieval says which memory it ends. A gate the similarity does not clear
// leaves the write with nothing to update, so it adds.
func TestMem0Frame_AppliesTheDeclarationToWhatTheGateAccepts(t *testing.T) {
	tests := []struct {
		name  string
		build func(*EmbedCounter) Frame
		op    Operation
		holds []string
	}{
		{"a declared replacement updates the nearest candidate", NewMem0NearestFrame,
			OperationReplace, []string{"copper-lantern"}},
		{"a declared join adds beside it", NewMem0NearestFrame,
			OperationJoin, []string{"amber-quarry", "copper-lantern"}},
		{"a gate the similarity does not clear turns a replacement into an add", NewMem0Frame,
			OperationReplace, []string{"amber-quarry", "copper-lantern"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			frame := tc.build(NewEmbedCounter())
			require.NoError(t, frame.Observe(ctx, stating(wrote(0, almaProject, "amber-quarry", "first."), OperationReplace)))
			require.NoError(t, frame.Observe(ctx, stating(wrote(400, almaProject, "copper-lantern", "second."), tc.op)))

			injected, err := frame.Context(ctx, asked(800, almaProject))
			require.NoError(t, err)
			for _, value := range []string{"amber-quarry", "copper-lantern"} {
				assert.Equal(t, contains(tc.holds, value), Mentions(injected.Text, value),
					"%q in %q", value, injected.Text)
			}
			size, err := frame.Size(ctx)
			require.NoError(t, err)
			assert.Equal(t, len(tc.holds), size.Units)
		})
	}
}

// The failure the gate exists to prevent, measured rather than argued: with
// no gate, a write that declares a replacement ends whatever is nearest,
// including a memory about something else entirely.
func TestNewMem0NearestFrame_EndsAnUnrelatedMemoryWhereTheAddressIsNew(t *testing.T) {
	ctx := context.Background()
	frame := NewMem0NearestFrame(NewEmbedCounter())
	otto := Address{Subject: "Otto", Attribute: "rota"}

	require.NoError(t, frame.Observe(ctx, stating(wrote(0, almaProject, "amber-quarry", "first."), OperationReplace)))
	require.NoError(t, frame.Observe(ctx, stating(wrote(1, otto, "copper-lantern", "second."), OperationReplace)))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, size.Units, "the second write ended the first, which was about another subject")
}

// The design's first phase is a model reading the message. A frame handed no
// extraction has no candidate fact, so it holds nothing — which is what its
// text-arm rows say.
func TestMem0Frame_HoldsNothingWithoutAnExtraction(t *testing.T) {
	ctx := context.Background()
	for name, frame := range map[string]Frame{
		"gated":   NewMem0Frame(NewEmbedCounter()),
		"nearest": NewMem0NearestFrame(NewEmbedCounter()),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, frame.Observe(ctx, observedAt(0, "Alma leads the amber-quarry project.")))
			size, err := frame.Size(ctx)
			require.NoError(t, err)
			assert.Equal(t, Size{}, size)
		})
	}
}

// A value written again is held once, whatever the declaration says: the
// no-op is the fourth operation the design has and the one a repeated
// assertion reaches.
func TestMem0Frame_HoldsAValueWrittenTwiceOnce(t *testing.T) {
	ctx := context.Background()
	frame := NewMem0Frame(NewEmbedCounter())

	require.NoError(t, frame.Observe(ctx, stating(wrote(0, almaProject, "amber-quarry", "first."), OperationReplace)))
	require.NoError(t, frame.Observe(ctx, stating(wrote(400, almaProject, "amber-quarry", "again."), OperationJoin)))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, size.Units)
}
