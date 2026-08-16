package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A coexistence assertion must not read as a correction, and a replacement
// must not read as an addition. The two coexistence classes are the same
// requirement in different words, and the whole point of having both is
// that only the wording differs — a template that quietly contradicts what
// came before would make one of them a replacement class instead.
func TestAttributes_WordEachAssertionAsWhatItIs(t *testing.T) {
	contradicting := []string{"instead", "no longer", "not ", "rather than", "stopped"}
	adding := []string{" as well", " too", "also "}

	for _, a := range attributes {
		t.Run(a.name, func(t *testing.T) {
			for _, template := range []string{a.assert, a.also, a.narrow, a.replace} {
				assert.Equal(t, 2, strings.Count(template, "%s"),
					"%q takes a subject and a value", template)
			}
			assert.Equal(t, 1, strings.Count(a.question, "%s"), "%q names the subject alone", a.question)

			for _, word := range contradicting {
				assert.NotContains(t, strings.ToLower(a.also), word,
					"a value that stands beside another must not read as a correction")
				assert.NotContains(t, strings.ToLower(a.narrow), word,
					"a narrower value must not read as a correction")
			}
			for _, word := range adding {
				assert.NotContains(t, strings.ToLower(a.replace), word,
					"a replacement must not read as an addition")
			}
			// A replacement names the value that is true now and never the
			// one it ends, or the stale figure would be counting the
			// workload's own words.
			assert.NotContains(t, strings.ToLower(a.replace), "was ")
		})
	}
}

func TestAttributes_AskForTheValueWithoutSayingIt(t *testing.T) {
	for _, a := range attributes {
		question := fmt.Sprintf(a.question, "Alma")
		fact := fmt.Sprintf(a.assert, "Alma", "amber-quarry")
		assert.False(t, Mentions(question, "amber-quarry"),
			"the question %q gives the answer away", question)
		assert.Contains(t, fact, "amber-quarry")
	}
}

func TestAllValues_AreDistinctPairs(t *testing.T) {
	values := allValues()
	require.Len(t, values, len(valueFirst)*len(valueSecond))

	seen := map[string]bool{}
	for _, v := range values {
		require.False(t, seen[v], "the value %q is in the vocabulary twice", v)
		seen[v] = true
		assert.Equal(t, 1, strings.Count(v, "-"), "a value is one hyphenated pair")
	}
	// No value may contain another as a whole token, or crediting one would
	// credit the other.
	for _, v := range values {
		for _, other := range values {
			if v == other {
				continue
			}
			require.False(t, Mentions(v, other), "%q credits %q", v, other)
		}
	}
}

func TestNoiseThings_AreNotPlantedValues(t *testing.T) {
	values := allValues()
	for _, thing := range noiseThings {
		for _, v := range values {
			require.False(t, Mentions(thing, v), "the noise subject %q credits the value %q", thing, v)
		}
	}
}
