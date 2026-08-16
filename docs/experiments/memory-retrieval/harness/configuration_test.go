package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The record's figures are checked against the run that produced them,
// and the run states the configuration it measured under. Nothing
// checked that configuration against the code, so the chain ran
// README ← results ← nothing, and a tuned constant in internal/memory
// moved what a re-run would measure while every committed figure and
// every claim check went on passing.
//
// This is the missing link and not a re-measurement. A committed figure
// stays what it was measured at; what fails here is the claim that the
// figure describes the shipped system, which is the claim that stops
// being true the moment a constant moves.
//
// The remedy when it fails is a decision, not an edit: re-run the
// harness and commit the new figures, or record in §Limits that the
// figure is historical and name the value it was measured under.
func TestCommittedConfiguration_IsStillTheShippedOne(t *testing.T) {
	for _, embedder := range measuredEmbedders {
		t.Run(embedder, func(t *testing.T) {
			assert.Equal(t, shippedConfig(), loadRunFor(t, embedder).Config,
				"internal/memory no longer holds the configuration this run measured under, "+
					"so these figures describe a system that has changed: re-run the harness and "+
					"commit the result, or record the figures as historical in §Limits")
		})
	}
}
