package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTop_ReadsNothingAtOrAfterTheBatch(t *testing.T) {
	units := []vec{
		{ID: 1, First: 1, Last: 1, Vector: []float32{1, 0}},
		{ID: 2, First: 2, Last: 2, Vector: []float32{0, 1}},
		{ID: 3, First: 3, Last: 3, Vector: []float32{1, 0}},
		{ID: 4, First: 4, Last: 4, Vector: []float32{1, 0}},
		{ID: 9, First: 5, Last: 8, Vector: []float32{1, 0}},
		{ID: 10, First: 9, Last: 12, Vector: []float32{1, 0}},
	}
	got := top([]float32{1, 0}, units, 9, 3)
	var ids []int64
	for _, u := range got {
		ids = append(ids, u.ID)
		assert.Less(t, u.Last, int64(9), "a unit reaching the batch is never a candidate")
	}
	assert.Equal(t, []int64{3, 4, 9}, ids, "the k best, newest first on a tie, returned in source order")
	assert.Len(t, top([]float32{1, 0}, units, 3, 3), 2, "only what ends before the batch: sources 1 and 2")
	assert.Empty(t, top([]float32{1, 0}, units, 1, 3))
}
