package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// streamRule is which published design a memory stream is run as. The three
// keep the same store — every observation appended as a record with a
// vector, a time and an importance — and differ in what ranks a read and in
// what a write does afterwards, which is the whole of what separates them in
// the papers.
type streamRule string

const (
	// ruleGenerativeAgents is Park et al. 2023 (arXiv 2304.03442, §Memory
	// and Retrieval): every observation enters the stream and nothing ever
	// leaves it; a retrieval scores each record on recency, importance and
	// relevance, min-max normalized and summed at equal weight, and returns
	// the top of that ranking.
	ruleGenerativeAgents streamRule = "generative-agents"
	// ruleMemoryBank is Zhong et al. 2023 (arXiv 2305.10250, §Memory
	// Updating Mechanism): the same stream under the Ebbinghaus forgetting
	// curve. A record's retention is exp(-t/S) in the days since it was last
	// recalled over its strength, retrieval rehearses what it returns —
	// strength up by one, elapsed time back to zero — and a record whose
	// retention falls below the threshold is forgotten.
	ruleMemoryBank streamRule = "memorybank"
	// ruleReflection is Park et al. 2023's second mechanism (§Reflection):
	// the stream, plus a higher-level record synthesized over the last window
	// of observations whenever the importance summed since the previous one
	// passes the trigger, retrievable like any other record.
	ruleReflection streamRule = "reflection"
)

// The constants the papers state, kept where the rule that reads each one
// is declared so no figure in the record rests on a number with no source.
const (
	// recencyDecay is Generative Agents' hourly decay factor: 0.99 raised to
	// the hours since the record was last accessed (arXiv 2304.03442 §A.2).
	recencyDecay = 0.99
	// importanceCeiling is the poignancy scale the same paper scores a
	// record on, 1 to 10, which the importance term is divided by to reach
	// [0,1].
	importanceCeiling = 10.0
	// forgettingThreshold is the retention below which MemoryBank drops a
	// record. The paper states the curve and not a cut, so this is the
	// harness's and is named here rather than buried: it is one half-life,
	// so a record is forgotten once the elapsed days pass its strength.
	forgettingThreshold = 1 / math.E
	// reflectionTrigger is the summed importance of the records seen since
	// the last reflection at which Generative Agents writes the next one,
	// and reflectionWindow is how many of the most recent records that
	// reflection reads. Both are the paper's.
	reflectionTrigger = 150.0
	reflectionWindow  = 100
)

// semanticImportance is what the substituted importance comes to for a turn
// that carries a fact, on the paper's 1-to-10 scale. It is what a reflection
// reads and what a reflection is written at.
const semanticImportance = salienceAddSemantic * importanceCeiling

// streamItem is one record in the stream.
type streamItem struct {
	text   string
	vector []float32
	// created is when the record entered the stream and accessed when a
	// retrieval last returned it. Generative Agents' recency term reads the
	// second; MemoryBank's forgetting curve reads the days since it.
	created  time.Time
	accessed time.Time
	// importance is the poignancy the design scores at write time. In the
	// papers a model assigns it; here it is the salience the shipped
	// writeback would have carried, which is the stand-in the shipped frame
	// already uses and which both arms therefore bracket the same way.
	importance float64
	// strength is MemoryBank's rehearsal count: one at first sight, one more
	// every time a retrieval returns the record.
	strength float64
	// reflected marks a record a reflection wrote, which no later reflection
	// reads.
	reflected bool
}

// streamFrame is the memory stream three published designs keep: every
// observation appended, none addressed, and a read that ranks the whole
// stream against the turn and returns as much of the top of it as the
// context budget holds.
//
// What the seam withholds and each design needs is stated on the rule
// constants and in the record: a query, which the frame takes from the
// turn's text; a limit, which it takes from the shipped recall budget so
// every frame that fills a context fills the same one; and a model at write
// time, which supplies importance in Generative Agents and the synthesis in
// Reflection and is substituted here.
type streamFrame struct {
	rule     streamRule
	budget   int
	embedder Embedder
	items    []*streamItem
	// pending is the importance summed since the last reflection, which is
	// what the reflecting rule triggers on.
	pending float64
}

// NewStreamFrame returns the memory stream under one published design's
// rule. A budget of zero takes the shipped default, which is what the
// document frame takes and what every unconstrained figure is measured at.
func NewStreamFrame(rule streamRule, counter *EmbedCounter, budget int) Frame {
	limit := documentLimit
	if budget > 0 {
		limit = budget * charsPerToken
	}
	return &streamFrame{rule: rule, budget: limit, embedder: counter.Wrap(NewHashingEmbedder())}
}

// Observe appends the turn as a record, and on the reflecting rule
// synthesizes one more once the importance summed since the last synthesis
// passes the trigger.
//
// It reads the text and never the extracted fact: none of the three designs
// addresses anything, so a turn's assertion changes what the record's
// importance is worth and nothing else. That is why these rows move between
// the arms at all.
func (f *streamFrame) Observe(ctx context.Context, t Turn) error {
	importance := gatewaySalienceAdd(len(t.Written) > 0) * importanceCeiling
	if err := f.append(ctx, t.Text, t.At, importance); err != nil {
		return fmt.Errorf("observe turn %d: %w", t.Index, err)
	}
	f.pending += importance
	if f.rule == ruleReflection && f.pending >= reflectionTrigger {
		f.pending = 0
		if err := f.reflect(ctx, t.At); err != nil {
			return fmt.Errorf("reflect at turn %d: %w", t.Index, err)
		}
	}
	if f.rule == ruleMemoryBank {
		f.forget(t.At)
	}
	return nil
}

// append embeds a text and puts it in the stream.
func (f *streamFrame) append(ctx context.Context, text string, at time.Time, importance float64) error {
	vector, err := f.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	f.items = append(f.items, &streamItem{text: text, vector: vector,
		created: at, accessed: at, importance: importance, strength: 1})
	return nil
}

// reflect writes one higher-level record over the last window of
// observations, retrievable like any other.
//
// Generative Agents has a model read the window and write what it concludes.
// The seam has no model, so the synthesis here is the window's own records at
// the higher importance, joined into one — the operation a summarizer
// performs and not the inference one does, which is what the record states
// this row bounds and does not measure. In an arm where no write marks
// anything as carrying a fact, one importance is all there is and the
// synthesis selects nothing.
//
// A reflection is never read into a later reflection and never grows past
// what a context holds. Without both, a synthesis of syntheses doubles at
// every trigger and the structure stops being a memory.
func (f *streamFrame) reflect(ctx context.Context, at time.Time) error {
	from := max(len(f.items)-reflectionWindow, 0)
	var (
		parts []string
		chars int
	)
	for _, item := range f.items[from:] {
		if item.importance < semanticImportance || item.reflected {
			continue
		}
		if chars+len(item.text) > f.budget {
			break
		}
		parts = append(parts, item.text)
		chars += len(item.text) + 1
	}
	if len(parts) == 0 {
		return nil
	}
	if err := f.append(ctx, strings.Join(parts, " "), at, semanticImportance); err != nil {
		return err
	}
	f.items[len(f.items)-1].reflected = true
	return nil
}

// forget drops the records the Ebbinghaus curve has taken below the
// threshold. It runs on every write rather than on a schedule, because a
// schedule is a parameter the paper does not state and the run's own clock
// is what the curve reads.
func (f *streamFrame) forget(at time.Time) {
	kept := f.items[:0]
	for _, item := range f.items {
		if retention(item, at) >= forgettingThreshold {
			kept = append(kept, item)
		}
	}
	f.items = kept
}

// retention is the Ebbinghaus curve MemoryBank keeps a record by: the
// elapsed days since it was last recalled, over the strength its rehearsals
// have built.
func retention(item *streamItem, at time.Time) float64 {
	days := at.Sub(item.accessed).Hours() / 24
	if days <= 0 {
		return 1
	}
	return math.Exp(-days / item.strength)
}

// Context ranks the whole stream against the turn and returns the top of it
// that fits the budget. The read rehearses what it returns, which is
// Generative Agents' access time and MemoryBank's strength.
func (f *streamFrame) Context(ctx context.Context, t Turn) (Injected, error) {
	if len(f.items) == 0 {
		return Injected{}, nil
	}
	query, err := f.embedder.Embed(ctx, t.Text)
	if err != nil {
		return Injected{}, fmt.Errorf("embed the query: %w", err)
	}
	ranked := f.rank(query, t.At)
	var (
		lines []string
		chars int
	)
	for _, item := range ranked {
		if chars+len(item.text) > f.budget && len(lines) > 0 {
			break
		}
		lines = append(lines, item.text)
		chars += len(item.text) + 1
		item.accessed = t.At
		item.strength++
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: len(f.items)}, nil
}

// rank orders the stream by the design's score. Generative Agents and
// Reflection sum recency, importance and relevance at equal weight after
// min-max normalizing each; MemoryBank ranks on relevance alone and leaves
// the time term to the forgetting curve that has already run.
func (f *streamFrame) rank(query []float32, at time.Time) []*streamItem {
	scores := make([]float64, len(f.items))
	relevance := make([]float64, len(f.items))
	for i, item := range f.items {
		relevance[i] = cosine(query, item.vector)
	}
	if f.rule == ruleMemoryBank {
		copy(scores, relevance)
	} else {
		recency := make([]float64, len(f.items))
		importance := make([]float64, len(f.items))
		for i, item := range f.items {
			recency[i] = math.Pow(recencyDecay, at.Sub(item.accessed).Hours())
			importance[i] = item.importance / importanceCeiling
		}
		normalize(recency)
		normalize(importance)
		normalize(relevance)
		for i := range scores {
			scores[i] = recency[i] + importance[i] + relevance[i]
		}
	}
	order := make([]int, len(f.items))
	for i := range order {
		order[i] = i
	}
	// Ties break on the later record, so a run does not depend on the order
	// equal scores happen to sit in.
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
	out := make([]*streamItem, len(order))
	for i, index := range order {
		out[i] = f.items[index]
	}
	return out
}

// Size counts one unit per record. A record costs its text and the vector
// the design retrieves it by, which is storage an addressed store does not
// pay and the shipped store does.
func (f *streamFrame) Size(context.Context) (Size, error) {
	out := Size{Units: len(f.items)}
	for _, item := range f.items {
		out.ContentBytes += int64(len(item.text))
	}
	out.StoredBytes = out.ContentBytes + int64(len(f.items))*vectorBytes
	return out, nil
}

// vectorBytes is what one embedding costs where it is kept: the shipped
// dimension at four bytes a component.
const vectorBytes = EmbeddingDim * 4

// Held is every record's text.
func (f *streamFrame) Held(context.Context) ([]string, error) {
	out := make([]string, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, item.text)
	}
	return out, nil
}

// Forget drops the records whose text it is given.
func (f *streamFrame) Forget(_ context.Context, held []string) error {
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	kept := f.items[:0]
	for _, item := range f.items {
		if !victims[item.text] {
			kept = append(kept, item)
		}
	}
	f.items = kept
	return nil
}

func (f *streamFrame) Close() error { return nil }

// normalize scales a slice into [0,1] by its own minimum and maximum, which
// is the scaling Generative Agents applies to each term before summing. A
// slice with no spread scales to zero, so a term that says nothing
// contributes nothing.
func normalize(values []float64) {
	if len(values) == 0 {
		return
	}
	low, high := values[0], values[0]
	for _, v := range values {
		low = math.Min(low, v)
		high = math.Max(high, v)
	}
	span := high - low
	if span == 0 {
		for i := range values {
			values[i] = 0
		}
		return
	}
	for i := range values {
		values[i] = (values[i] - low) / span
	}
}
