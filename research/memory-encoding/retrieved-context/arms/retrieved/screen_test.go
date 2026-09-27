package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
)

func TestScreen_IsTheRetrievalRWKeyMakes(t *testing.T) {
	p := &proxy{}
	e := env(t, p)
	require.NoError(t, writeScreen(context.Background(), e))
	assert.Equal(t, 0, p.calls(), "the screen calls no model")
	screened, err := study.ReadJSONL[record](filepath.Join(e.Dir, "arms", "retrieved", "evidence", "screen-b4.jsonl"))
	require.NoError(t, err)
	_, err = arm{}.Build(context.Background(), e, settingKey, filepath.Join(e.Work, "builds", "RW", settingKey))
	require.NoError(t, err)
	byFirst := map[int64]record{}
	for _, r := range recording(t, e, settingKey) {
		byFirst[r.First] = r
	}
	require.Len(t, screened, 4)
	for _, s := range screened {
		assert.Equal(t, s.Messages, byFirst[s.First].Messages)
		assert.Equal(t, s.Mids, byFirst[s.First].Mids)
	}
}
