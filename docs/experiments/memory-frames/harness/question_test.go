package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The round is worthless if forming an address needs the answer. These are
// the checks that it does not, and they are mechanical rather than a claim
// in the record's prose: the address is re-derived after the workload's
// ground truth has been inverted, and it has to come out the same.
//
// The first of them passes today because QuestionAddress takes a sentence, a
// subject and a value and nothing else, which is the point. It is here so
// that widening what an address may read fails a test rather than quietly
// making the round circular.

func TestQuestionAddress_DoesNotMoveWhenTheGroundTruthIsInverted(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	require.NoError(t, err)

	before := map[string]Address{}
	for _, thread := range w.Threads {
		for i, a := range thread.Asserts {
			before[fmt.Sprintf("%s|%d", thread.Subject, i)+a.Value] =
				QuestionAddress(a.Text, thread.Subject, a.Value, thread.Attribute)
		}
	}
	require.NotEmpty(t, before)

	for i := range w.Threads {
		thread := &w.Threads[i]
		thread.Class = classFresh
		for j := range thread.Asserts {
			thread.Asserts[j].Replaces = !thread.Asserts[j].Replaces
			thread.Asserts[j].Wording = wordingAssert
		}
	}
	for _, thread := range w.Threads {
		for i, a := range thread.Asserts {
			key := fmt.Sprintf("%s|%d", thread.Subject, i) + a.Value
			assert.Equal(t, before[key],
				QuestionAddress(a.Text, thread.Subject, a.Value, thread.Attribute),
				"the address moved with the answer, so the arity it carries came from the oracle")
		}
	}
}

func TestQuestionAddress_TakesEveryRestrictionFromWordsTheSentenceCarries(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	require.NoError(t, err)

	var restricted int
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			at := QuestionAddress(a.Text, thread.Subject, a.Value, thread.Attribute)
			if at.Qualifier == "" {
				continue
			}
			restricted++
			assert.Contains(t, strings.ToLower(a.Text), at.Qualifier,
				"a restriction the sentence does not carry is a restriction the address invented")
		}
	}
	assert.Positive(t, restricted, "no restriction was read at all, so the rule is untested")
}

// The four wordings differ in what they say about the values already at an
// address, and only one of them narrows the question. That is the whole of
// what the addressing changes, so it is checked template by template rather
// than in aggregate.
func TestQuestionAddress_RestrictsOnANarrowingSentenceAndOnNoOther(t *testing.T) {
	const (
		subject = "Maria"
		value   = "amber-quarry"
	)
	for _, a := range attributes {
		for _, tc := range []struct {
			wording  string
			template string
			restrict bool
		}{
			{wordingAssert, a.assert, false},
			{wordingAlso, a.also, false},
			{wordingReplace, a.replace, false},
			{wordingNarrow, a.narrow, true},
		} {
			t.Run(a.name+" "+tc.wording, func(t *testing.T) {
				text := fmt.Sprintf(tc.template, subject, value)
				at := QuestionAddress(text, subject, value, a.name)

				assert.Equal(t, questionStems[a.name], at.Attribute,
					"the address names the question the checkpoint asks")
				if tc.restrict {
					assert.NotEmpty(t, at.Qualifier,
						"a sentence that narrows the question must narrow the address")
					return
				}
				assert.Empty(t, at.Qualifier,
					"a sentence answering the same question must land at the same address")
			})
		}
	}
}

// A framed or packed message puts other words around an assertion, and a
// restriction read out of those words would be a restriction the generator
// wrote rather than the workload. Losing one is a cost the record reports;
// gaining one would be reading the generator, so this ranges over the lists
// the generator draws from rather than over a copy of them.
func TestQuestionAddress_FramingNeverAddsARestriction(t *testing.T) {
	const (
		subject = "Maria"
		value   = "amber-quarry"
	)
	for _, a := range attributes {
		plain := fmt.Sprintf(a.assert, subject, value)
		for kind, texts := range framingTexts() {
			for _, framing := range texts {
				t.Run(a.name+" "+kind, func(t *testing.T) {
					framed := fmt.Sprintf(framing, clause(plain))
					assert.Empty(t, QuestionAddress(framed, subject, value, a.name).Qualifier,
						"framing added a restriction the assertion never carried")
				})
			}
		}
	}
}

func TestNumberOf_ReadsTheInterrogativeAndNotTheRestOfTheQuestion(t *testing.T) {
	tests := []struct {
		name string
		stem string
		want questionArity
	}{
		{"a determiner with a singular head", "which-project-does-lead", questionSingular},
		{"a determiner with a plural head", "what-languages-does-speak", questionPlural},
		{"a bare interrogative", "where-does-rehearse", questionUnmarked},
		{"an interrogative followed by an auxiliary", "what-does-keep-the-accounts-in", questionUnmarked},
		{"a head noun ending in a double s", "which-address-does-use", questionSingular},
		{"one word", "where", questionUnmarked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, numberOf(test.stem))
		})
	}
}

func TestQuestionFrame_AnswersAQuestionWithWhatANarrowerOneHolds(t *testing.T) {
	ctx := context.Background()
	frame := NewQuestionFrame(arityQuestion)
	t.Cleanup(func() { require.NoError(t, frame.Close()) })

	const (
		subject   = "Maria"
		attribute = "project"
		wide      = "amber-quarry"
		narrow    = "copper-lantern"
	)
	for i, text := range []string{
		fmt.Sprintf(attributes[0].assert, subject, wide),
		fmt.Sprintf(attributes[0].narrow, subject, narrow),
	} {
		value := wide
		if i == 1 {
			value = narrow
		}
		require.NoError(t, frame.Observe(ctx, Turn{At: time.Now().Add(time.Duration(i) * time.Hour), Text: text,
			Written: []Written{{Fact: Fact{
				Address: QuestionAddress(text, subject, value, attribute), Value: value}}}}))
	}

	asked := Address{Subject: subject, Attribute: questionStems[attribute]}
	injected, err := frame.Context(ctx, Turn{Ask: &asked})
	require.NoError(t, err)

	assert.True(t, Mentions(injected.Text, wide), "the question's own value is missing")
	assert.True(t, Mentions(injected.Text, narrow),
		"a value written at a narrower question is unreachable from the question it narrows")
}

// A question naming a subject and no attribute reaches every address under
// that subject, and a restricted question is one of them. A walk by
// attribute name would return the unrestricted address twice and the
// restricted one never, which is what `addressesUnder` exists to prevent.
func TestQuestionFrame_AnswersAPartialAddressFromItsRestrictedAddressesToo(t *testing.T) {
	ctx := context.Background()
	frame := NewQuestionFrame(arityQuestion)
	t.Cleanup(func() { require.NoError(t, frame.Close()) })

	const (
		subject   = "Maria"
		attribute = "project"
		wide      = "amber-quarry"
		narrow    = "copper-lantern"
		elsewhere = "cobalt-ridge"
	)
	written := []struct {
		text      string
		attribute string
		value     string
	}{
		{fmt.Sprintf(attributes[0].assert, subject, wide), attribute, wide},
		{fmt.Sprintf(attributes[0].narrow, subject, narrow), attribute, narrow},
		{fmt.Sprintf(attributes[1].assert, subject, elsewhere), attributes[1].name, elsewhere},
	}
	for i, one := range written {
		require.NoError(t, frame.Observe(ctx, Turn{At: time.Now().Add(time.Duration(i) * time.Hour),
			Text: one.text, Written: []Written{{Fact: Fact{
				Address: QuestionAddress(one.text, subject, one.value, one.attribute),
				Value:   one.value}}}}))
	}

	injected, err := frame.Context(ctx, Turn{Ask: &Address{Subject: subject}})
	require.NoError(t, err)

	for _, value := range []string{wide, narrow, elsewhere} {
		assert.True(t, Mentions(injected.Text, value),
			"a question about the whole subject did not reach %q", value)
	}
}

func TestMeasureQuestions_PutsTheResidualWhereASecondValueAnswersTheSameQuestion(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	require.NoError(t, err)

	measured := MeasureQuestions(w)

	assert.Less(t, measured.QuestionWrong, measured.TopicWrong,
		"a question-shaped address that carries no more arity than a topic one has bought nothing")
	assert.Positive(t, measured.Qualified, "no address was restricted, so the two addressings are the same")

	wrongByClass := map[string]int{}
	for _, cut := range measured.Residual {
		if cut.Cut == "class" {
			wrongByClass[cut.Name] = cut.Wrong
		}
	}
	assert.Equal(t, 0, wrongByClass[classRefined],
		"a narrowing statement asks its own question, so its address holds one value")
	assert.Positive(t, wrongByClass[classParallel],
		"a conjunction adds a value answering the question already answered, and no address form separates that")
	for _, class := range []string{classCarried, classReplaced, classReverted, classFresh} {
		assert.Equal(t, 0, wrongByClass[class], "%s holds one value at a time", class)
	}
}

// decadeParams is the decade run's shape, which is what the addressing has
// to be read on: a smaller workload would not plant every class.
func decadeParams() Params {
	return Params{Years: 10, SessionsPerYear: 36, TurnsPerSession: 16, ThreadsPerClass: 40}
}
