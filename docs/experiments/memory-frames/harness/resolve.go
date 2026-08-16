package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// An addressed store reads one place, and a question in words names none.
// Something has to turn the words into an address, and this is that step
// measured rather than assumed: one model call per question, answering with
// an address out of the same address space the write side is given.
//
// It sits in front of the store rather than inside it. A ranking function
// and a threshold bolted onto the store would be a different structure than
// the one this record recommends, and measuring that would answer a
// question nobody asked; a step that reads the words is the read-side twin
// of the writeback that already reads them.
const (
	resolveCallLimit = 1_000
	resolveCostLimit = 0.10
	// resolveMaxTokens bounds one answer. The answer is a small object.
	resolveMaxTokens = 60
)

// resolveInstruction states the job and the address space. The attribute
// names are given for the reason the extractor is given them: what is
// measured is whether a model puts a question at the right address among
// names it was handed, not whether it can invent a key vocabulary.
var resolveInstruction = "You are the read step of a memory system. Memory holds facts at an address, which is a subject and one of its attributes.\n\n" +
	"Read the question and return the address it asks about: the subject it names, and the attribute it asks for.\n\n" +
	"The attributes are exactly: " + strings.Join(attributeNames(), ", ") + ".\n\n" +
	"A question that asks about a person in general, rather than about one of their attributes, gives the subject and leaves the attribute empty.\n\n" +
	`Answer with JSON only, in this shape: {"subject":"...","attribute":"..."}`

// The kinds of question a resolver is asked. They are reported apart
// because they are different jobs: one names an attribute and one spans
// every attribute a subject has.
const (
	kindThread  = "thread"
	kindSubject = "subject"
)

// kindNames is the order every table reports the kinds in.
var kindNames = []string{kindThread, kindSubject}

// Question is one question the finding run asks, with the address the
// workload generated it from. Attribute is empty where the question asks
// about a subject and no attribute.
type Question struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
	Address
}

// Questions is every distinct question the finding run puts to a frame:
// one per thread, and one per subject the workload ever asserts about.
func Questions(w Workload) []Question {
	var out []Question
	seen := map[string]bool{}
	for _, thread := range w.Threads {
		if seen[thread.Question] {
			continue
		}
		seen[thread.Question] = true
		out = append(out, Question{Text: thread.Question, Kind: kindThread,
			Address: Address{Subject: thread.Subject, Attribute: thread.Attribute}})
	}
	asked := map[string]bool{}
	for _, thread := range w.Threads {
		asked[thread.Subject] = true
	}
	for _, subject := range subjects {
		if !asked[subject] {
			continue
		}
		out = append(out, Question{Text: subjectQuestion(subject), Kind: kindSubject,
			Address: Address{Subject: subject}})
	}
	return out
}

// Resolution is one question with the address a model read out of it,
// beside the address the workload generated it from.
type Resolution struct {
	Question       string `json:"question"`
	Kind           string `json:"kind"`
	Subject        string `json:"subject"`
	Attribute      string `json:"attribute"`
	FoundSubject   string `json:"found_subject"`
	FoundAttribute string `json:"found_attribute"`
}

// Correct reports whether the model reached the address the question was
// generated from.
func (r Resolution) Correct() bool {
	return r.FoundSubject == r.Subject && r.FoundAttribute == r.Attribute
}

// Resolutions is the paid artifact: what the model answered for every
// question, and what the calls cost.
type Resolutions struct {
	Model            string       `json:"model"`
	Seed             int64        `json:"seed"`
	Calls            int          `json:"calls"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	PriceInPerM      float64      `json:"price_input_per_million_usd"`
	PriceOutPerM     float64      `json:"price_output_per_million_usd"`
	PriceRead        string       `json:"price_read_on"`
	CostUSD          float64      `json:"cost_usd"`
	Resolutions      []Resolution `json:"resolutions"`
}

// Resolve asks the model for every question and returns what it answered
// with what the calls cost. It refuses to start a run past the bound.
func Resolve(ctx context.Context, w Workload, cachePath string) (Resolutions, error) {
	asked := Questions(w)
	out := Resolutions{
		Model: declareModel, Seed: w.Seed, Calls: len(asked),
		PriceInPerM: declarePriceIn, PriceOutPerM: declarePriceOut, PriceRead: declarePriceRead,
	}
	if len(asked) > resolveCallLimit {
		return Resolutions{}, fmt.Errorf(
			"resolving needs %d calls and is bounded at %d: measure a smaller workload or raise the bound deliberately",
			len(asked), resolveCallLimit)
	}
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return Resolutions{}, fmt.Errorf("resolving needs %s in the environment", declareKeyEnv)
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return Resolutions{}, err
	}
	client := &declareClient{key: key, http: &http.Client{Timeout: declareTimeout}}

	resolved := make([]Resolution, len(asked))
	for i, question := range asked {
		resolved[i] = Resolution{Question: question.Text, Kind: question.Kind,
			Subject: question.Subject, Attribute: question.Attribute}
	}
	var (
		mu      sync.Mutex
		failure error
		wg      sync.WaitGroup
	)
	work := make(chan int)
	for range declareConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				prompt := "Question: " + asked[i].Text
				mu.Lock()
				cached, ok := cache[prompt]
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				if ok {
					mu.Lock()
					resolved[i] = withAnswer(resolved[i], cached)
					out.Calls--
					mu.Unlock()
					continue
				}
				answer, usage, err := client.resolve(ctx, prompt)
				mu.Lock()
				if err != nil && failure == nil {
					failure = err
				}
				if err == nil {
					resolved[i] = withAnswer(resolved[i], answer)
					cache[prompt] = answer
					out.PromptTokens += usage.Prompt
					out.CompletionTokens += usage.Completion
				}
				mu.Unlock()
			}
		}()
	}
	for i := range asked {
		work <- i
	}
	close(work)
	wg.Wait()

	if err := writeDeclarationCache(cachePath, cache); err != nil {
		return Resolutions{}, err
	}
	if failure != nil {
		return Resolutions{}, failure
	}
	out.Resolutions = resolved
	out.CostUSD = declareCost(out.PromptTokens, out.CompletionTokens)
	if out.CostUSD > resolveCostLimit {
		return Resolutions{}, fmt.Errorf("resolving spent $%.4f against a bound of $%.2f",
			out.CostUSD, resolveCostLimit)
	}
	return out, nil
}

// resolve sends one question and reads back the address the model puts it
// at.
func (c *declareClient) resolve(ctx context.Context, prompt string) (string, declareUsage, error) {
	body, err := json.Marshal(map[string]any{
		"model": declareModel,
		"messages": []map[string]string{
			{"role": "system", "content": resolveInstruction},
			{"role": "user", "content": prompt},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": resolveMaxTokens,
		"response_format":       map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", declareUsage{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, declareBaseURL+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return "", declareUsage{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.do(ctx, req, body)
	if err != nil {
		return "", declareUsage{}, fmt.Errorf("resolve: %w", err)
	}
	defer resp.Body.Close()
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", declareUsage{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", declareUsage{}, fmt.Errorf("resolve: %s: %s", resp.Status, decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", declareUsage{}, fmt.Errorf("resolve: the provider returned no choice")
	}
	return decoded.Choices[0].Message.Content,
		declareUsage{Prompt: decoded.Usage.PromptTokens, Completion: decoded.Usage.CompletionTokens}, nil
}

// withAnswer reads the model's JSON onto the question it answered. An
// answer that will not parse leaves the address empty, which is a question
// the model could not place rather than one it placed wrongly.
func withAnswer(r Resolution, answer string) Resolution {
	var decoded struct {
		Subject   string `json:"subject"`
		Attribute string `json:"attribute"`
	}
	if err := json.Unmarshal([]byte(answer), &decoded); err != nil {
		return r
	}
	r.FoundSubject = canonicalSubject(decoded.Subject)
	r.FoundAttribute = strings.ToLower(strings.TrimSpace(decoded.Attribute))
	return r
}

// canonicalSubject spells a subject the way the store holds it. It is the
// same normalisation the write side applies to an attribute and a value,
// and without it the comparison would be one of capitalisation.
func canonicalSubject(name string) string {
	name = strings.TrimSpace(name)
	for _, subject := range subjects {
		if strings.EqualFold(subject, name) {
			return subject
		}
	}
	return name
}

// ResolutionScore is what a model made of the questions, with the ways it
// failed reported apart from one another. A question placed at the wrong
// address is answered from a place that holds something else, which is a
// different harm from one it could not place at all.
type ResolutionScore struct {
	Model            string  `json:"model"`
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	PriceInPerM      float64 `json:"price_input_per_million_usd"`
	PriceOutPerM     float64 `json:"price_output_per_million_usd"`
	PriceRead        string  `json:"price_read_on"`
	CostUSD          float64 `json:"cost_usd"`
	Asked            int     `json:"asked"`
	Correct          int     `json:"correct"`
	WrongSubject     int     `json:"wrong_subject"`
	WrongAttribute   int     `json:"wrong_attribute"`
	Unplaced         int     `json:"unplaced"`
	CorrectRate      Figure  `json:"correct_rate"`
	WrongSubjectRate Figure  `json:"wrong_subject_rate"`
	WrongAttrRate    Figure  `json:"wrong_attribute_rate"`
	UnplacedRate     Figure  `json:"unplaced_rate"`
	// Kinds is the correct rate cut by what the question asks for, which is
	// where a question spanning a whole subject is told from one naming an
	// attribute.
	Kinds []CutScore `json:"kinds"`
}

// ScoreResolutions reads the committed answers against the addresses the
// workload generated the questions from. It costs nothing.
func ScoreResolutions(r Resolutions) ResolutionScore {
	out := ResolutionScore{
		Model: r.Model, Calls: r.Calls,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
		PriceInPerM: r.PriceInPerM, PriceOutPerM: r.PriceOutPerM, PriceRead: r.PriceRead,
		CostUSD: r.CostUSD, Asked: len(r.Resolutions),
	}
	var correct, wrongSubject, wrongAttribute, unplaced []float64
	byKind := map[string][]float64{}
	for _, one := range r.Resolutions {
		right := one.Correct()
		correct = append(correct, boolAsFloat(right))
		wrongSubject = append(wrongSubject, boolAsFloat(one.FoundSubject != "" && one.FoundSubject != one.Subject))
		wrongAttribute = append(wrongAttribute,
			boolAsFloat(one.FoundSubject == one.Subject && one.FoundAttribute != one.Attribute))
		unplaced = append(unplaced, boolAsFloat(one.FoundSubject == ""))
		switch {
		case right:
			out.Correct++
		case one.FoundSubject == "":
			out.Unplaced++
		case one.FoundSubject != one.Subject:
			out.WrongSubject++
		default:
			out.WrongAttribute++
		}
		byKind[one.Kind] = append(byKind[one.Kind], boolAsFloat(right))
	}
	out.CorrectRate = NewFigure(correct)
	out.WrongSubjectRate = NewFigure(wrongSubject)
	out.WrongAttrRate = NewFigure(wrongAttribute)
	out.UnplacedRate = NewFigure(unplaced)
	out.Kinds = cutFrom(byKind, kindNames)
	return out
}

// Resolving is how a question's words become an address. A run has exactly
// one of them, and a run without a committed resolution has none: the
// resolved form is not measured rather than measured as something else.
type Resolving func(question string) *Address

// Resolving returns the read step these answers describe. A question the
// model could not place resolves to no address, which is what a frame faces
// when the step in front of it fails.
func (r Resolutions) Resolving() Resolving {
	answers := make(map[string]*Address, len(r.Resolutions))
	for _, one := range r.Resolutions {
		if one.FoundSubject == "" {
			continue
		}
		answers[one.Question] = &Address{Subject: one.FoundSubject, Attribute: one.FoundAttribute}
	}
	return func(question string) *Address { return answers[question] }
}
