package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// What the paid half of a run is allowed to spend. The declaration is one
// call per fact-carrying turn and nothing else, so the count is known
// before anything is sent; a workload that would exceed these stops the run
// instead of spending, because a bound discovered afterwards is not a bound.
const (
	declareCallLimit = 1_000
	declareCostLimit = 0.25
)

// The model asked to declare, and what it cost. gpt-4.1-nano is the
// cheapest model here that answers in one word without reasoning tokens:
// the nano reasoning model is listed cheaper per input token and bills its
// reasoning as output, which for a one-word answer costs more and varies.
// Prices are per million tokens, read from the provider's pricing page on
// the date below, and they are inputs to the cost figure rather than claims
// that outlive it.
const (
	declareModel       = "gpt-4.1-nano"
	declarePriceIn     = 0.10
	declarePriceOut    = 0.40
	declarePriceRead   = "2026-08-16"
	declareBaseURL     = "https://api.openai.com/v1"
	declareKeyEnv      = "OPENAI_API_KEY"
	declareConcurrency = 8
	declareTimeout     = 60 * time.Second
	// declareMaxTokens bounds one answer. The answer is one word.
	declareMaxTokens = 4
	// declareRetries and declareBackoff bound the wait on a rate limit.
	declareRetries = 8
	declareBackoff = 2 * time.Second
)

// declareInstruction is what the model is asked. It states the job and the
// two answers and nothing else: the question under test is whether the
// sentence carries arity, so the prompt must not carry it instead.
const declareInstruction = `You are the write step of a memory system. Memory holds facts at an address, which is a subject and one of its attributes. A new statement about an address either REPLACES the values held there, because they have stopped being true, or JOINS them, because it adds a value that is true at the same time as the ones already held.

Answer with one word, either replace or join. Answer with nothing else.`

// Declaration is one operation a model declared for one assertion, beside
// the operation the workload planted. Committing these is what makes the
// scoring below reproducible without paying for the calls again.
type Declaration struct {
	Subject   string    `json:"subject"`
	Attribute string    `json:"attribute"`
	Value     string    `json:"value"`
	Class     string    `json:"class"`
	Wording   string    `json:"wording"`
	Year      int       `json:"year"`
	Held      []string  `json:"held"`
	Text      string    `json:"text"`
	Truth     Operation `json:"truth"`
	Declared  Operation `json:"declared"`
}

// Correct reports whether the model's answer matched the planted operation.
func (d Declaration) Correct() bool { return d.Declared == d.Truth }

// Declarations is the paid artifact: what the model answered, what it cost,
// and the price the cost was computed at.
type Declarations struct {
	Model            string        `json:"model"`
	Seed             int64         `json:"seed"`
	Calls            int           `json:"calls"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	PriceInPerM      float64       `json:"price_input_per_million_usd"`
	PriceOutPerM     float64       `json:"price_output_per_million_usd"`
	PriceRead        string        `json:"price_read_on"`
	CostUSD          float64       `json:"cost_usd"`
	Declarations     []Declaration `json:"declarations"`
}

// Asked is every assertion the model is asked about: one per fact-carrying
// turn, with what the address held before that write.
func Asked(w Workload) []Declaration {
	var out []Declaration
	for _, thread := range w.Threads {
		var held []string
		for _, a := range thread.Asserts {
			truth := OperationJoin
			if a.Replaces {
				truth = OperationReplace
			}
			out = append(out, Declaration{
				Subject: thread.Subject, Attribute: thread.Attribute, Value: a.Value,
				Class: thread.Class, Wording: a.Wording, Year: a.Year,
				Held: append([]string(nil), held...), Text: a.Text, Truth: truth,
			})
			if a.Replaces {
				held = nil
			}
			if !contains(held, a.Value) {
				held = append(held, a.Value)
			}
		}
	}
	return out
}

// Declare asks the model for every assertion and returns what it answered
// with what the calls cost. It refuses to start a run past the bound.
func Declare(ctx context.Context, w Workload, cachePath string) (Declarations, error) {
	asked := Asked(w)
	out := Declarations{
		Model: declareModel, Seed: w.Seed, Calls: len(asked),
		PriceInPerM: declarePriceIn, PriceOutPerM: declarePriceOut, PriceRead: declarePriceRead,
	}
	if len(asked) > declareCallLimit {
		return Declarations{}, fmt.Errorf(
			"the workload needs %d calls and the run is bounded at %d: raise the bound deliberately or measure a smaller workload",
			len(asked), declareCallLimit)
	}
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return Declarations{}, fmt.Errorf("declaring needs %s in the environment", declareKeyEnv)
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return Declarations{}, err
	}
	client := &declareClient{
		key:  key,
		http: &http.Client{Timeout: declareTimeout},
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
				prompt := declarePrompt(asked[i])
				mu.Lock()
				cached, ok := cache[prompt]
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				if ok {
					mu.Lock()
					asked[i].Declared = Operation(cached)
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
					asked[i].Declared = answer
					cache[prompt] = string(answer)
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
		return Declarations{}, err
	}
	if failure != nil {
		return Declarations{}, failure
	}
	out.Declarations = asked
	out.CostUSD = declareCost(out.PromptTokens, out.CompletionTokens)
	if out.CostUSD > declareCostLimit {
		return Declarations{}, fmt.Errorf("the run spent $%.4f against a bound of $%.2f", out.CostUSD, declareCostLimit)
	}
	return out, nil
}

// declareCost is the arithmetic behind the cost figure, in one place so the
// record and the result file cannot disagree about it.
func declareCost(prompt, completion int) float64 {
	return float64(prompt)/1e6*declarePriceIn + float64(completion)/1e6*declarePriceOut
}

// declarePrompt is what the model is shown for one assertion: the address,
// what is held there, and the sentence. It never names the operation the
// workload planted.
func declarePrompt(d Declaration) string {
	held := "nothing yet"
	if len(d.Held) > 0 {
		held = strings.Join(d.Held, ", ")
	}
	return fmt.Sprintf("Address: %s / %s\nHeld there now: %s\nNew statement: %s",
		d.Subject, d.Attribute, held, d.Text)
}

// declareClient posts one chat completion. It is a few lines rather than
// internal/agent/openai because that client is built for the reasoning
// loop's tool calls and does not surface the token usage this run has to
// report.
type declareClient struct {
	key  string
	http *http.Client
	// temperature is zero for the declaring pass, which wants one settled
	// answer, and one for the resampling pass, which is asking whether the
	// answer is settled at all.
	temperature int
}

type declareUsage struct {
	Prompt     int
	Completion int
}

// declare sends one prompt and reads one word back. The key goes in a
// header from the environment and reaches nothing else.
func (c *declareClient) declare(ctx context.Context, prompt string) (Operation, declareUsage, error) {
	body, err := json.Marshal(map[string]any{
		"model": declareModel,
		"messages": []map[string]string{
			{"role": "system", "content": declareInstruction},
			{"role": "user", "content": prompt},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": declareMaxTokens,
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
		return "", declareUsage{}, fmt.Errorf("declare: %w", err)
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
		return "", declareUsage{}, fmt.Errorf("declare: %s: %s", resp.Status, decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", declareUsage{}, fmt.Errorf("declare: the provider returned no choice")
	}
	usage := declareUsage{Prompt: decoded.Usage.PromptTokens, Completion: decoded.Usage.CompletionTokens}
	return parseOperation(decoded.Choices[0].Message.Content), usage, nil
}

// do sends the request, waiting and retrying a bounded number of times when
// the provider says the rate limit was reached. A run that stops on a limit
// it was always going to hit has spent money for nothing, and the retry is
// bounded so a provider that is down still fails rather than looping.
func (c *declareClient) do(ctx context.Context, req *http.Request, body []byte) (*http.Response, error) {
	var last error
	for attempt := range declareRetries {
		if attempt > 0 {
			req = req.Clone(ctx)
			req.Body = io.NopCloser(bytes.NewReader(body))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * declareBackoff):
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		resp.Body.Close()
		last = fmt.Errorf("rate limited after %d attempts", attempt+1)
	}
	return nil, last
}

// parseOperation reads the model's word. Anything that is not one of the
// two answers is kept as itself rather than mapped onto one of them, so an
// unusable answer is counted as wrong instead of being silently corrected.
func parseOperation(content string) Operation {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(content), ".")) {
	case "replace":
		return OperationReplace
	case "join":
		return OperationJoin
	default:
		return Operation("unusable")
	}
}

// readDeclarationCache reads answers already paid for, tolerating a missing
// file. The cache holds prompts and answers and no credential.
func readDeclarationCache(path string) (map[string]string, error) {
	cache := map[string]string{}
	if path == "" {
		return cache, nil
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &cache); err != nil {
		return nil, fmt.Errorf("decode cache %s: %w", path, err)
	}
	return cache, nil
}

func writeDeclarationCache(path string, cache map[string]string) error {
	if path == "" {
		return nil
	}
	body, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write cache %s: %w", path, err)
	}
	return nil
}

// DirectionScore is how often the model erred in one direction, over the
// writes that direction can touch. It is reported per direction and never
// blended, for the reason misdeclaration.go states: the two are different
// failures and one rate covering both would hide which the model has.
// Consequential counts the eligible writes landing on an address that
// already holds something. A write to an empty address is the same either
// way it is declared, so its declaration cannot be wrong — and most writes
// over a decade are of that kind. A rate over every write is therefore
// diluted by decisions that carry no consequence, and the two denominators
// are reported side by side rather than one standing for both.
type DirectionScore struct {
	Direction           string `json:"direction"`
	Eligible            int    `json:"eligible_writes"`
	Errors              int    `json:"errors"`
	Rate                Figure `json:"rate"`
	Consequential       int    `json:"consequential_writes"`
	ConsequentialErrors int    `json:"consequential_errors"`
	ConsequentialRate   Figure `json:"consequential_rate"`
}

// CutScore is the model's accuracy over one slice of the assertions.
type CutScore struct {
	Name     string `json:"name"`
	Asked    int    `json:"asked"`
	Accuracy Figure `json:"accuracy"`
}

// DeclarerScore is what the model answered, read the ways that decide
// whether the recommendation survives it: per direction, because the two
// failures compound differently, and per wording, because a model that errs
// only where the sentence marks an addition with a qualifier is saying
// something about where arity lives in language rather than about itself.
type DeclarerScore struct {
	Model            string           `json:"model"`
	Calls            int              `json:"calls"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	PriceInPerM      float64          `json:"price_input_per_million_usd"`
	PriceOutPerM     float64          `json:"price_output_per_million_usd"`
	PriceRead        string           `json:"price_read_on"`
	CostUSD          float64          `json:"cost_usd"`
	Asked            int              `json:"asked"`
	Consequential    int              `json:"consequential"`
	Unusable         int              `json:"unusable_answers"`
	Accuracy         Figure           `json:"accuracy"`
	ConsequentialAcc Figure           `json:"consequential_accuracy"`
	Directions       []DirectionScore `json:"directions"`
	Wordings         []CutScore       `json:"wordings"`
	Classes          []CutScore       `json:"classes"`
}

// ScoreDeclarations reads the committed answers against the workload's own
// ground truth. It costs nothing: the calls were paid for once and their
// answers are in the tree.
func ScoreDeclarations(d Declarations) DeclarerScore {
	out := DeclarerScore{
		Model: d.Model, Calls: d.Calls,
		PromptTokens: d.PromptTokens, CompletionTokens: d.CompletionTokens,
		PriceInPerM: d.PriceInPerM, PriceOutPerM: d.PriceOutPerM, PriceRead: d.PriceRead,
		CostUSD: d.CostUSD, Asked: len(d.Declarations),
	}
	var correct, consequential []float64
	for _, one := range d.Declarations {
		correct = append(correct, boolAsFloat(one.Correct()))
		if one.Declared != OperationJoin && one.Declared != OperationReplace {
			out.Unusable++
		}
		if len(one.Held) > 0 {
			out.Consequential++
			consequential = append(consequential, boolAsFloat(one.Correct()))
		}
	}
	out.Accuracy = NewFigure(correct)
	out.ConsequentialAcc = NewFigure(consequential)

	for _, direction := range directionNames {
		truth := OperationJoin
		if direction == directionReplaceAsJoin {
			truth = OperationReplace
		}
		score := DirectionScore{Direction: direction}
		var wrong, consequential []float64
		for _, one := range d.Declarations {
			if one.Truth != truth {
				continue
			}
			score.Eligible++
			if !one.Correct() {
				score.Errors++
			}
			wrong = append(wrong, boolAsFloat(!one.Correct()))
			if len(one.Held) == 0 {
				continue
			}
			score.Consequential++
			if !one.Correct() {
				score.ConsequentialErrors++
			}
			consequential = append(consequential, boolAsFloat(!one.Correct()))
		}
		score.Rate = NewFigure(wrong)
		score.ConsequentialRate = NewFigure(consequential)
		out.Directions = append(out.Directions, score)
	}
	out.Wordings = cutBy(d.Declarations, wordingNames, func(one Declaration) string { return one.Wording })
	out.Classes = cutBy(d.Declarations, classNames, func(one Declaration) string { return one.Class })
	return out
}

// cutBy scores the declarations sliced by one of their fields, in the order
// the names are given so a table reads the same on every run.
func cutBy(declarations []Declaration, names []string, of func(Declaration) string) []CutScore {
	out := make([]CutScore, 0, len(names))
	for _, name := range names {
		cut := CutScore{Name: name}
		var correct []float64
		for _, one := range declarations {
			if of(one) != name {
				continue
			}
			cut.Asked++
			correct = append(correct, boolAsFloat(one.Correct()))
		}
		if cut.Asked == 0 {
			continue
		}
		cut.Accuracy = NewFigure(correct)
		out = append(out, cut)
	}
	return out
}

// key identifies one assertion. A thread that returns to a value asserts it
// twice at the same address, so the year is part of what identifies it.
func (d Declaration) key() string {
	return fmt.Sprintf("%s|%s|%s|%d", d.Subject, d.Attribute, d.Value, d.Year)
}

// Declaring replaces what a write declares with what the model answered for
// that assertion. It is the strongest form of the question this round asks:
// not what a rate of errors costs, but what this model's errors cost, with
// whatever pattern they fall in.
func (d Declarations) Declaring() Declaring {
	answers := make(map[string]Operation, len(d.Declarations))
	for _, one := range d.Declarations {
		answers[one.key()] = one.Declared
	}
	return func(t Turn) Turn {
		written := make([]Written, len(t.Written))
		copy(written, t.Written)
		for i := range written {
			asserted := Declaration{
				Subject: written[i].Subject, Attribute: written[i].Attribute,
				Value: written[i].Value, Year: t.Day/daysPerYear + 1,
			}
			if answered, ok := answers[asserted.key()]; ok {
				written[i].Declared = answered
			}
		}
		t.Written = written
		return t
	}
}

// Flips are the assertions the model got wrong in one direction, in the
// shape the damage measurement reads.
func (d Declarations) Flips(direction string) []Flip {
	truth := OperationJoin
	if direction == directionReplaceAsJoin {
		truth = OperationReplace
	}
	var out []Flip
	for _, one := range d.Declarations {
		if one.Truth == truth && !one.Correct() {
			out = append(out, Flip{
				Address: Address{Subject: one.Subject, Attribute: one.Attribute},
				Year:    one.Year,
			})
		}
	}
	return out
}

// DirectionDamage is what the model's errors in one direction did.
type DirectionDamage struct {
	Direction string `json:"direction"`
	Damage    Damage `json:"damage"`
}

// ModelRun is the declared-operation frame driven by what the model
// actually answered over the whole decade. A rate says what an average
// error costs; this says what these errors cost, including that they fall
// on the classes the model finds hard rather than at random.
type ModelRun struct {
	Frame  string            `json:"frame"`
	Model  string            `json:"model"`
	Years  []YearResult      `json:"years"`
	Sizes  []SizeAt          `json:"sizes"`
	Damage []DirectionDamage `json:"damage"`
}

// MeasureModelRun runs a frame that reads declarations on the model's own
// answers.
func MeasureModelRun(ctx context.Context, w Workload, d Declarations, frame string) (ModelRun, error) {
	measured, err := Measure(ctx, w, frame, armOracle, NewEmbedCounter(), d.Declaring())
	if err != nil {
		return ModelRun{}, fmt.Errorf("%s on %s's declarations: %w", frame, d.Model, err)
	}
	out := ModelRun{Frame: frame, Model: d.Model, Years: measured.Years, Sizes: measured.Sizes}
	for _, direction := range directionNames {
		out.Damage = append(out.Damage, DirectionDamage{
			Direction: direction,
			Damage:    MeasureDamage(d.Flips(direction), measured.Probes, w.Params.Years),
		})
	}
	return out, nil
}
