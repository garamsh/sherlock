package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/research/chained-recall/study"
)

func TestWindowFor_IsTheBudgetWindowTokensJSONRecordsForThisCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tokens.json"))
	require.NoError(t, err)
	var tokens struct {
		Corpora map[string]struct {
			Window struct {
				Messages int   `json:"messages"`
				Oldest   int64 `json:"oldest_id"`
			} `json:"window"`
		} `json:"corpora"`
	}
	require.NoError(t, json.Unmarshal(data, &tokens))
	w := tokens.Corpora["#890 encoding validation (46ca289)"].Window

	windower, ok := New().(study.Windower)
	require.True(t, ok, "W sets its own window")
	assert.Equal(t, w.Messages, windower.WindowFor(Setting))
	assert.Equal(t, int64(2039), w.Oldest)
	corpus, err := study.LoadCorpus(filepath.Join("..", "..", "..", "..", "memory-encoding", "validation"))
	require.NoError(t, err)
	messages := corpus.Messages()
	assert.Equal(t, w.Oldest, messages[len(messages)-windower.WindowFor(Setting)].ID, "the window's oldest message is the one tokens.json names")
	assert.NotEqual(t, corpus.Window, windower.WindowFor(Setting), "the control: the corpus's own window is not W's")
}
