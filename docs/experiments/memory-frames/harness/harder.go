package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
)

// Every figure before this file was measured on one conversation shape:
// short single-clause sentences, one fact to a message. Real conversation is
// neither, and #463 already showed the shape of a sentence is the dominant
// variable — a model scored 1.000 on a conjunction and 0.425 on one short
// qualifier clause.
//
// Three shapes are generated from the same threads and the same schedule, so
// what differs between two runs is only what the shape changes:
//
//   - plain — the sentences the decade run uses, at this run's size.
//   - clausal — the same assertions, each wrapped in a subordinate frame, so
//     a sentence embeds and qualifies rather than stating one clause.
//   - packed — the plain sentences, several to a message, so one message
//     carries several replace-or-join decisions.
//
// The two harder shapes vary one thing each rather than both, so a
// difference from plain is attributable: clausal is the sentence and packed
// is the number of decisions in it. Holding the threads and the schedule
// fixed is what makes any of it a comparison.
const (
	shapePlain   = "plain"
	shapeClausal = "clausal"
	shapePacked  = "packed"
)

// shapeNames is the order every table reports the shapes in.
var shapeNames = []string{shapePlain, shapeClausal, shapePacked}

// packedAssertions is the most a packed message carries. Three is what the
// issue asks about: enough that a misread clause has neighbours to spoil.
const packedAssertions = 3

// clausalFrames wrap one assertion in subordination. The clause keeps its
// own wording — the conjunction or the qualifier that marks what it does to
// its address — so the wording families still line up with #463's table and
// only the sentence around them changes.
//
// None of them may introduce a word the cue rule reads as marking an
// addition, or wrapping a replacement would turn it into a join for that
// frame. `harder_test.go` checks that rather than trusting it.
var clausalFrames = []string{
	"Now that the handover is finally out of the way, and I should have said this sooner, %s, which is worth writing down somewhere.",
	"There is a thing I keep meaning to mention, which is that %s, in case it matters for the planning.",
	"Before I forget again — and I have forgotten twice already — %s, though nobody seems to have written it anywhere.",
	"For the record, since the last version of this went out of date without anyone noticing, %s.",
	"It came up in passing at the review, where half the room had not heard it either, that %s.",
	"Assuming nothing else has changed since the last time we went through this, %s, which I am told is settled.",
}

// packedFrames join several assertions into one message. They carry no
// marker of their own: what each assertion does to its address stays inside
// its own clause.
var packedFrames = []string{
	"A few things came out of the review, and I am writing them down before they go the way of the last set: %s",
	"Three or four bits of housekeeping, none of them urgent but all of them the sort of thing that gets lost: %s",
	"Catching you up on what changed while you were away, in no particular order: %s",
	"From the notes, tidied up and probably still incomplete: %s",
}

// packedJoins are what separates one assertion from the next inside a
// packed message.
var packedJoins = []string{
	"; and separately, ",
	"; meanwhile, ",
	"; on a different note, ",
	"; also worth saying, ",
}

// clause strips the sentence-ending period so an assertion can be embedded
// in a longer sentence without doubling it.
func clause(sentence string) string { return strings.TrimSuffix(sentence, ".") }

// harden rewrites an assertion's sentence for the shape. The plain shape
// returns it untouched, which is what keeps a plain run of this size
// comparable to the decade run sentence for sentence.
func harden(rng *rand.Rand, shape, sentence string) string {
	if shape != shapeClausal {
		return sentence
	}
	return fmt.Sprintf(clausalFrames[rng.Intn(len(clausalFrames))], clause(sentence))
}

// pack joins several already-hardened sentences into one message.
func pack(rng *rand.Rand, sentences []string) string {
	if len(sentences) == 1 {
		return sentences[0]
	}
	clauses := make([]string, len(sentences))
	for i, sentence := range sentences {
		clauses[i] = clause(sentence)
	}
	joined := clauses[0]
	for _, rest := range clauses[1:] {
		joined += packedJoins[rng.Intn(len(packedJoins))] + rest
	}
	return fmt.Sprintf(packedFrames[rng.Intn(len(packedFrames))], joined) + "."
}

// Language is how hard a workload's sentences actually are. A null result on
// a harder shape is more likely the generator than the model, so what the
// generator produced is measured rather than asserted.
type Language struct {
	Shape string `json:"shape"`
	// Messages carrying at least one assertion, and the assertions they
	// carry between them.
	FactMessages int    `json:"fact_messages"`
	Assertions   int    `json:"assertions"`
	PerMessage   Figure `json:"assertions_per_message"`
	// Characters and clauses in a fact-carrying message. A clause is
	// counted at every comma, semicolon, dash and subordinator, which is a
	// crude count that moves in the right direction for the right reason.
	Chars   Figure `json:"chars_per_message"`
	Clauses Figure `json:"clauses_per_message"`
}

// subordinators are the words a clause count looks for beyond punctuation.
var subordinators = []string{"that", "which", "since", "while", "though", "because", "before", "after", "assuming", "where"}

// MeasureLanguage reads what the generator produced.
func MeasureLanguage(w Workload) Language {
	out := Language{Shape: w.Params.Shape}
	var per, chars, clauses []float64
	for _, t := range w.Turns {
		if len(t.Written) == 0 {
			continue
		}
		out.FactMessages++
		out.Assertions += len(t.Written)
		per = append(per, float64(len(t.Written)))
		chars = append(chars, float64(len(t.Text)))
		clauses = append(clauses, float64(countClauses(t.Text)))
	}
	out.PerMessage = NewFigure(per)
	out.Chars = NewFigure(chars)
	out.Clauses = NewFigure(clauses)
	return out
}

// countClauses is one for the sentence plus one for every boundary marker in
// it. It is not a parser and does not need to be: what it has to do is order
// three shapes the same way a reader would.
func countClauses(text string) int {
	count := 1 + strings.Count(text, ",") + strings.Count(text, ";") + strings.Count(text, "—")
	lower := strings.ToLower(text)
	for _, word := range subordinators {
		count += strings.Count(lower, " "+word+" ")
	}
	return count
}

// harderParams is the shape study's size. It is smaller than the decade run
// on purpose: every message is read by a model once per shape, and the bound
// on that is what sets this. Its figures are therefore comparable across the
// three shapes and not to the decade run, which the record says plainly.
func harderParams(shape string) Params {
	return Params{Years: 10, SessionsPerYear: 8, TurnsPerSession: 10, ThreadsPerClass: 12, Shape: shape}
}

// armExtracted is the middle of the bracket: the same messages, carrying
// what a model read out of them rather than what the workload put in.
const armExtracted = "extracted"

// ShapeRun is one conversation shape measured end to end.
type ShapeRun struct {
	Shape      string           `json:"shape"`
	Digest     string           `json:"turn_digest"`
	Params     Params           `json:"params"`
	Language   Language         `json:"language"`
	Extraction *ExtractionScore `json:"extraction,omitempty"`
	Frames     []FrameResult    `json:"frames"`
}

// MeasureShapes runs every frame over every shape, in the oracle and text
// arms always and in the extracted arm wherever a committed extraction says
// what a model read.
func MeasureShapes(ctx context.Context, seed int64, extractions map[string]Extraction) ([]ShapeRun, error) {
	var out []ShapeRun
	for _, shape := range shapeNames {
		w, err := GenerateWorkload(seed, harderParams(shape))
		if err != nil {
			return nil, fmt.Errorf("generate %s: %w", shape, err)
		}
		run := ShapeRun{Shape: shape, Digest: w.TurnDigest, Params: w.Params, Language: MeasureLanguage(w)}
		arms := []struct {
			name string
			over Workload
		}{{armOracle, w}, {armText, w}}
		if e, ok := extractions[shape]; ok {
			scored := ScoreExtraction(w, e)
			run.Extraction = &scored
			arms = append(arms, struct {
				name string
				over Workload
			}{armExtracted, e.Observed(w)})
		}
		for _, arm := range arms {
			// The extracted arm delivers what a model read, so it reaches a
			// frame through the oracle path: the arm's name says where the
			// assertions came from, not whether there are any.
			deliver := arm.name
			if deliver == armExtracted {
				deliver = armOracle
			}
			for _, frame := range frameNames {
				measured, err := Measure(ctx, arm.over, frame, deliver, NewEmbedCounter(), nil)
				if err != nil {
					return nil, fmt.Errorf("%s %s %s: %w", shape, frame, arm.name, err)
				}
				measured.Arm = arm.name
				run.Frames = append(run.Frames, measured)
			}
		}
		out = append(out, run)
	}
	return out, nil
}
