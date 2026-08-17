package main

import (
	"context"
	"fmt"
)

// What a design that finds the memory it updates by similarity has to work
// with, measured on the workload rather than read off one design's score.
//
// A structure that computes an address knows which memory a write lands on.
// A structure that searches for it has to separate the memory the write
// updates from every other memory it holds, and this is the evidence for
// whether that separation exists at all — the same treatment §The arity
// question gave the timing rule, and for the same reason: a design whose
// mechanism cannot work on this workload should be shown not to work before
// its row is read as a score.

// Identity is the separation a similarity-based write has to find.
//
// Target is the similarity between a new assertion and the value already at
// its address, which is what a correct update must match. Distractor is the
// highest similarity between the same assertion and any value held at a
// different address, which is what it must not match. Inverted counts the
// assertions where the distractor is at least the target — where the nearest
// memory is the wrong one — and BelowGate the assertions whose own target
// falls under the gate a write would accept a candidate at.
type Identity struct {
	Assertions int     `json:"assertions_landing_on_a_held_address"`
	Target     Figure  `json:"similarity_to_the_value_at_the_address"`
	Distractor Figure  `json:"similarity_to_the_nearest_value_elsewhere"`
	Inverted   int     `json:"assertions_whose_nearest_value_is_elsewhere"`
	BelowGate  int     `json:"assertions_whose_target_falls_below_the_gate"`
	Gate       float64 `json:"gate"`
}

// MeasureIdentity replays the workload's assertions in order and reads both
// similarities off the store as it stands when each one arrives.
//
// The candidate set is every value written so far, which is the widest a
// design could be holding and therefore the hardest separation to make. A
// design that prunes holds a subset of it, and a separation that fails here
// does not become available by holding less.
func MeasureIdentity(ctx context.Context, w Workload, counter *EmbedCounter) (Identity, error) {
	embedder := counter.Wrap(NewHashingEmbedder())
	out := Identity{Gate: mem0Gate}
	// held is the vector of the value most recently written at each address,
	// and written every vector put in so far with the address it went to.
	held := map[Address][]float32{}
	type placed struct {
		address Address
		vector  []float32
	}
	var written []placed
	var targets, distractors []float64
	for _, turn := range w.Turns {
		for _, one := range turn.Written {
			vector, err := embedder.Embed(ctx, line(one.Address, one.Value))
			if err != nil {
				return Identity{}, fmt.Errorf("embed at turn %d: %w", turn.Index, err)
			}
			if at, ok := held[one.Address]; ok {
				target := cosine(vector, at)
				distractor := 0.0
				for _, other := range written {
					if other.address == one.Address {
						continue
					}
					distractor = max(distractor, cosine(vector, other.vector))
				}
				out.Assertions++
				targets = append(targets, target)
				distractors = append(distractors, distractor)
				if distractor >= target {
					out.Inverted++
				}
				if target < out.Gate {
					out.BelowGate++
				}
			}
			held[one.Address] = vector
			written = append(written, placed{address: one.Address, vector: vector})
		}
	}
	out.Target = NewFigure(targets)
	out.Distractor = NewFigure(distractors)
	return out, nil
}
