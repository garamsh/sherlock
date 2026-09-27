package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	// Bound is the most characters the rendered document may hold.
	Bound = 16000
	// lineRunes bounds one line's text.
	lineRunes = 200
	// PassMids is how many new mids one pass reads.
	PassMids = 8
	maxOps   = 16
)

// Line is one line of the document: a fact and the mids it cites, with the
// source interval those mids span.
type Line struct {
	ID    int     `json:"id"`
	Text  string  `json:"text"`
	Mids  []int64 `json:"mids"`
	First int64   `json:"first"`
	Last  int64   `json:"last"`
	Pass  int     `json:"pass"`
}

// Version is one line as an operation left it; the history keeps every
// version, so nothing a pass replaces or drops is lost.
type Version struct {
	Line int     `json:"line"`
	Op   string  `json:"op"`
	Text string  `json:"text"`
	Mids []int64 `json:"mids"`
	Pass int     `json:"pass"`
}

// Document is D's memory document.
type Document struct {
	Lines   []Line    `json:"lines"`
	History []Version `json:"history"`
	NextID  int       `json:"next_id"`
}

// Op is one operation a pass returns. Line numbers the document as the pass
// was shown it, from 1.
type Op struct {
	Op   string  `json:"op"`
	Line int     `json:"line,omitempty"`
	Text string  `json:"text,omitempty"`
	Mids []int64 `json:"mids,omitempty"`
}

// Applied counts what a pass did.
type Applied struct {
	Added      int `json:"added"`
	Superseded int `json:"superseded"`
	Dropped    int `json:"dropped"`
	// Rejected counts operations that broke a rule and were left out.
	Rejected int `json:"rejected"`
	// Skipped counts valid operations a partial pass left out to hold the
	// quarter and the bound.
	Skipped int `json:"skipped"`
}

// ErrRefused is a pass that would drop more than a quarter of the lines or
// exceed the bound; nothing of it is applied.
var ErrRefused = errors.New("pass refused")

// Render is the document as the model reads it: one numbered line each,
// with the mids it cites.
func (d *Document) Render() string {
	var b strings.Builder
	for i, l := range d.Lines {
		fmt.Fprintf(&b, "%d. %s (mids %s)\n", i+1, l.Text, joinIDs(l.Mids))
	}
	return b.String()
}

// Chars is the rendered document's length in characters.
func (d *Document) Chars() int { return len([]rune(d.Render())) }

func joinIDs(ids []int64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(s, ", ")
}

// Apply applies one pass's operations. An operation is rejected when its
// line does not exist, its text is empty or longer than lineRunes, or it
// cites no mid or a mid the pass was not given. Strict, a pass whose drops
// exceed a quarter of the lines or whose result exceeds Bound is refused
// whole; not strict, drops past the quarter and adds past the bound are
// skipped, in order. intervals gives each of the pass's mids its sources.
func (d *Document) Apply(ops []Op, pass int, intervals map[int64][2]int64, strict bool) (Applied, error) {
	var a Applied
	var valid []Op
	for _, op := range ops {
		if d.rule(op, intervals) {
			valid = append(valid, op)
		} else {
			a.Rejected++
		}
	}
	drops := 0
	for _, op := range valid {
		if op.Op == "drop" {
			drops++
		}
	}
	quota := len(d.Lines) / 4
	if strict && drops > quota {
		return a, fmt.Errorf("%w: %d drops of %d lines; at most a quarter, %d", ErrRefused, drops, len(d.Lines), quota)
	}
	next := d.clone()
	byPosition := slices.Clone(d.Lines)
	gone := map[int]bool{}
	for _, op := range valid {
		switch op.Op {
		case "drop":
			id := byPosition[op.Line-1].ID
			if gone[id] || (!strict && a.Dropped >= quota) {
				a.Skipped++
				continue
			}
			gone[id] = true
			next.remove(id)
			next.History = append(next.History, Version{Line: id, Op: "drop", Pass: pass})
			a.Dropped++
		case "supersede":
			id := byPosition[op.Line-1].ID
			if gone[id] {
				a.Skipped++
				continue
			}
			next.set(id, op, pass, intervals)
			next.History = append(next.History, Version{Line: id, Op: "supersede", Text: strings.TrimSpace(op.Text), Mids: op.Mids, Pass: pass})
			a.Superseded++
		case "add":
			line := newLine(next.NextID, op, pass, intervals)
			if !strict && next.Chars()+len([]rune(fmt.Sprintf("%d. %s (mids %s)\n", len(next.Lines)+1, line.Text, joinIDs(line.Mids)))) > Bound {
				a.Skipped++
				continue
			}
			next.NextID++
			next.Lines = append(next.Lines, line)
			next.History = append(next.History, Version{Line: line.ID, Op: "add", Text: line.Text, Mids: line.Mids, Pass: pass})
			a.Added++
		}
	}
	if next.Chars() > Bound {
		if strict {
			return Applied{Rejected: a.Rejected}, fmt.Errorf("%w: the document would hold %d characters; at most %d", ErrRefused, next.Chars(), Bound)
		}
		return Applied{Rejected: a.Rejected}, fmt.Errorf("a partial pass still exceeds the bound: %d characters", next.Chars())
	}
	*d = next
	return a, nil
}

func (d *Document) rule(op Op, intervals map[int64][2]int64) bool {
	switch op.Op {
	case "drop":
		return op.Line >= 1 && op.Line <= len(d.Lines)
	case "supersede":
		if op.Line < 1 || op.Line > len(d.Lines) {
			return false
		}
	case "add":
	default:
		return false
	}
	if n := len([]rune(strings.TrimSpace(op.Text))); n == 0 || n > lineRunes || len(op.Mids) == 0 {
		return false
	}
	for _, m := range op.Mids {
		if _, ok := intervals[m]; !ok {
			return false
		}
	}
	return true
}

func newLine(id int, op Op, pass int, intervals map[int64][2]int64) Line {
	l := Line{ID: id, Text: strings.TrimSpace(op.Text), Mids: slices.Clone(op.Mids), Pass: pass}
	l.First, l.Last = span(op.Mids, intervals)
	return l
}

func span(mids []int64, intervals map[int64][2]int64) (first, last int64) {
	for i, m := range mids {
		iv := intervals[m]
		if i == 0 || iv[0] < first {
			first = iv[0]
		}
		if iv[1] > last {
			last = iv[1]
		}
	}
	return first, last
}

func (d *Document) set(id int, op Op, pass int, intervals map[int64][2]int64) {
	for i := range d.Lines {
		if d.Lines[i].ID == id {
			d.Lines[i] = newLine(id, op, pass, intervals)
		}
	}
}

func (d *Document) remove(id int) {
	d.Lines = slices.DeleteFunc(d.Lines, func(l Line) bool { return l.ID == id })
}

func (d *Document) clone() Document {
	return Document{Lines: slices.Clone(d.Lines), History: slices.Clone(d.History), NextID: d.NextID}
}
