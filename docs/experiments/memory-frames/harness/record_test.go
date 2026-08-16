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
// constrained runs and the conversation shapes each ask a different
// question of a different workload and are their own files, so reading one
// of them is reading a third of the record.
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
	return result
}

// A figure without an interval cannot be told from a figure the workload
// only appeared to produce, so every one of them carries one or says it has
// no sample.
func TestRenderTables_GivesEveryFigureAnIntervalOrSaysItHasNone(t *testing.T) {
	result := committedRun(t)

	for _, line := range strings.Split(RenderTables(result), "\n") {
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

// isSizeCell reports whether a cell is one of the size table's, which is a
// store read at one moment rather than a sample and carries no interval.
func isSizeCell(cell string) bool {
	return !strings.Contains(cell, "±") && strings.Count(cell, ".") == 1 &&
		!strings.Contains(cell, " ")
}

func TestReplace_RefusesARecordWithoutBothMarkers(t *testing.T) {
	tests := []struct {
		name   string
		record string
	}{
		{"neither marker", "# A record\n"},
		{"only the opening marker", "# A record\n" + beginMarker + "\n"},
		{"only the closing marker", "# A record\n" + endMarker + "\n"},
		{"the markers the wrong way round", endMarker + "\n" + beginMarker + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Replace(tc.record, "tables")
			assert.Error(t, err)
		})
	}
}

func TestReplace_LeavesEverythingOutsideTheMarkersAlone(t *testing.T) {
	record := "# A record\n\nbefore.\n\n" + beginMarker + "\n\nold tables\n" + endMarker + "\n\nafter.\n"

	out, err := Replace(record, "new tables")
	require.NoError(t, err)

	assert.Contains(t, out, "before.")
	assert.Contains(t, out, "after.")
	assert.Contains(t, out, "new tables")
	assert.NotContains(t, out, "old tables")
}
