package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wrote returns the turn that writes a value to an address, said in the
// given words.
func wrote(day int, address Address, value, text string) Turn {
	fact := Fact{Address: address, Value: value}
	return Turn{Index: day, Day: day, At: start.AddDate(0, 0, day), Text: text, Fact: &fact}
}

// asked returns the turn that questions an address.
func asked(day int, address Address) Turn {
	return Turn{Index: day, Day: day, At: start.AddDate(0, 0, day),
		Text: "what is it?", Ask: &address}
}

var almaProject = Address{Subject: "Alma", Attribute: "project"}

// The three write-time rules differ in what an address holds after a second
// value is written to it, and in nothing else.
func TestAttributeFrame_HoldsWhatItsArityRuleAllows(t *testing.T) {
	tests := []struct {
		name  string
		rule  arityRule
		text  string
		holds []string
	}{
		{"single replaces whatever was there", aritySingle, "Alma leads the copper-lantern project from now on.",
			[]string{"copper-lantern"}},
		{"single replaces even when the words add", aritySingle, "Alma leads the copper-lantern project as well.",
			[]string{"copper-lantern"}},
		{"multi keeps both however the words read", arityMulti, "Alma leads the copper-lantern project from now on.",
			[]string{"amber-quarry", "copper-lantern"}},
		{"cued replaces when nothing marks an addition", arityCued, "Alma leads the copper-lantern project from now on.",
			[]string{"copper-lantern"}},
		{"cued keeps both when the words mark an addition", arityCued, "Alma leads the copper-lantern project as well.",
			[]string{"amber-quarry", "copper-lantern"}},
		{"cued reads a qualifier as a replacement", arityCued, "On Thursdays Alma leads the copper-lantern project.",
			[]string{"copper-lantern"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame := NewAttributeFrame(tc.rule)
			ctx := context.Background()
			require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry",
				"Alma leads the amber-quarry project.")))
			require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", tc.text)))

			injected, err := frame.Context(ctx, asked(800, almaProject))
			require.NoError(t, err)
			for _, value := range []string{"amber-quarry", "copper-lantern"} {
				assert.Equal(t, contains(tc.holds, value), Mentions(injected, value),
					"%q in %q", value, injected)
			}
			size, err := frame.Size(ctx)
			require.NoError(t, err)
			assert.Equal(t, len(tc.holds), size.Units)
		})
	}
}

// A value written again is the value that is live, and it is held once
// however many times it is said.
func TestAttributeFrame_HoldsAValueWrittenTwiceOnce(t *testing.T) {
	ctx := context.Background()
	for _, rule := range []arityRule{aritySingle, arityMulti, arityLearned, arityCued} {
		t.Run(string(rule), func(t *testing.T) {
			frame := NewAttributeFrame(rule)
			require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
			require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))
			require.NoError(t, frame.Observe(ctx, wrote(800, almaProject, "amber-quarry", "back again.")))

			size, err := frame.Size(ctx)
			require.NoError(t, err)
			assert.LessOrEqual(t, size.Units, 2)
			injected, err := frame.Context(ctx, asked(900, almaProject))
			require.NoError(t, err)
			assert.True(t, Mentions(injected, "amber-quarry"), "the value written last is live: %q", injected)
		})
	}
}

// The learned rule keeps every value and decides at read time, so it turns
// an attribute multi-valued for every address under it as soon as one
// address anywhere under that attribute has been written twice.
func TestAttributeFrame_LearnedTurnsAnAttributeMultiValuedFromOneAddress(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(arityLearned)
	otto := Address{Subject: "Otto", Attribute: "project"}

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(1, otto, "ironwood-annex", "otto's first.")))

	injected, err := frame.Context(ctx, asked(2, otto))
	require.NoError(t, err)
	assert.True(t, Mentions(injected, "ironwood-annex"))

	require.NoError(t, frame.Observe(ctx, wrote(400, otto, "silverfin-ridge", "otto's second.")))
	injected, err = frame.Context(ctx, asked(401, otto))
	require.NoError(t, err)
	assert.True(t, Mentions(injected, "ironwood-annex"),
		"one address written twice makes the attribute multi-valued for every address under it")
	assert.True(t, Mentions(injected, "silverfin-ridge"))
}

// The learned rule cannot recover a value it never kept, which is why it
// stores what multi stores rather than what single stores.
func TestAttributeFrame_LearnedStoresWhatMultiStores(t *testing.T) {
	ctx := context.Background()
	sizes := map[arityRule]int{}
	for _, rule := range []arityRule{aritySingle, arityMulti, arityLearned} {
		frame := NewAttributeFrame(rule)
		require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
		require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))
		size, err := frame.Size(ctx)
		require.NoError(t, err)
		sizes[rule] = size.Units
	}
	assert.Equal(t, 1, sizes[aritySingle])
	assert.Equal(t, sizes[arityMulti], sizes[arityLearned])
}

// Without an extracted fact the frame has nowhere to write and nothing to
// look up, which is the whole of what it does in the text arm.
func TestAttributeFrame_HoldsNothingItCannotAddress(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(aritySingle)

	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project."}))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, Size{}, size)

	injected, err := frame.Context(ctx, Turn{Index: 1, Day: 0, At: start, Text: "Which project does Alma lead?"})
	require.NoError(t, err)
	assert.Empty(t, injected)
}

// An address answers with what is at it and with nothing else: a frame that
// goes to one place does not fill a budget.
func TestAttributeFrame_AnswersFromTheAddressAlone(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(arityMulti)
	other := Address{Subject: "Otto", Attribute: "clinic"}

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(1, other, "copper-lantern", "otto's.")))

	injected, err := frame.Context(ctx, asked(2, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected, "amber-quarry"))
	assert.False(t, Mentions(injected, "copper-lantern"), "another address is not returned")

	injected, err = frame.Context(ctx, asked(3, Address{Subject: "Nobody", Attribute: "project"}))
	require.NoError(t, err)
	assert.Empty(t, injected, "an address never written answers with nothing")
}

func TestMarksAddition_ReadsCuesAsWholeWords(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"Alma leads the copper-lantern project as well.", true},
		{"Alma goes to the copper-lantern clinic too.", true},
		{"Alma leads the copper-lantern project from now on.", false},
		{"On Thursdays Alma leads the copper-lantern project.", false},
		{"Alma took the copper-lantern route.", false},
		{"Alma also leads the copper-lantern project.", true},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			assert.Equal(t, tc.want, marksAddition(tc.text))
		})
	}
}

func TestNewest_ReturnsTheValueWrittenLast(t *testing.T) {
	values := []heldValue{
		{value: "a", written: start},
		{value: "b", written: start.Add(48 * time.Hour)},
		{value: "c", written: start.Add(24 * time.Hour)},
	}
	assert.Equal(t, "b", newest(values).value)
}
