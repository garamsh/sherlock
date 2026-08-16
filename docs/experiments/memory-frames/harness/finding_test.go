package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The subject relation comes from the workload and counts every value live
// anywhere on that subject, which is what a turn could have corrected. What
// it reports stale is what the subject has stopped holding and nothing that
// is still true somewhere on it.
func TestSubjectTruth_SeparatesWhatIsLiveOnASubjectFromWhatIsNot(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	live, stale := subjectTruth(w, w.Params.Years)

	var total, expected int
	for _, values := range live {
		total += len(values)
	}
	for _, thread := range w.Threads {
		values, _ := thread.TruthAt(w.Params.Years)
		expected += len(values)
		for _, value := range values {
			assert.Contains(t, live[thread.Subject], value)
		}
	}
	assert.Equal(t, expected, total)

	for subject, values := range stale {
		for _, value := range values {
			assert.NotContains(t, live[subject], value,
				"%q is reported stale on %s while it is still live there", value, subject)
		}
	}
}

// A question naming a subject and no attribute is a partial address, and an
// addressed structure answers it from every address under that subject and
// from nothing else. It is the only thing such a structure can do with one:
// the address is the whole of its read path.
func TestAddressedFrames_AnswerAPartialAddressFromEveryAddressUnderIt(t *testing.T) {
	ctx := context.Background()
	otherSubject := Address{Subject: "Otto", Attribute: "project"}
	almaClinic := Address{Subject: "Alma", Attribute: "clinic"}

	for _, name := range []string{frameAttributeSingle, frameInterval, frameLog} {
		t.Run(name, func(t *testing.T) {
			frame, err := NewFrame(name, t.TempDir(), NewEmbedCounter(), 0)
			require.NoError(t, err)
			defer frame.Close()

			require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry",
				"Alma leads the amber-quarry project.")))
			require.NoError(t, frame.Observe(ctx, wrote(10, almaClinic, "copper-lantern",
				"Alma goes to the copper-lantern clinic.")))
			require.NoError(t, frame.Observe(ctx, wrote(20, otherSubject, "cobalt-ridge",
				"Otto leads the cobalt-ridge project.")))

			injected, err := frame.Context(ctx, asked(30, Address{Subject: "Alma"}))
			require.NoError(t, err)

			assert.True(t, Mentions(injected.Text, "amber-quarry"))
			assert.True(t, Mentions(injected.Text, "copper-lantern"))
			assert.False(t, Mentions(injected.Text, "cobalt-ridge"),
				"a question about one subject reached another subject's address")
			assert.Less(t, strings.Index(injected.Text, "copper-lantern"),
				strings.Index(injected.Text, "amber-quarry"),
				"the addresses are emitted in attribute order, so a run repeats")
		})
	}
}

// A frame that fills a budget answers a multi-address question differently
// as the budget moves; a structure holding an address and a value has no
// budget to answer inside and returns the same thing whatever it is given.
// That is the property the overflow table reports, and it is a property of
// the structure rather than of the run.
func TestFindSubjects_MovesOnlyTheFramesThatFillABudget(t *testing.T) {
	ctx := context.Background()
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	chars := func(name string, budget int) float64 {
		years, err := findSubjects(ctx, w, name, armOracle, questionAddressed, budget, nil)
		require.NoError(t, err)
		return years[len(years)-1].ContextChars.Mean
	}
	assert.Less(t, chars(frameDocument, 64), chars(frameDocument, 1024),
		"a bounded document is what the budget bounds")
	assert.Less(t, chars(frameShipped, 64), chars(frameShipped, 1024),
		"a ranked store fills whatever budget it is given")
	assert.Equal(t, chars(frameAttributeSingle, 64), chars(frameAttributeSingle, 1024),
		"an addressed store has no budget to answer inside and returns every address under the subject")
}

// The form is the one thing that decides what a question carries. A worded
// question carries no address whatever the arm, which is what makes the
// finding half measurable at all.
func TestAddressFor_CarriesAnAddressOnlyWhereTheFormSuppliesOne(t *testing.T) {
	address := &Address{Subject: "Alma", Attribute: "project"}
	resolving := Resolving(func(string) *Address { return &Address{Subject: "Otto", Attribute: "clinic"} })

	assert.Equal(t, address, addressFor("Which project does Alma lead?", address, questionAddressed, resolving))
	assert.Nil(t, addressFor("Which project does Alma lead?", address, questionWorded, resolving))
	assert.Equal(t, &Address{Subject: "Otto", Attribute: "clinic"},
		addressFor("Which project does Alma lead?", address, questionResolved, resolving))
}

// The finding run asks the same questions of the same workload as the
// decade run, so where it hands over the address it must answer what that
// run answered. This compares the two committed files value by value rather
// than by hashing them: the check is that the measurements agree, not that
// the files are identical.
func TestFindingStudy_AnswersTheCommittedDecadeRowsWhereTheQuestionCarriesItsAddress(t *testing.T) {
	study := committedFinding(t)
	decade := committedRun(t)

	for _, arm := range armNames {
		form := questionAddressed
		if arm == armText {
			// The text arm strips the address from everything a frame
			// receives, the question included, so the row it committed is the
			// worded one.
			form = questionWorded
		}
		for _, frame := range frameNames {
			t.Run(frame+" "+arm, func(t *testing.T) {
				want := decadeFrame(t, decade, frame, arm)
				got := findingFrame(t, study, frame, arm, form)
				require.Equal(t, len(want.Years), len(got.Years))
				for i, year := range want.Years {
					assert.Equal(t, year.Recall, got.Years[i].Recall, "recall at year %d", year.Year)
					assert.Equal(t, year.Stale, got.Years[i].Stale, "stale at year %d", year.Year)
					assert.Equal(t, year.Classes, got.Years[i].Classes, "classes at year %d", year.Year)
				}
			})
		}
	}
}

// A question the words alone cannot address reaches an addressed store as
// nothing, so what it answers is nothing. It is the finding this study is
// for, and it is asserted here so a later change that quietly gives those
// frames a way in fails.
func TestFindingStudy_LeavesAnAddressedStoreWithNothingToAnswerFromWords(t *testing.T) {
	study := committedFinding(t)

	for _, frame := range []string{frameAttributeSingle, frameAttributeMulti, frameAttributeDeclared,
		frameInterval, frameIntervalDeclared, frameLog} {
		t.Run(frame, func(t *testing.T) {
			worded := findingFrame(t, study, frame, armOracle, questionWorded)
			for _, year := range worded.Years {
				assert.Zero(t, year.Recall.Mean, "the worded question answered something at year %d", year.Year)
			}
			for _, year := range worded.Subjects {
				assert.Zero(t, year.Recall.Mean, "the worded question answered something at year %d", year.Year)
			}
		})
	}
}

// committedFinding is the finding study as it is committed.
func committedFinding(t *testing.T) FindingStudy {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "results", "finding_seed7.json"))
	require.NoError(t, err)
	var study FindingStudy
	require.NoError(t, json.Unmarshal(body, &study))
	return study
}

func decadeFrame(t *testing.T, r Run, frame, arm string) FrameResult {
	t.Helper()
	for _, f := range r.Frames {
		if f.Frame == frame && f.Arm == arm {
			return f
		}
	}
	t.Fatalf("the decade run has no %s in the %s arm", frame, arm)
	return FrameResult{}
}

func findingFrame(t *testing.T, s FindingStudy, frame, arm, form string) FindingFrame {
	t.Helper()
	for _, f := range s.Frames {
		if f.Frame == frame && f.Arm == arm && f.Question == form {
			return f
		}
	}
	t.Fatalf("the finding study has no %s in the %s arm under a %s question", frame, arm, form)
	return FindingFrame{}
}
