package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transcriptFrame keeps every turn it observed and returns all of it. It is
// what the scoring rules are checked against: a frame that forgets nothing
// must be credited with every value that is true, and must equally be
// caught returning every value that has stopped being true.
type transcriptFrame struct{ lines []string }

func (f *transcriptFrame) Observe(_ context.Context, t Turn) error {
	f.lines = append(f.lines, t.Text)
	return nil
}

func (f *transcriptFrame) Context(context.Context, Turn) (string, error) {
	return strings.Join(f.lines, "\n"), nil
}

func (f *transcriptFrame) Size(context.Context) (Size, error) {
	return Size{Units: len(f.lines)}, nil
}

func (f *transcriptFrame) Close() error { return nil }

// silentFrame answers every turn with nothing.
type silentFrame struct{}

func (silentFrame) Observe(context.Context, Turn) error           { return nil }
func (silentFrame) Context(context.Context, Turn) (string, error) { return "", nil }
func (silentFrame) Size(context.Context) (Size, error)            { return Size{}, nil }
func (silentFrame) Close() error                                  { return nil }

// measureWith runs a frame the registry does not build, which is also the
// check that the seam admits a structure neither shipped nor document.
func measureWith(t *testing.T, frame Frame, w Workload, arm string) FrameResult {
	t.Helper()
	out := FrameResult{}
	year := 1
	ctx := context.Background()
	checkpoint := func() {
		probes, size, err := ask(ctx, frame, w, year, arm)
		require.NoError(t, err)
		out.Probes = append(out.Probes, probes...)
		out.Years = append(out.Years, summarizeYear(year, probes))
		out.Sizes = append(out.Sizes, SizeAt{Year: year, Size: size})
		year++
	}
	for _, turn := range w.Turns {
		for year <= w.Params.Years && turn.Day >= year*daysPerYear {
			checkpoint()
		}
		require.NoError(t, frame.Observe(ctx, observed(turn, arm)))
	}
	for year <= w.Params.Years {
		checkpoint()
	}
	return out
}

func TestMeasure_CreditsEveryLiveValueToAFrameThatForgetsNothing(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	result := measureWith(t, &transcriptFrame{}, w, armOracle)

	require.Len(t, result.Years, w.Params.Years)
	for _, y := range result.Years {
		assert.Equal(t, 1.0, y.Recall.Mean, "year %d: a frame holding every turn answers every question", y.Year)
		for _, c := range y.Classes {
			assert.Equal(t, 1.0, c.Recall.Mean, "year %d, %s", y.Year, c.Class)
		}
	}
	// The same frame returns the values that stopped being true, which is
	// what the stale figure is for. Nothing is superseded until the first
	// replacement year, and the figure has no sample before then.
	assert.Equal(t, 0, result.Years[0].Stale.N)
	for _, y := range result.Years[2:] {
		assert.Equal(t, 1.0, y.Stale.Mean, "year %d", y.Year)
		assert.Positive(t, y.Stale.N)
	}
}

func TestMeasure_CreditsNothingToAFrameThatReturnsNothing(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	result := measureWith(t, silentFrame{}, w, armOracle)

	for _, y := range result.Years {
		assert.Equal(t, 0.0, y.Recall.Mean, "year %d", y.Year)
		assert.Equal(t, 0.0, y.ContextChars.Mean, "year %d", y.Year)
	}
	for _, y := range result.Years[2:] {
		assert.Equal(t, 0.0, y.Stale.Mean, "year %d", y.Year)
	}
}

func TestMeasure_AsksEveryAnswerableThreadAtEveryYear(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	result := measureWith(t, silentFrame{}, w, armOracle)

	var total int
	for i, y := range result.Years {
		assert.Equal(t, i+1, y.Year)
		var expected int
		for _, thread := range w.Threads {
			if thread.ProbedAt(y.Year) {
				expected++
			}
		}
		assert.Equal(t, expected, y.Probes, "year %d", y.Year)
		total += y.Probes
	}
	assert.Equal(t, w.Params.Probes, total)
	assert.Len(t, result.Probes, total)
}

func TestMeasure_RunsTheRegisteredFramesOverTheWholeDecade(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 2, TurnsPerSession: 4, ThreadsPerClass: 1})
	require.NoError(t, err)

	for _, arm := range armNames {
		for _, name := range frameNames {
			result, err := Measure(context.Background(), w, name, arm, NewEmbedCounter())
			require.NoError(t, err, "%s in the %s arm", name, arm)
			assert.Equal(t, name, result.Frame)
			assert.Equal(t, arm, result.Arm)
			assert.Len(t, result.Years, w.Params.Years)
			assert.Len(t, result.Sizes, len(SizeYears(w.Params.Years)))
			assert.Equal(t, SizeYears(w.Params.Years), []int{result.Sizes[0].Year, result.Sizes[1].Year, result.Sizes[2].Year})
		}
	}
}

// The text arm withholds the extracted fact from every turn. A frame that
// reads only the text cannot tell the arms apart, and that its figures are
// identical is the check that an arm difference is a design's own choice
// rather than a difference in the workload.
func TestMeasure_LeavesAFrameThatReadsOnlyTextUnchangedByTheArm(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 2, TurnsPerSession: 4, ThreadsPerClass: 1})
	require.NoError(t, err)

	oracle, err := Measure(context.Background(), w, frameDocument, armOracle, NewEmbedCounter())
	require.NoError(t, err)
	text, err := Measure(context.Background(), w, frameDocument, armText, NewEmbedCounter())
	require.NoError(t, err)

	assert.Equal(t, oracle.Probes, text.Probes)
}

func TestMentions_CreditsAValueOnlyOnAWholeToken(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		value    string
		want     bool
	}{
		{"the value stands alone", "Alma leads the amber-quarry project.", "amber-quarry", true},
		{"the value ends a sentence", "the project is amber-quarry", "amber-quarry", true},
		{"a longer token that starts with it", "Alma leads amber-quarryside.", "amber-quarry", false},
		{"a longer token that ends with it", "Alma leads the deep-amber-quarry.", "amber-quarry", false},
		{"the first half alone", "Alma leads the amber project.", "amber-quarry", false},
		{"the second half alone", "Alma leads the quarry project.", "amber-quarry", false},
		{"the two halves apart", "amber is not quarry", "amber-quarry", false},
		{"a different case", "AMBER-QUARRY", "amber-quarry", true},
		{"another value sharing a half", "Alma leads the amber-lantern project.", "amber-quarry", false},
		{"nothing at all", "", "amber-quarry", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Mentions(tc.injected, tc.value))
		})
	}
}

func TestSummarizeYear_ReportsAClassOnlyWhereItWasAsked(t *testing.T) {
	probes := []ProbeResult{
		{Year: 4, Class: classCarried, Expected: 1, Found: 1},
		{Year: 4, Class: classCarried, Expected: 1, Found: 0},
		{Year: 4, Class: classReplaced, Expected: 1, Found: 1, Superseded: 2, Stale: 1},
	}
	out := summarizeYear(4, probes)

	assert.Equal(t, 3, out.Probes)
	require.Len(t, out.Classes, 2)
	assert.Equal(t, classCarried, out.Classes[0].Class)
	assert.Equal(t, 0.5, out.Classes[0].Recall.Mean)
	assert.Equal(t, 0, out.Classes[0].Stale.N, "a class with nothing superseded reports no stale figure")
	assert.Equal(t, classReplaced, out.Classes[1].Class)
	assert.Equal(t, 1.0, out.Classes[1].Stale.Mean)
	assert.InDelta(t, 2.0/3.0, out.Recall.Mean, 1e-9)
}
