package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The record's figures are generated rather than restated, so the check
// that they are current is that rendering the committed results into the
// committed record changes nothing. This is what stands in for a register
// of claims: there is no prose figure to trace back, because there is no
// prose figure.
func TestRenderTables_LeavesTheCommittedRecordUnchanged(t *testing.T) {
	result := committedRun(t)

	record, err := os.ReadFile(filepath.Join("..", "README.md"))
	require.NoError(t, err)

	rendered, err := Replace(string(record), RenderTables(result))
	require.NoError(t, err)
	assert.Equal(t, string(record), rendered,
		"the record and the results disagree; run `go run ./docs/experiments/memory-frames/harness -render`")
}

// committedRun is every committed result the record renders from. The
// constrained runs, the conversation shapes and the finding study each ask
// a different question and are their own files, so reading one of them is
// reading a part of the record.
func committedRun(t *testing.T) Run {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "results", "decade_seed7.json"))
	require.NoError(t, err)
	var result Run
	require.NoError(t, json.Unmarshal(body, &result))

	constrained, err := os.ReadFile(filepath.Join("..", "results", "constrained_seed7.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(constrained, &result.Constrained))

	shapes, err := os.ReadFile(filepath.Join("..", "results", "harder_seed7.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(shapes, &result.Shapes))

	finding := committedFinding(t)
	result.Finding = &finding
	return result
}

// A figure without an interval cannot be told from a figure the workload
// only appeared to produce, so every one of them carries one or says it has
// no sample.
func TestRenderTables_GivesEveryFigureAnIntervalOrSaysItHasNone(t *testing.T) {
	result := committedRun(t)

	for _, s := range RenderTables(result) {
		for _, line := range strings.Split(s.Tables, "\n") {
			if !strings.HasPrefix(line, "| `") {
				continue
			}
			for _, cell := range strings.Split(strings.Trim(line, "| "), " | ") {
				if strings.ContainsAny(cell, "0123456789") && strings.Contains(cell, ".") {
					assert.True(t, strings.Contains(cell, "±") || isSizeCell(cell),
						"the cell %q in %q carries a figure without an interval", cell, line)
				}
			}
		}
	}
}

// isSizeCell reports whether a cell is one of the size table's, which is a
// store read at one moment rather than a sample and carries no interval.
func isSizeCell(cell string) bool {
	return !strings.Contains(cell, "±") && strings.Count(cell, ".") == 1 &&
		!strings.Contains(cell, " ")
}

// A region on one side and not the other is a table silently dropped or a
// section of the record silently left stale, so every mismatch is an error
// rather than a partial rewrite.
func TestReplace_RefusesARecordThatDoesNotMatchWhatIsRendered(t *testing.T) {
	rendered := []Section{{Name: "one", Tables: "tables"}}
	tests := []struct {
		name   string
		record string
	}{
		{"no region at all", "# A record\n"},
		{"a region that does not close", "# A record\n" + begin("one") + "\n"},
		{"a closing marker with nothing opened", "# A record\n" + endMarker + "\n"},
		{"the markers the wrong way round", endMarker + "\n" + begin("one") + "\n"},
		{"a region the harness does not render", begin("two") + "\n" + endMarker + "\n"},
		{"a region that opens without a name", beginPrefix + "\n" + endMarker + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Replace(tc.record, rendered)
			assert.Error(t, err)
		})
	}
}

func TestReplace_FillsEachRegionAndLeavesEverythingElseAlone(t *testing.T) {
	record := "# A record\n\nbefore.\n\n" + begin("one") + "\n\nold one\n" + endMarker +
		"\n\nbetween.\n\n" + begin("two") + "\n\nold two\n" + endMarker + "\n\nafter.\n"

	out, err := Replace(record, []Section{{Name: "one", Tables: "new one"}, {Name: "two", Tables: "new two"}})
	require.NoError(t, err)

	assert.Contains(t, out, "before.")
	assert.Contains(t, out, "between.")
	assert.Contains(t, out, "after.")
	assert.Contains(t, out, "new one")
	assert.Contains(t, out, "new two")
	assert.NotContains(t, out, "old one")
	assert.NotContains(t, out, "old two")
}

// begin is the record's opening marker for one named region.
func begin(name string) string { return beginPrefix + name + beginSuffix }
