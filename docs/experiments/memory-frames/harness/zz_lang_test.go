package main

import (
	"fmt"
	"testing"
)

func TestLanguageShapes(t *testing.T) {
	p := Params{Years: 10, SessionsPerYear: 8, TurnsPerSession: 10, ThreadsPerClass: 12}
	for _, shape := range shapeNames {
		q := p
		q.Shape = shape
		w, err := GenerateWorkload(7, q)
		if err != nil {
			t.Fatal(shape, err)
		}
		l := MeasureLanguage(w)
		fmt.Printf("%-8s digest=%s turns=%d factMsgs=%d assertions=%d perMsg=%.2f chars=%.0f clauses=%.2f\n",
			shape, w.TurnDigest[:8], w.Params.Turns, l.FactMessages, l.Assertions,
			l.PerMessage.Mean, l.Chars.Mean, l.Clauses.Mean)
		for _, x := range w.Turns {
			if len(x.Written) > 1 {
				fmt.Printf("   packed example: %q\n", x.Text)
				break
			}
		}
	}
}
