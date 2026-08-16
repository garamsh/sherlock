package main

// What the workload's addresses look like, which is where the answer to
// "can a frame decide arity from what it observes" has to come from. Every
// candidate rule reads one of these two shapes — the interval between
// writes to one address, or how the attribute's other addresses have
// behaved — so measuring them says whether a rule of that shape could work
// before one is written.

// ClassAddresses is one fact class's addresses. Values counts the distinct
// values an address is written; Gap is the interval between the last two
// writes to it, in years, over the addresses written more than once.
type ClassAddresses struct {
	Class     string `json:"class"`
	Addresses int    `json:"addresses"`
	Values    Figure `json:"values_per_address"`
	Gap       Figure `json:"years_between_the_last_two_writes"`
}

// AttributeAddresses is one attribute's addresses and how many of them were
// ever written more than one value. A rule that decides arity per attribute
// reads this column, so a column with the same value on every row is a rule
// that cannot separate anything.
type AttributeAddresses struct {
	Attribute   string  `json:"attribute"`
	Addresses   int     `json:"addresses"`
	MultiValued int     `json:"addresses_written_more_than_one_value"`
	Share       float64 `json:"share"`
}

// Arity is the evidence about deciding arity from what a frame observes.
type Arity struct {
	Classes    []ClassAddresses     `json:"classes"`
	Attributes []AttributeAddresses `json:"attributes"`
}

// MeasureArity reads the workload's threads. One thread is one address, so
// this describes exactly what an attribute-addressed frame sees.
func MeasureArity(w Workload) Arity {
	out := Arity{}
	for _, class := range classNames {
		row := ClassAddresses{Class: class}
		var values, gaps []float64
		for _, thread := range w.Threads {
			if thread.Class != class {
				continue
			}
			row.Addresses++
			values = append(values, float64(distinctValues(thread)))
			if len(thread.Asserts) > 1 {
				last := thread.Asserts[len(thread.Asserts)-1]
				previous := thread.Asserts[len(thread.Asserts)-2]
				gaps = append(gaps, float64(last.Day-previous.Day)/daysPerYear)
			}
		}
		row.Values = NewFigure(values)
		row.Gap = NewFigure(gaps)
		out.Classes = append(out.Classes, row)
	}
	for _, attribute := range attributes {
		row := AttributeAddresses{Attribute: attribute.name}
		for _, thread := range w.Threads {
			if thread.Attribute != attribute.name {
				continue
			}
			row.Addresses++
			if distinctValues(thread) > 1 {
				row.MultiValued++
			}
		}
		row.Share = ratio(row.MultiValued, row.Addresses)
		out.Attributes = append(out.Attributes, row)
	}
	return out
}

// distinctValues counts the values a frame would end up holding at the
// address if it kept every one written to it. A thread that returns to a
// value it left writes that value twice and holds one.
func distinctValues(thread Thread) int {
	var seen []string
	for _, a := range thread.Asserts {
		if !contains(seen, a.Value) {
			seen = append(seen, a.Value)
		}
	}
	return len(seen)
}
