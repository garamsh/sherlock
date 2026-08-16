package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The record's tables are generated from the committed results, and the
// results state the configuration the decade run measured under —
// §What the shipped design costs renders it as a table. Nothing checked
// that configuration against the code, so the chain ran
// README ← results ← nothing, and a tuned constant in internal/memory
// moved what a re-run would measure while every committed figure and
// every rendered table went on passing.
//
// This is the missing link and not a re-measurement. A committed figure
// stays what it was measured at; what fails here is the claim that the
// figure describes the shipped design, which is the claim that stops
// being true the moment a constant moves.
//
// The remedy when it fails is a decision, not an edit: re-run the
// harness and commit the new figures, or record in §Limits that the
// figure is historical and name the value it was measured under.
func TestCommittedConfiguration_IsStillTheShippedOne(t *testing.T) {
	assert.Equal(t, shippedConfig(), committedRun(t).Config,
		"internal/memory no longer holds the configuration this run measured under, "+
			"so these figures describe a design that has changed: re-run the harness and "+
			"commit the result, or record the figures as historical in §Limits")
}
