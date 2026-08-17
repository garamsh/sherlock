package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
)

// TestAskedProse_AsksEveryAssertionWithItsOwnProbe pins the pairing this
// round rests on: the question a fact is asked with is the workload's own,
// carried by the same attribute that wrote the sentence, so nothing here
// decides which probe belongs to which fact.
func TestAskedProse_AsksEveryAssertionWithItsOwnProbe(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	asked := AskedProse(w)

	var assertions int
	for _, thread := range w.Threads {
		assertions += len(thread.Asserts)
	}
	require.Equal(t, assertions, len(asked),
		"every assertion is asked; a sample would let a class go under-represented")

	byText := map[string]ProseWrite{}
	for _, a := range asked {
		byText[a.Text] = a
	}
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			got, ok := byText[a.Text]
			require.True(t, ok, "assertion %q was not asked", a.Text)
			assert.Equal(t, thread.Question, got.Probe,
				"the fact and its probe come from one attribute")
			assert.Equal(t, thread.Subject, got.Subject)
			assert.Equal(t, a.Value, got.Value)
		}
	}
}

// TestAskedProseReads_AsksEachProbeOnce keeps the read half from paying
// for the same question once per subject that shares it.
func TestAskedProseReads_AsksEachProbeOnce(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	reads := AskedProseReads(w)

	seen := map[string]bool{}
	for _, r := range reads {
		assert.False(t, seen[r.Probe], "probe %q asked twice", r.Probe)
		seen[r.Probe] = true
	}
	for _, thread := range w.Threads {
		assert.True(t, seen[thread.Question], "probe %q never asked", thread.Question)
	}
}

// TestScoreProse_SeparatesWellFormedFromFindable is the round's own
// acceptance test, and it encodes the requirement #547 states: a score
// that can be high while every fact is unreachable is the wrong score.
//
// The rows below are the shape of the observation that opened the issue —
// "Say hi. My cat is named Mochi." returning subject "Mochi" and value
// "Mochi". Every row parses, so a harness reporting conformance would call
// this a clean run. What the round has to report instead is that the
// address holds its own answer and no probe reaches it.
func TestScoreProse_SeparatesWellFormedFromFindable(t *testing.T) {
	run := ProseRun{
		Writes: []ProseWrite{
			{
				Subject: "Zara", Value: "verdigris-annex", Class: classCarried, Wording: "assert",
				Text: "Zara rehearses at the verdigris-annex hall.", Probe: "Where does Zara rehearse?",
				Parsed: true, Ops: 1,
				GotSubject: "verdigris-annex", GotQuestion: "where does Zara rehearse", GotValue: "verdigris-annex",
			},
			{
				Subject: "Nadia", Value: "ember-foundry", Class: classCarried, Wording: "assert",
				Text: "Nadia sits with the ember-foundry team.", Probe: "Which team does Nadia sit with?",
				Parsed: true, Ops: 1,
				GotSubject: "ember-foundry", GotQuestion: "which team is ember-foundry", GotValue: "ember-foundry",
			},
		},
		Reads: []ProseRead{
			{Probe: "Where does Zara rehearse?", Subject: "Zara", GotSubject: "Zara", GotQuestion: "where does Zara rehearse"},
			{Probe: "Which team does Nadia sit with?", Subject: "Nadia", GotSubject: "Nadia", GotQuestion: "which team does Nadia sit with"},
		},
	}

	score := ScoreProse(run)

	assert.Equal(t, 1.0, score.Parsed.Mean,
		"every response was the shape the schema demands")
	assert.Equal(t, 0.0, score.SubjectRight.Mean,
		"and not one of them put the subject the fact is about in the subject slot")
	assert.Equal(t, 1.0, score.SubjectHoldsValue.Mean,
		"the value is in the subject slot, which is the failure #547 observed")
	assert.Equal(t, 1.0, score.SelfAnswering.Mean)
	assert.Equal(t, 0.0, score.Reachable.Mean,
		"no probe reaches a fact whose address only its answer could form")

	// The decomposition has to say which slot failed, because the two point
	// at different sentences of the contract.
	assert.Equal(t, 0.0, score.SubjectMatch.Mean, "the subject slot is what diverged")
	assert.Equal(t, 0.5, score.QuestionMatch.Mean,
		"the first row's question matches the read and the second's does not, "+
			"so the two slots are scored apart")
}

// TestScoreProse_ReachableWhenTheAddressIsTheQuestion is the other end:
// an address a probe can form is reached, so the metric is not one that
// reports zero whatever happens.
func TestScoreProse_ReachableWhenTheAddressIsTheQuestion(t *testing.T) {
	run := ProseRun{
		Writes: []ProseWrite{{
			Subject: "Zara", Value: "verdigris-annex", Class: classCarried, Wording: "assert",
			Text: "Zara rehearses at the verdigris-annex hall.", Probe: "Where does Zara rehearse?",
			Parsed: true, Ops: 1,
			GotSubject: "Zara", GotQuestion: "where does Zara rehearse", GotValue: "verdigris-annex",
		}},
		Reads: []ProseRead{{
			Probe: "Where does Zara rehearse?", Subject: "Zara",
			GotSubject: "Zara", GotQuestion: "Where does Zara rehearse?",
		}},
	}

	score := ScoreProse(run)

	assert.Equal(t, 1.0, score.Parsed.Mean)
	assert.Equal(t, 1.0, score.SubjectRight.Mean)
	assert.Equal(t, 0.0, score.SelfAnswering.Mean)
	assert.Equal(t, 1.0, score.Reachable.Mean,
		"punctuation and case are not an address difference; wording is")
}

// TestScoreProse_UnparsedIsARowAndNotADrop keeps a response the shipped
// parser refuses in the denominator. A prompt that produces no write is
// the finding, so dropping those rows would report the surviving ones as
// the whole result.
func TestScoreProse_UnparsedIsARowAndNotADrop(t *testing.T) {
	run := ProseRun{
		Writes: []ProseWrite{
			{Subject: "Zara", Value: "verdigris-annex", Class: classCarried, Wording: "assert",
				Text: "Zara rehearses at the verdigris-annex hall.", Probe: "Where does Zara rehearse?",
				Parsed: false, Reply: `{"reply":"Hi.","memory_ops":[]}`},
			{Subject: "Nadia", Value: "ember-foundry", Class: classCarried, Wording: "assert",
				Text: "Nadia sits with the ember-foundry team.", Probe: "Which team does Nadia sit with?",
				Parsed: true, Ops: 1,
				GotSubject: "Nadia", GotQuestion: "which team does Nadia sit with", GotValue: "ember-foundry"},
		},
		Reads: []ProseRead{
			{Probe: "Where does Zara rehearse?", GotSubject: "Zara", GotQuestion: "where does Zara rehearse"},
			{Probe: "Which team does Nadia sit with?", GotSubject: "Nadia", GotQuestion: "which team does Nadia sit with"},
		},
	}

	score := ScoreProse(run)

	assert.Equal(t, 2, score.Asked, "the empty writeback is a row")
	assert.Equal(t, 0.5, score.Parsed.Mean)
	assert.Equal(t, 0.5, score.Reachable.Mean,
		"a turn that stated no fact reaches nothing, and is counted")
}

// TestProseExamples_ShowTheFailuresFirst pins what #547 asked for beyond a
// rate: the record has to show what the failure looks like, and a rate
// cannot.
func TestProseExamples_ShowTheFailuresFirst(t *testing.T) {
	run := ProseRun{
		Writes: []ProseWrite{{
			Subject: "Zara", Value: "verdigris-annex", Class: classCarried, Wording: "assert",
			Text: "Zara rehearses at the verdigris-annex hall.", Probe: "Where does Zara rehearse?",
			Parsed: true, Ops: 1,
			GotSubject: "verdigris-annex", GotQuestion: "where does Zara rehearse", GotValue: "verdigris-annex",
		}},
		Reads: []ProseRead{{
			Probe: "Where does Zara rehearse?", GotSubject: "Zara", GotQuestion: "where does Zara rehearse",
		}},
	}

	examples := ScoreProse(run).Examples
	require.NotEmpty(t, examples)
	assert.Equal(t, "subject holds the value", examples[0].Kind)
	assert.Equal(t, "Zara rehearses at the verdigris-annex hall.", examples[0].Text,
		"the sentence is shown verbatim, so a reader judges the failure rather than the label")
	assert.Contains(t, examples[0].ReadAs, "Zara",
		"and beside it, the address the probe actually landed on")
}

// TestProseNormalize_IsCaseAndPunctuationOnly states the comparison this
// round makes. It is deliberately strict: two questions that differ in
// wording are different addresses, because a store keyed by an address
// does exactly that, and softening it would introduce a constant nothing
// measured.
func TestProseNormalize_IsCaseAndPunctuationOnly(t *testing.T) {
	assert.Equal(t, "where does zara rehearse", proseNormalize("Where does Zara rehearse?"))
	assert.Equal(t, "where does zara rehearse", proseNormalize("  where  does Zara REHEARSE  "))
	assert.NotEqual(t, proseNormalize("where does Zara rehearse"),
		proseNormalize("where does Zara practise"),
		"a different wording is a different address, and this round does not hide that")
}

// TestProseRound_MeasuredAPromptThatHasSinceChanged is what this
// round's link in the chain #546 built became. That commit held the
// decade run against the internal/memory constants it measured under;
// the constant this round measured under is the shipped prompt, and the
// shipped prompt moved when the writeback became a tool call (#615):
// under a tool transport the sentence "Your response is one JSON object
// with two fields" describes a mechanism the agent no longer has.
//
// The register is not softened and the digest is not edited to match —
// that would make the record say a prompt was measured that never was.
// It is inverted, because the true claim has inverted: proseMeasuredPrompt
// is now the record of a past prompt rather than an assertion about the
// present one, and §Limits says the figures are historical. What this
// still catches is the transition back: restore that prose and the
// figures describe the shipped prompt again, which makes the §Limits
// note wrong and must fail rather than pass quietly.
//
// The schema half is untouched and stays a live assertion: #615 moved
// where ResponseSchema travels, not what it says.
func TestProseRound_MeasuredAPromptThatHasSinceChanged(t *testing.T) {
	prompt := sha256.Sum256([]byte(agent.ComposeInstructions("")))
	assert.NotEqual(t, proseMeasuredPrompt, hex.EncodeToString(prompt[:]),
		"internal/agent/instructions.go holds the prose this round measured again, "+
			"so §What the shipped prose produces is no longer historical: "+
			"remove that note from §Limits, or re-run `-prose` and commit the result")

	schema := sha256.Sum256([]byte(agent.ResponseSchema))
	assert.Equal(t, proseMeasuredSchema, hex.EncodeToString(schema[:]),
		"agent.ResponseSchema no longer holds the schema this round measured")
}
