package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuleEntities_TakesCapitalizedRunsWithoutLeadingStopwords(t *testing.T) {
	got := ruleEntities("Every part for the Rodend build is ordered from Gebind Components. The Suvun Hotel's coach is Keahaill Coaches, reference REF-FEPE732. User thanked the assistant.")
	assert.Equal(t, []string{"rodend", "gebind components", "suvun hotel", "keahaill coaches", "ref-fepe732"}, got)
	assert.Empty(t, ruleEntities("the user asked about a grinding noise and got an answer."), "the control: lowercase text names no entity")
}

func TestRuleEntities_SplitsRunsAtPunctuation(t *testing.T) {
	assert.Equal(t, []string{"gebind components", "mateo deatreal"}, ruleEntities("Gebind Components, Mateo Deatreal"))
	assert.Equal(t, []string{"gebind components mateo deatreal"}, ruleEntities("Gebind Components Mateo Deatreal"), "the control: without the comma they are one run")
}

func TestNames_NormalizesAndDropsEmptyAndRepeated(t *testing.T) {
	assert.Equal(t, []string{"suvun hotel", "mateo deatreal"}, names([]string{"the Suvun Hotel", "Suvun Hotel", "The", "Mateo Deatreal's", ""}))
}
