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

// A question-shaped address is formed by whatever writes to the store, and
// in production that is a model reading a sentence. `question.go` forms one
// by a rule, which proves the address is derivable from the sentence and
// says nothing about whether a model derives the same one. This asks it.
//
// It is the same 840 assertions the declaring pass asks about, from the
// same model on the same day, so the two answers are comparable: one buys
// the operation and the other buys the address, and the record reports what
// each is worth against the same sentences. The model is not asked for the
// operation here, because a question address having to be declared as well
// is the hypothesis failing rather than the measurement missing something.
const (
	addressCallLimit = 1_000
	addressCostLimit = 0.25
	// addressMaxTokens bounds one answer. It is a small JSON object with
	// two short strings in it.
	addressMaxTokens = 60
)

// addressInstruction states the job and hands over the address space, the
// way the extractor is handed the attribute names: what is asked is whether
// a model puts a fact at the right question, not whether it can invent a set
// of questions and keep to them.
//
// It states what a restriction is not, because the first version did not and
// most of its answers came back with the value in that field. That is a
// defect in the instruction rather than an answer worth publishing, so the
// pass was re-bought; what it deliberately does not say is which sentences
// carry a restriction, which is the judgement under measurement.
var addressInstruction = "You are the write step of a memory system. Memory holds a fact at the question it answers.\n\n" +
	"Read the statement and answer with the question it answers, chosen from this list, and with the restriction the statement places on that question.\n\n" +
	"The questions are exactly: " + strings.Join(questionKeys(), ", ") + ".\n\n" +
	"A restriction is a phrase in the statement that narrows what is being asked: never the subject and never the answer itself, " +
	"but other material limiting when or for what the statement holds. " +
	"Give it in the statement's own words, lower-cased. Where the statement narrows nothing, give an empty string.\n\n" +
	`Answer with JSON only, in this shape: {"question":"...","restriction":"..."}`

// questionKeys is the address space the model is given, in the vocabulary's
// order.
func questionKeys() []string {
	out := make([]string, 0, len(attributes))
	for _, a := range attributes {
		out = append(out, questionStems[a.name])
	}
	return out
}

// Addressed is one assertion's address as a model formed it, beside the
// address the rule forms from the same sentence.
type Addressed struct {
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
	Class     string `json:"class"`
	Wording   string `json:"wording"`
	Text      string `json:"text"`
	// Formed is what `question.go` reads out of the sentence and Answered
	// is what the model read out of it.
	Formed   Address `json:"formed"`
	Answered Address `json:"answered"`
}

// Right reports whether the model put the fact where the rule puts it.
func (a Addressed) Right() bool { return a.Answered == a.Formed }

// QuestionRight reports whether the model chose the question the rule
// chooses, whatever it did with the restriction.
func (a Addressed) QuestionRight() bool { return a.Answered.Attribute == a.Formed.Attribute }

// Addresses is the paid artifact: what the model answered for every
// assertion, and what the calls cost.
type Addresses struct {
	Model            string      `json:"model"`
	Seed             int64       `json:"seed"`
	Calls            int         `json:"calls"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	PriceInPerM      float64     `json:"price_input_per_million_usd"`
	PriceOutPerM     float64     `json:"price_output_per_million_usd"`
	PriceRead        string      `json:"price_read_on"`
	CostUSD          float64     `json:"cost_usd"`
	Addressed        []Addressed `json:"addressed"`
}

// AskedToAddress is every assertion the model is asked to address: the same
// population the declaring pass asks about, so the two are read against each
// other.
func AskedToAddress(w Workload) []Addressed {
	var out []Addressed
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			out = append(out, Addressed{
				Subject: thread.Subject, Attribute: thread.Attribute, Value: a.Value,
				Class: thread.Class, Wording: a.Wording, Text: a.Text,
				Formed: QuestionAddress(a.Text, thread.Subject, a.Value, thread.Attribute),
			})
		}
	}
	return out
}

// AskToAddress asks the model where every assertion goes and returns what it
// answered with what the calls cost. It refuses to start a run past the
// bound.
func AskToAddress(ctx context.Context, w Workload, cachePath string) (Addresses, error) {
	asked := AskedToAddress(w)
	out := Addresses{
		Model: declareModel, Seed: w.Seed, Calls: len(asked),
		PriceInPerM: declarePriceIn, PriceOutPerM: declarePriceOut, PriceRead: declarePriceRead,
	}
	if len(asked) > addressCallLimit {
		return Addresses{}, fmt.Errorf(
			"addressing needs %d calls and is bounded at %d: measure a smaller workload or raise the bound deliberately",
			len(asked), addressCallLimit)
	}
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return Addresses{}, fmt.Errorf("addressing needs %s in the environment", declareKeyEnv)
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return Addresses{}, err
	}
	client := &declareClient{key: key, http: &http.Client{Timeout: declareTimeout}}

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
				prompt := "Subject: " + asked[i].Subject + "\nStatement: " + asked[i].Text
				mu.Lock()
				cached, ok := cache[prompt]
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				if ok {
					mu.Lock()
					asked[i].Answered = parseAddress(cached, asked[i].Subject)
					out.Calls--
					mu.Unlock()
					continue
				}
				answer, usage, err := client.address(ctx, prompt)
				mu.Lock()
				if err != nil && failure == nil {
					failure = err
				}
				if err == nil {
					asked[i].Answered = parseAddress(answer, asked[i].Subject)
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
		return Addresses{}, err
	}
	if failure != nil {
		return Addresses{}, failure
	}
	out.Addressed = asked
	out.CostUSD = declareCost(out.PromptTokens, out.CompletionTokens)
	if out.CostUSD > addressCostLimit {
		return Addresses{}, fmt.Errorf("addressing spent $%.4f against a bound of $%.2f",
			out.CostUSD, addressCostLimit)
	}
	return out, nil
}

// address sends one statement and reads back where the model would put it.
func (c *declareClient) address(ctx context.Context, prompt string) (string, declareUsage, error) {
	body, err := json.Marshal(map[string]any{
		"model": declareModel,
		"messages": []map[string]string{
			{"role": "system", "content": addressInstruction},
			{"role": "user", "content": prompt},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": addressMaxTokens,
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
		return "", declareUsage{}, fmt.Errorf("address: %w", err)
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
		return "", declareUsage{}, fmt.Errorf("address: %s: %s", resp.Status, decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", declareUsage{}, fmt.Errorf("address: the provider returned no choice")
	}
	return decoded.Choices[0].Message.Content,
		declareUsage{Prompt: decoded.Usage.PromptTokens, Completion: decoded.Usage.CompletionTokens}, nil
}

// parseAddress reads the model's JSON into an address. An answer that will
// not parse addresses nothing, which counts as a wrong address rather than
// being passed over.
func parseAddress(answer, subject string) Address {
	var decoded struct {
		Question    string `json:"question"`
		Restriction string `json:"restriction"`
	}
	if err := json.Unmarshal([]byte(answer), &decoded); err != nil {
		return Address{Subject: subject}
	}
	return Address{
		Subject:   subject,
		Attribute: strings.ToLower(strings.TrimSpace(decoded.Question)),
		Qualifier: strings.Trim(strings.ToLower(strings.TrimSpace(decoded.Restriction)), " .,"),
	}
}

// AddressScore is what a model's addressing got right, cut the ways that say
// whether the sentence or the class moves it.
type AddressScore struct {
	Model string `json:"model"`
	Asked int    `json:"asked"`
	// Right is the whole address, QuestionRight the question alone. The two
	// are reported apart because they are different failures: a wrong
	// question puts a fact where nothing belongs, and a wrong restriction
	// puts it at a question nothing asks.
	Right         int    `json:"address_right"`
	QuestionRight int    `json:"question_right"`
	Accuracy      Figure `json:"address_accuracy"`
	QuestionAcc   Figure `json:"question_accuracy"`
	// Restricted is the assertions the rule reads a restriction out of, and
	// RestrictionRight how many of them the model restricted in the same
	// words. RestrictionOverlap is the looser test: the two restrictions
	// contain one another, which is the same phrase given differently
	// rather than a different phrase, and the two are apart because only
	// one of them is a disagreement about what the sentence says.
	Restricted         int    `json:"assertions_the_rule_restricts"`
	RestrictionRight   int    `json:"restriction_right"`
	RestrictionAcc     Figure `json:"restriction_accuracy"`
	RestrictionOverlap Figure `json:"restriction_overlaps"`
	// Invented is a restriction where the rule reads none. It is the
	// damaging direction: a statement sent to a restricted address answers
	// a question nothing asks.
	Invented         int        `json:"restrictions_invented"`
	InventedPerAsked Figure     `json:"restrictions_invented_per_assertion"`
	Wordings         []CutScore `json:"wordings"`
	Classes          []CutScore `json:"classes"`
	// Inventions cuts the invented restrictions by the wording that wrote
	// the sentence, which is where they concentrate.
	Inventions       []CutScore `json:"inventions_by_wording"`
	CostUSD          float64    `json:"cost_usd"`
	Calls            int        `json:"calls"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
}

// ScoreAddresses reads a committed addressing against the rule's. It costs
// nothing.
func ScoreAddresses(a Addresses) AddressScore {
	out := AddressScore{Model: a.Model, Asked: len(a.Addressed), CostUSD: a.CostUSD, Calls: a.Calls,
		PromptTokens: a.PromptTokens, CompletionTokens: a.CompletionTokens}
	byWording, byClass, inventedBy := map[string][]float64{}, map[string][]float64{}, map[string][]float64{}
	var whole, question, restriction, overlap, invented []float64
	for _, one := range a.Addressed {
		whole = append(whole, boolAsFloat(one.Right()))
		question = append(question, boolAsFloat(one.QuestionRight()))
		if one.Right() {
			out.Right++
		}
		if one.QuestionRight() {
			out.QuestionRight++
		}
		if one.Formed.Qualifier != "" {
			out.Restricted++
			right := one.Answered.Qualifier == one.Formed.Qualifier
			restriction = append(restriction, boolAsFloat(right))
			overlap = append(overlap, boolAsFloat(sharePhrase(one.Answered.Qualifier, one.Formed.Qualifier)))
			if right {
				out.RestrictionRight++
			}
		}
		madeUp := one.Formed.Qualifier == "" && one.Answered.Qualifier != ""
		invented = append(invented, boolAsFloat(madeUp))
		if madeUp {
			out.Invented++
		}
		byWording[one.Wording] = append(byWording[one.Wording], boolAsFloat(one.Right()))
		byClass[one.Class] = append(byClass[one.Class], boolAsFloat(one.Right()))
		inventedBy[one.Wording] = append(inventedBy[one.Wording], boolAsFloat(madeUp))
	}
	out.Accuracy = NewFigure(whole)
	out.QuestionAcc = NewFigure(question)
	out.RestrictionAcc = NewFigure(restriction)
	out.RestrictionOverlap = NewFigure(overlap)
	out.InventedPerAsked = NewFigure(invented)
	out.Wordings = cutFrom(byWording, wordingNames)
	out.Classes = cutFrom(byClass, classNames)
	out.Inventions = cutFrom(inventedBy, wordingNames)
	return out
}

// sharePhrase reports whether two restrictions are the same phrase given
// differently. One containing the other is a model that read the phrase the
// rule read and wrote it without its preposition; neither containing the
// other is a model that read something else.
func sharePhrase(answered, formed string) bool {
	if answered == "" || formed == "" {
		return false
	}
	return strings.Contains(formed, answered) || strings.Contains(answered, formed)
}
