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

// The record has both ends of the extraction bracket and nothing between
// them. With a perfect oracle the addressed frames answer nearly everything;
// with no extraction at all they hold nothing and answer 0.000 at every
// year, because there is nowhere to put a sentence they cannot address. A
// real extractor is somewhere between, and this is where it lands.
//
// Failures are reported by kind rather than as one rate. A fact missed
// leaves an addressed store empty where it should have held something; a
// fact addressed to the wrong place puts something where nothing belongs,
// which corrupts rather than merely omits; a fact invented from a message
// that asserted none fills a store with what was never said. They are three
// different harms and one rate covering them would hide which a structure
// has.
const (
	extractCallLimit = 3_000
	extractCostLimit = 0.40
	// extractMaxTokens bounds one answer. A message asserts at most three
	// facts and each is a short object.
	extractMaxTokens = 300
)

// extractInstruction states the job and the address space. The attribute
// names are given because an addressed store has an address space: what is
// measured here is whether a model finds the facts and puts them in the
// right places, not whether it can invent a key vocabulary and keep to it,
// which is a harder problem and a different one.
var extractInstruction = "You are the write step of a memory system. Memory holds facts at an address, which is a subject and one of its attributes.\n\n" +
	"Read the message and return every fact it asserts about a person. For each, give the subject, the attribute, the value, and whether the statement REPLACES the values already held at that address because they have stopped being true, or JOINS them because it adds a value true at the same time.\n\n" +
	"The attributes are exactly: " + strings.Join(attributeNames(), ", ") + ".\n\n" +
	"A message may assert several facts, one, or none. Small talk asserts none.\n\n" +
	`Answer with JSON only, in this shape: {"facts":[{"subject":"...","attribute":"...","value":"...","operation":"replace"}]}`

// attributeNames is the address space the extractor is given.
func attributeNames() []string {
	out := make([]string, 0, len(attributes))
	for _, a := range attributes {
		out = append(out, a.name)
	}
	return out
}

// Extracted is what a model read out of one message, beside what the
// workload put in it.
type Extracted struct {
	Index int    `json:"turn"`
	Text  string `json:"text"`
	// Planted is what the message actually asserts and Found is what the
	// model returned; the failure counts below are read from the two.
	Planted []Written `json:"planted"`
	Found   []Written `json:"found"`
}

// Extraction is the paid artifact for one shape: what the model read out of
// every message, and what the calls cost.
type Extraction struct {
	Shape            string      `json:"shape"`
	Model            string      `json:"model"`
	Seed             int64       `json:"seed"`
	Calls            int         `json:"calls"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	PriceInPerM      float64     `json:"price_input_per_million_usd"`
	PriceOutPerM     float64     `json:"price_output_per_million_usd"`
	PriceRead        string      `json:"price_read_on"`
	CostUSD          float64     `json:"cost_usd"`
	Messages         []Extracted `json:"messages"`
}

// Extract asks a model to read every message of a workload.
func Extract(ctx context.Context, w Workload, cachePath string) (Extraction, error) {
	out := Extraction{
		Shape: w.Params.Shape, Model: declareModel, Seed: w.Seed, Calls: len(w.Turns),
		PriceInPerM: declarePriceIn, PriceOutPerM: declarePriceOut, PriceRead: declarePriceRead,
	}
	if len(w.Turns) > extractCallLimit {
		return Extraction{}, fmt.Errorf(
			"extracting needs %d calls and is bounded at %d: measure a smaller workload or raise the bound deliberately",
			len(w.Turns), extractCallLimit)
	}
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return Extraction{}, fmt.Errorf("extracting needs %s in the environment", declareKeyEnv)
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return Extraction{}, err
	}
	client := &declareClient{key: key, http: &http.Client{Timeout: declareTimeout}}

	messages := make([]Extracted, len(w.Turns))
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
				prompt := "Message: " + w.Turns[i].Text
				mu.Lock()
				cached, ok := cache[prompt]
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				if ok {
					mu.Lock()
					messages[i].Found = parseExtraction(cached)
					out.Calls--
					mu.Unlock()
					continue
				}
				answer, usage, err := client.extract(ctx, prompt)
				mu.Lock()
				if err != nil && failure == nil {
					failure = err
				}
				if err == nil {
					messages[i].Found = parseExtraction(answer)
					cache[prompt] = answer
					out.PromptTokens += usage.Prompt
					out.CompletionTokens += usage.Completion
				}
				mu.Unlock()
			}
		}()
	}
	for i, t := range w.Turns {
		messages[i].Index, messages[i].Text, messages[i].Planted = t.Index, t.Text, t.Written
	}
	for i := range w.Turns {
		work <- i
	}
	close(work)
	wg.Wait()

	if err := writeDeclarationCache(cachePath, cache); err != nil {
		return Extraction{}, err
	}
	if failure != nil {
		return Extraction{}, failure
	}
	out.Messages = messages
	out.CostUSD = declareCost(out.PromptTokens, out.CompletionTokens)
	if out.CostUSD > extractCostLimit {
		return Extraction{}, fmt.Errorf("extracting spent $%.4f against a bound of $%.2f",
			out.CostUSD, extractCostLimit)
	}
	return out, nil
}

// extract sends one message and reads back what the model found in it.
func (c *declareClient) extract(ctx context.Context, prompt string) (string, declareUsage, error) {
	body, err := json.Marshal(map[string]any{
		"model": declareModel,
		"messages": []map[string]string{
			{"role": "system", "content": extractInstruction},
			{"role": "user", "content": prompt},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": extractMaxTokens,
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
		return "", declareUsage{}, fmt.Errorf("extract: %w", err)
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
		return "", declareUsage{}, fmt.Errorf("extract: %s: %s", resp.Status, decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", declareUsage{}, fmt.Errorf("extract: the provider returned no choice")
	}
	return decoded.Choices[0].Message.Content,
		declareUsage{Prompt: decoded.Usage.PromptTokens, Completion: decoded.Usage.CompletionTokens}, nil
}

// parseExtraction reads the model's JSON. An answer that will not parse is
// read as having found nothing, which counts every fact in that message as
// missed rather than passing it over.
func parseExtraction(answer string) []Written {
	var decoded struct {
		Facts []struct {
			Subject   string `json:"subject"`
			Attribute string `json:"attribute"`
			Value     string `json:"value"`
			Operation string `json:"operation"`
		} `json:"facts"`
	}
	if err := json.Unmarshal([]byte(answer), &decoded); err != nil {
		return nil
	}
	out := make([]Written, 0, len(decoded.Facts))
	for _, one := range decoded.Facts {
		if one.Subject == "" || one.Attribute == "" || one.Value == "" {
			continue
		}
		out = append(out, Written{
			Fact: Fact{
				Address: Address{Subject: one.Subject, Attribute: strings.ToLower(one.Attribute)},
				Value:   strings.ToLower(one.Value),
			},
			Declared: parseOperation(one.Operation),
		})
	}
	return out
}

// ExtractionScore is what a model's reading of a workload got right and the
// three ways it got things wrong.
type ExtractionScore struct {
	Shape            string `json:"shape"`
	Model            string `json:"model"`
	Messages         int    `json:"messages"`
	FactMessages     int    `json:"fact_messages"`
	Planted          int    `json:"planted_assertions"`
	Found            int    `json:"assertions_returned"`
	Correct          int    `json:"correct"`
	Missed           int    `json:"missed"`
	Misaddressed     int    `json:"misaddressed"`
	Invented         int    `json:"invented"`
	CorrectRate      Figure `json:"correct_rate"`
	MissedRate       Figure `json:"missed_rate"`
	MisaddressedRate Figure `json:"misaddressed_rate"`
	// InventedPerMessage is counted against messages rather than against
	// planted facts: a message asserting nothing can still have something
	// invented from it, and dividing by facts would hide exactly that case.
	InventedPerMessage Figure `json:"invented_per_message"`
	// Whether the failures inside one message are independent of each
	// other. A rate per assertion cannot tell three separate errors from
	// one message read wrong, and those are different failures: the second
	// says a message is read whole or not at all. Both shares are over the
	// messages that assert something, so on a shape putting one assertion
	// in a message they are the correct rate and its complement.
	MessagesRight Figure `json:"messages_read_entirely_right"`
	MessagesWrong Figure `json:"messages_read_entirely_wrong"`
	// Declaration accuracy over the assertions the model both found and
	// addressed correctly, which is the only population where the question
	// #463 asked can still be asked.
	DeclaredCorrect int    `json:"declared_correct"`
	DeclaredRate    Figure `json:"declared_rate"`
	// Wordings and Classes cut the correct rate the way #463 cut its
	// declaration accuracy, so the two tables read against each other.
	Wordings []CutScore `json:"wordings"`
	Classes  []CutScore `json:"classes"`
	CostUSD  float64    `json:"cost_usd"`
	Calls    int        `json:"calls"`
	Tokens   int        `json:"tokens"`
}

// ScoreExtraction reads a committed extraction against the workload. It
// costs nothing.
func ScoreExtraction(w Workload, e Extraction) ExtractionScore {
	out := ExtractionScore{Shape: e.Shape, Model: e.Model, Messages: len(e.Messages),
		CostUSD: e.CostUSD, Calls: e.Calls, Tokens: e.PromptTokens + e.CompletionTokens}
	// wordingOf and classOf come from the workload, so a found assertion is
	// cut by what the workload knows about it rather than by anything the
	// model said.
	wordingOf, classOf := map[string]string{}, map[string]string{}
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			wordingOf[a.Value] = a.Wording
			classOf[a.Value] = thread.Class
		}
	}
	byWording, byClass := map[string][]float64{}, map[string][]float64{}
	var correct, missed, misaddressed, invented, declared []float64
	var messagesRight, messagesWrong []float64

	for _, m := range e.Messages {
		var rightHere int
		if len(m.Planted) > 0 {
			out.FactMessages++
		}
		out.Planted += len(m.Planted)
		out.Found += len(m.Found)
		plantedValues := map[string]Written{}
		for _, one := range m.Planted {
			plantedValues[one.Value] = one
		}
		foundAt := map[string]Written{}
		for _, one := range m.Found {
			if _, planted := plantedValues[one.Value]; planted {
				foundAt[one.Value] = one
				continue
			}
			out.Invented++
		}
		invented = append(invented, float64(len(m.Found)-len(foundAt)))
		for _, one := range m.Planted {
			found, ok := foundAt[one.Value]
			right := ok && found.Address == one.Address
			correct = append(correct, boolAsFloat(right))
			missed = append(missed, boolAsFloat(!ok))
			misaddressed = append(misaddressed, boolAsFloat(ok && found.Address != one.Address))
			switch {
			case right:
				out.Correct++
				rightHere++
			case ok:
				out.Misaddressed++
			default:
				out.Missed++
			}
			if right {
				got := found.Declared == one.Declared
				declared = append(declared, boolAsFloat(got))
				if got {
					out.DeclaredCorrect++
				}
			}
			byWording[wordingOf[one.Value]] = append(byWording[wordingOf[one.Value]], boolAsFloat(right))
			byClass[classOf[one.Value]] = append(byClass[classOf[one.Value]], boolAsFloat(right))
		}
		if len(m.Planted) > 0 {
			messagesRight = append(messagesRight, boolAsFloat(rightHere == len(m.Planted)))
			messagesWrong = append(messagesWrong, boolAsFloat(rightHere == 0))
		}
	}
	out.CorrectRate = NewFigure(correct)
	out.MissedRate = NewFigure(missed)
	out.MisaddressedRate = NewFigure(misaddressed)
	out.InventedPerMessage = NewFigure(invented)
	out.DeclaredRate = NewFigure(declared)
	out.MessagesRight = NewFigure(messagesRight)
	out.MessagesWrong = NewFigure(messagesWrong)
	out.Wordings = cutFrom(byWording, wordingNames)
	out.Classes = cutFrom(byClass, classNames)
	return out
}

// cutFrom turns gathered per-slice values into the record's cut scores, in
// the order the names are given.
func cutFrom(gathered map[string][]float64, names []string) []CutScore {
	out := make([]CutScore, 0, len(names))
	for _, name := range names {
		values := gathered[name]
		if len(values) == 0 {
			continue
		}
		out = append(out, CutScore{Name: name, Asked: len(values), Accuracy: NewFigure(values)})
	}
	return out
}

// Observed is the turn stream as the extractor read it: the same messages,
// carrying what a model found in them instead of what the workload put
// there. It is the middle of the bracket, run through every frame.
func (e Extraction) Observed(w Workload) Workload {
	found := make(map[int][]Written, len(e.Messages))
	for _, m := range e.Messages {
		found[m.Index] = m.Found
	}
	out := w
	out.Turns = make([]Turn, len(w.Turns))
	for i, t := range w.Turns {
		t.Written = found[t.Index]
		out.Turns[i] = t
	}
	return out
}
