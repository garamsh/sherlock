package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// What the second paid measurement is allowed to spend. It asks only the
// assertions whose declaration has a consequence, and asks each of them a
// few times.
const (
	resampleSamples     = 3
	resampleTemperature = 1
	resampleCallLimit   = 800
	resampleCostLimit   = 0.10
)

// Resampled is one assertion asked several times over.
type Resampled struct {
	Subject   string      `json:"subject"`
	Attribute string      `json:"attribute"`
	Value     string      `json:"value"`
	Class     string      `json:"class"`
	Wording   string      `json:"wording"`
	Year      int         `json:"year"`
	Truth     Operation   `json:"truth"`
	Samples   []Operation `json:"samples"`
}

// Unanimous reports whether every sample gave the same answer.
func (r Resampled) Unanimous() bool {
	for _, sample := range r.Samples[1:] {
		if sample != r.Samples[0] {
			return false
		}
	}
	return true
}

// Wrong reports whether the answer the samples agree on is the wrong one.
// A split answer is not counted either way.
func (r Resampled) Wrong() bool { return r.Unanimous() && r.Samples[0] != r.Truth }

// Stability is whether asking the same question again gets the same answer.
//
// It decides whether an uncertainty signal exists at all. A bias toward the
// cheaper failure needs something to bias on, and the obvious candidate is
// the model's own doubt. If a wrong answer is given again every time it is
// asked, the model is not in doubt and there is nothing to read — not that
// the signal is hard to extract, but that it is not there. That is a
// stronger statement than declining to build a confidence threshold, and it
// is the reason this measurement was worth paying for.
type Stability struct {
	Model            string     `json:"model"`
	Samples          int        `json:"samples_per_assertion"`
	Temperature      int        `json:"temperature"`
	Calls            int        `json:"calls"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	CostUSD          float64    `json:"cost_usd"`
	Asked            int        `json:"asked"`
	Unanimous        Figure     `json:"unanimous"`
	UnanimousWrong   int        `json:"unanimously_wrong"`
	Split            int        `json:"split"`
	Wordings         []CutScore `json:"wordings_unanimous"`
	// SplitAmongWrong and SplitAmongRight are what decide whether the
	// samples disagreeing says anything about the answer being wrong. Two
	// equal figures would mean disagreement is noise and there is no doubt
	// to read; a gap means there is one, and how wide it is says what
	// reading it could buy.
	SplitAmongWrong Figure `json:"split_among_wrong"`
	SplitAmongRight Figure `json:"split_among_right"`
	// Recovers and Costs are what joining on a split would do: destroying
	// errors it turns back into joins, against correct replacements it
	// wrongly turns into joins.
	Recovers int         `json:"split_join_recovers"`
	Costs    int         `json:"split_join_costs"`
	Answers  []Resampled `json:"answers"`
}

// Resample asks every assertion whose declaration has a consequence, a few
// times each, at a temperature that lets the answer vary if anything about
// it is uncertain.
func Resample(ctx context.Context, w Workload, cachePath string) (Stability, error) {
	var asked []Declaration
	for _, one := range Asked(w) {
		if len(one.Held) > 0 {
			asked = append(asked, one)
		}
	}
	calls := len(asked) * resampleSamples
	out := Stability{
		Model: declareModel, Samples: resampleSamples, Temperature: resampleTemperature,
		Calls: calls, Asked: len(asked),
	}
	if calls > resampleCallLimit {
		return Stability{}, fmt.Errorf(
			"resampling needs %d calls and is bounded at %d: narrow what is asked or raise the bound deliberately",
			calls, resampleCallLimit)
	}
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return Stability{}, fmt.Errorf("resampling needs %s in the environment", declareKeyEnv)
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return Stability{}, err
	}
	client := &declareClient{key: key, http: &http.Client{Timeout: declareTimeout},
		temperature: resampleTemperature}

	answers := make([]Resampled, len(asked))
	type job struct{ assertion, sample int }
	var (
		mu      sync.Mutex
		failure error
		wg      sync.WaitGroup
	)
	work := make(chan job)
	for range declareConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				// The sample number is part of the cache key, so asking the
				// same question a second time is a second question rather
				// than the first one's answer read back.
				prompt := declarePrompt(asked[j.assertion])
				cacheKey := fmt.Sprintf("resample %d\n%s", j.sample, prompt)
				mu.Lock()
				cached, ok := cache[cacheKey]
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				if ok {
					mu.Lock()
					answers[j.assertion].Samples[j.sample] = Operation(cached)
					out.Calls--
					mu.Unlock()
					continue
				}
				answer, usage, err := client.declare(ctx, prompt)
				mu.Lock()
				if err != nil && failure == nil {
					failure = err
				}
				if err == nil {
					answers[j.assertion].Samples[j.sample] = answer
					cache[cacheKey] = string(answer)
					out.PromptTokens += usage.Prompt
					out.CompletionTokens += usage.Completion
				}
				mu.Unlock()
			}
		}()
	}
	for i, one := range asked {
		answers[i] = Resampled{
			Subject: one.Subject, Attribute: one.Attribute, Value: one.Value,
			Class: one.Class, Wording: one.Wording, Year: one.Year, Truth: one.Truth,
			Samples: make([]Operation, resampleSamples),
		}
	}
	for i := range asked {
		for sample := range resampleSamples {
			work <- job{assertion: i, sample: sample}
		}
	}
	close(work)
	wg.Wait()

	if err := writeDeclarationCache(cachePath, cache); err != nil {
		return Stability{}, err
	}
	if failure != nil {
		return Stability{}, failure
	}
	out.Answers = answers
	out.CostUSD = declareCost(out.PromptTokens, out.CompletionTokens)
	if out.CostUSD > resampleCostLimit {
		return Stability{}, fmt.Errorf("resampling spent $%.4f against a bound of $%.2f",
			out.CostUSD, resampleCostLimit)
	}
	return ScoreStability(out), nil
}

// ScoreStability reads the samples. It costs nothing and runs from the
// committed answers.
func ScoreStability(s Stability) Stability {
	var unanimous []float64
	s.UnanimousWrong, s.Split = 0, 0
	for _, one := range s.Answers {
		unanimous = append(unanimous, boolAsFloat(one.Unanimous()))
		switch {
		case !one.Unanimous():
			s.Split++
		case one.Wrong():
			s.UnanimousWrong++
		}
	}
	s.Unanimous = NewFigure(unanimous)
	s.Asked = len(s.Answers)

	var wrongSplit, rightSplit []float64
	s.Recovers, s.Costs = 0, 0
	for _, one := range s.Answers {
		answer, agreed := one.Majority()
		if answer == one.Truth {
			rightSplit = append(rightSplit, boolAsFloat(!agreed))
		} else {
			wrongSplit = append(wrongSplit, boolAsFloat(!agreed))
		}
		if agreed {
			continue
		}
		if one.Truth == OperationJoin {
			s.Recovers++
			continue
		}
		s.Costs++
	}
	s.SplitAmongWrong = NewFigure(wrongSplit)
	s.SplitAmongRight = NewFigure(rightSplit)

	s.Wordings = nil
	for _, name := range wordingNames {
		cut := CutScore{Name: name}
		var agreed []float64
		for _, one := range s.Answers {
			if one.Wording != name {
				continue
			}
			cut.Asked++
			agreed = append(agreed, boolAsFloat(one.Unanimous()))
		}
		if cut.Asked == 0 {
			continue
		}
		cut.Accuracy = NewFigure(agreed)
		s.Wordings = append(s.Wordings, cut)
	}
	return s
}

// Majority is the answer most of the samples gave, and whether they all
// gave it. Samples are read in order, so a tie takes the first answer seen
// and the result does not depend on anything but the committed samples.
func (r Resampled) Majority() (Operation, bool) {
	counts := map[Operation]int{}
	for _, sample := range r.Samples {
		counts[sample]++
	}
	best, top := r.Samples[0], 0
	for _, sample := range r.Samples {
		if counts[sample] > top {
			best, top = sample, counts[sample]
		}
	}
	return best, top == len(r.Samples)
}

// Declaring is the one way found to err toward the cheaper failure without
// asking the model for a number: ask it more than once and treat the
// samples disagreeing as the doubt. The answer is what most samples said,
// unless they split, and a split resolves to a join.
//
// It is a mechanism — three calls where there was one — and its sample
// count is a knob. What the tables say about it is the point rather than
// what it is called: it is measured beside blind biasing and beside taking
// the declaration, and a reader can see what each buys.
func (s Stability) Declaring(base Declarations) Declaring {
	inner := base.Declaring()
	resampled := make(map[string]Operation, len(s.Answers))
	for _, one := range s.Answers {
		key := Declaration{Subject: one.Subject, Attribute: one.Attribute,
			Value: one.Value, Year: one.Year}.key()
		answer, unanimous := one.Majority()
		if !unanimous {
			answer = OperationJoin
		}
		resampled[key] = answer
	}
	return func(t Turn) Turn {
		t = inner(t)
		written := make([]Written, len(t.Written))
		copy(written, t.Written)
		for i := range written {
			key := Declaration{Subject: written[i].Subject, Attribute: written[i].Attribute,
				Value: written[i].Value, Year: t.Day/daysPerYear + 1}.key()
			if answer, ok := resampled[key]; ok {
				written[i].Declared = answer
			}
		}
		t.Written = written
		return t
	}
}
