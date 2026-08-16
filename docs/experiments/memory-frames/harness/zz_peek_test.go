package main

import (
	"context"
	"fmt"
	"testing"
)

func TestPeekQuestionFrames(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{frameAttributeSingle, frameAttributeCued, frameAttributeDeclared, frameQuestion, frameQuestionDeclared} {
		m, err := Measure(context.Background(), w, name, armOracle, NewEmbedCounter(), nil)
		if err != nil {
			t.Fatal(err)
		}
		y := m.Years[len(m.Years)-1]
		fmt.Printf("%-20s recall %.3f stale %.3f chars %.0f scanned %.2f | ", name, y.Recall.Mean, y.Stale.Mean, y.ContextChars.Mean, y.Scanned.Mean)
		for _, c := range y.Classes {
			fmt.Printf("%s %.3f ", c.Class, c.Recall.Mean)
		}
		fmt.Printf("| units %d\n", m.Sizes[len(m.Sizes)-1].Size.Units)
	}
}
