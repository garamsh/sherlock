package main

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shapeParams is the shape study's own size, which is what the shapes have
// to be compared at: a smaller workload would not plant every class.
func shapeParams(shape string) Params { return harderParams(shape) }

// fixedRand is the generator the shape helpers draw their framing from,
// seeded so a test reads the same message twice.
func fixedRand() *rand.Rand { return rand.New(rand.NewSource(7)) }

// framingTexts is every fixed phrase a shape wraps an assertion in, with
// the assertion itself removed. What the cue rule reads is the whole
// message, so a marker anywhere in the framing is a marker on every
// assertion the message carries.
func framingTexts() map[string][]string {
	return map[string][]string{
		"clausal frame": clausalFrames,
		"packed frame":  packedFrames,
		"packed join":   packedJoins,
	}
}

func TestShapeFramingMarksNoAddition(t *testing.T) {
	for kind, texts := range framingTexts() {
		for _, text := range texts {
			t.Run(kind+" "+text[:min(24, len(text))], func(t *testing.T) {
				assert.False(t, marksAddition(strings.ReplaceAll(text, "%s", "")),
					"framing carrying an addition cue turns every replacement in the message into a join")
			})
		}
	}
}

func TestHardenKeepsWhetherAnAssertionMarksAnAddition(t *testing.T) {
	plain, err := GenerateWorkload(7, shapeParams(shapePlain))
	require.NoError(t, err)
	clausal, err := GenerateWorkload(7, shapeParams(shapeClausal))
	require.NoError(t, err)

	marked := map[string]bool{}
	for _, thread := range plain.Threads {
		for _, a := range thread.Asserts {
			marked[a.Value] = marksAddition(a.Text)
		}
	}
	require.NotEmpty(t, marked)
	for _, thread := range clausal.Threads {
		for _, a := range thread.Asserts {
			assert.Equal(t, marked[a.Value], marksAddition(a.Text),
				"framing %q changed what the cue rule reads out of the assertion", a.Value)
		}
	}
}

func TestHardenLeavesThePlainShapeAlone(t *testing.T) {
	const sentence = "Alma works on amber-quarry."
	assert.Equal(t, sentence, harden(fixedRand(), shapePlain, sentence),
		"the plain shape is the decade run's sentences, so hardening it would move that baseline")
}

func TestPackDoesNotDoubleAPeriod(t *testing.T) {
	sentences := []string{
		"Alma works on amber-quarry.",
		"Bo answers to copper-lantern.",
		"Cyd is allergic to silver-thistle.",
	}
	packed := pack(fixedRand(), sentences)

	assert.Equal(t, 1, strings.Count(packed, "."), "a packed message ends once")
	assert.True(t, strings.HasSuffix(packed, "."), "a packed message ends in a period")
	for _, sentence := range sentences {
		assert.Contains(t, packed, clause(sentence), "every assertion survives packing")
	}
}

func TestPackLeavesALoneSentenceUntouched(t *testing.T) {
	const sentence = "Alma works on amber-quarry."
	assert.Equal(t, sentence, pack(fixedRand(), []string{sentence}),
		"a shape putting one assertion in a message must produce the sentence itself")
}

func TestMeasureLanguageOrdersTheShapes(t *testing.T) {
	measured := map[string]Language{}
	for _, shape := range shapeNames {
		w, err := GenerateWorkload(7, shapeParams(shape))
		require.NoError(t, err)
		measured[shape] = MeasureLanguage(w)
	}

	plain, clausal, packed := measured[shapePlain], measured[shapeClausal], measured[shapePacked]
	assert.Less(t, plain.Clauses.Mean, clausal.Clauses.Mean, "a subordinate frame embeds clauses")
	assert.Less(t, clausal.Clauses.Mean, packed.Clauses.Mean, "several assertions in a message embed more")
	assert.Less(t, plain.Chars.Mean, clausal.Chars.Mean)
	assert.Less(t, clausal.Chars.Mean, packed.Chars.Mean)

	assert.Equal(t, plain.PerMessage.Mean, clausal.PerMessage.Mean,
		"clausal varies the sentence and not the number of decisions in it")
	assert.Equal(t, float64(packedAssertions), packed.PerMessage.Mean,
		"packed varies the number of decisions and not the sentence")
	assert.Equal(t, plain.Assertions, packed.Assertions,
		"the shapes share their threads and their schedule, so they assert the same things")
}

func TestCountClausesReadsPunctuationAndSubordinators(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"one clause", "Alma works on amber-quarry.", 1},
		{"a comma", "Alma works on amber-quarry, apparently.", 2},
		{"a semicolon", "Alma works on amber-quarry; Bo does not.", 2},
		{"a subordinator", "Alma works on amber-quarry since the review.", 2},
		{"a subordinator inside a word", "Alma sincerely works on amber-quarry.", 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, countClauses(test.text))
		})
	}
}
