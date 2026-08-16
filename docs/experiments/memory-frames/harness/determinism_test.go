package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A run must depend on its seed and on nothing else.
//
// The condition PYTHONHASHSEED controls in a Python harness is the order a
// hash container is iterated in. Go randomizes that order on every range of
// every map, independently, so two runs in one process are already two
// conditions in that sense — a run that read an order would disagree with
// itself here. The scheduler is varied on top of it, because the second
// thing that can leak into a result is the order concurrent work finishes
// in.
func TestMeasureRun_IsDeterministicUnderConditionsThatReorderIteration(t *testing.T) {
	p := Params{Years: 10, SessionsPerYear: 2, TurnsPerSession: 4, ThreadsPerClass: 1}

	digests := map[int]string{}
	for _, procs := range []int{1, 4} {
		previous := runtime.GOMAXPROCS(procs)
		w, err := GenerateWorkload(7, p)
		require.NoError(t, err)
		result, err := MeasureRun(context.Background(), w)
		require.NoError(t, err)
		runtime.GOMAXPROCS(previous)

		body, err := json.Marshal(result)
		require.NoError(t, err)
		sum := sha256.Sum256(body)
		digests[procs] = hex.EncodeToString(sum[:])
	}
	assert.Equal(t, digests[1], digests[4],
		"the same seed produced two different runs under two scheduling conditions")
}

func TestGenerateWorkload_ProducesOneTurnStreamUnderBothConditions(t *testing.T) {
	p := Params{Years: 10, SessionsPerYear: 4, TurnsPerSession: 8, ThreadsPerClass: 2}

	digests := map[int]string{}
	for _, procs := range []int{1, 4} {
		previous := runtime.GOMAXPROCS(procs)
		w, err := GenerateWorkload(7, p)
		require.NoError(t, err)
		runtime.GOMAXPROCS(previous)
		digests[procs] = w.TurnDigest
	}
	assert.Equal(t, digests[1], digests[4])
}
