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
	"unicode"

	"github.com/garamsh/gagent/internal/agent"
)

// The prose round measures the write the platform ships rather than a
// prompt written for this record. Every other address figure here came
// from `addressing.go`, whose instruction names the twelve questions and
// asks for one of them; `internal/agent/instructions.go` §Memory writeback
// names none of them, is written for a person talking to an agent, and is
// what actually runs. Nothing had measured what it produces (#547).
//
// What is measured is not whether an address is well formed. A well formed
// address no question will reach is the failure this round exists to
// catch, so the score separates four things that a single accuracy number
// would merge:
//
//	parsed              the shipped parser turned the response into a write
//	subject right       the subject is the one the workload asserted about
//	self-answering      the value appears inside the address
//	reachable           the paired probe resolves onto the same address
//
// The first three are decided against the workload's own ground truth and
// need no second model call, so the finding does not rest on the read
// prompt this round has to supply for the fourth.
const (
	proseCallLimit = 2_000
	proseCostLimit = 0.60
	// proseMaxTokens bounds one answer: the shipped schema's document with
	// a reply and the turn's writes in it.
	proseMaxTokens = 400
	// proseReadMaxTokens bounds one read answer, which is two short strings.
	proseReadMaxTokens = 60
	// proseModel is what internal/config resolves to with nothing
	// configured, so this round asks the shipped prompt under the shipped
	// model. It is not declareModel: the rest of the paid halves buy the
	// cheapest model at this provider, and a measurement of what ships has
	// to use what ships. It is also the model the observation in #547 came
	// from, so a refutation refutes that observation rather than a cheaper
	// stand-in for it.
	proseModel     = "gpt-4o-mini"
	prosePriceIn   = 0.15
	prosePriceOut  = 0.60
	prosePriceRead = "2026-08-17"

	// proseMeasuredPrompt and proseMeasuredSchema are the SHA-256 of
	// agent.ComposeInstructions("") and agent.ResponseSchema as the
	// committed round measured them. #546 held this record's figures
	// against the constants they were measured under; the constant this
	// round is measured under is the prompt itself, so it is pinned the
	// same way. A change to instructions.go fails the test rather than
	// leaving a record that describes a prompt no longer shipped — and
	// that is the whole subject here, so the failure is the point rather
	// than an inconvenience. The remedy is a decision: re-run and commit,
	// or say in §Limits that these figures are historical.
	proseMeasuredPrompt = "6be53ea7ac20e7897020ddf8158e5ccd3018d3b7c1954b7506c3f913496574cc"
	proseMeasuredSchema = "0944fae4c3f16b2db3551289c7872aad257e23142491f1df141b84e91ce5b69f"
)

// proseReadInstruction is the read step, and this round has to supply it:
// the platform ships a write prompt and no read prompt, because #336 step
// 3 is where one lands. It is the shipped prose's own two address
// sentences with "statement" read as "question", and nothing else — no
// example, no subject list, no attribute list. That matters: a read prompt
// carrying more than the write prompt could place a question the write
// could not, and the number would then describe the read prompt.
//
// The read call is given the probe and nothing else. It never sees the
// assertion, the value or the subject, which is the whole control: an
// address only the answer can form is one this call cannot reach.
const proseReadInstruction = `A memory holds a fact at the address of the question it answers. An address is:

- subject — who or what the fact is about.
- question — the question the fact answers, in the question's own words. Not a topic noun: "what is the user's favorite telescope" is an address, "telescopes" is not.

Read the question below and answer with the address it asks about.

Answer with JSON only, in this shape: {"subject":"...","question":"..."}`

// ProseWrite is one assertion put through the shipped contract prose,
// beside the ground truth the workload planted for it. Committing these is
// what lets the scoring below run without buying the calls again.
type ProseWrite struct {
	// The workload's ground truth for this assertion.
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
	Class     string `json:"class"`
	Wording   string `json:"wording"`
	Text      string `json:"text"`
	// Probe is the question the workload asks this fact with. The pairing
	// is the workload's own: one attribute carries both templates.
	Probe string `json:"probe"`

	// What the shipped prompt and the shipped parser produced.
	Parsed      bool   `json:"parsed"`
	Ops         int    `json:"ops"`
	GotSubject  string `json:"got_subject"`
	GotQuestion string `json:"got_question"`
	GotValue    string `json:"got_value"`
	// Reply is kept for the unparsed rows only, so a reader can see what
	// the model said instead of a write.
	Reply string `json:"reply,omitempty"`
}

// ProseRead is one probe put through the read instruction above.
type ProseRead struct {
	Probe   string `json:"probe"`
	Subject string `json:"subject"`

	GotSubject  string `json:"got_subject"`
	GotQuestion string `json:"got_question"`
}

// ProseRun is the committed artifact for the round: what it cost, and
// every write and read it bought.
type ProseRun struct {
	Model                   string       `json:"model"`
	Seed                    int64        `json:"seed"`
	Calls                   int          `json:"calls"`
	PromptTokens            int          `json:"prompt_tokens"`
	CompletionTokens        int          `json:"completion_tokens"`
	PriceInputPerMillionUSD float64      `json:"price_input_per_million_usd"`
	PriceOutputPerMillion   float64      `json:"price_output_per_million_usd"`
	PriceReadOn             string       `json:"price_read_on"`
	CostUSD                 float64      `json:"cost_usd"`
	Writes                  []ProseWrite `json:"writes"`
	Reads                   []ProseRead  `json:"reads"`
}

// proseCost is the arithmetic behind this round's cost figure. It is not
// declareCost because the model and therefore the prices differ.
func proseCost(prompt, completion int) float64 {
	return float64(prompt)/1e6*prosePriceIn + float64(completion)/1e6*prosePriceOut
}

// AskedProse returns one row per assertion in the decade workload, filled
// with ground truth and waiting for the model's answer. Every assertion is
// asked: the round is a census rather than a sample, so a class or a
// wording cannot be under-represented by chance.
func AskedProse(w Workload) []ProseWrite {
	var out []ProseWrite
	for _, t := range w.Threads {
		for _, a := range t.Asserts {
			out = append(out, ProseWrite{
				Subject:   t.Subject,
				Attribute: t.Attribute,
				Value:     a.Value,
				Class:     t.Class,
				Wording:   a.Wording,
				Text:      a.Text,
				Probe:     t.Question,
			})
		}
	}
	return out
}

// AskedProseReads returns one row per distinct probe. The probes repeat
// across subjects, so this is the smaller half of the spend.
func AskedProseReads(w Workload) []ProseRead {
	seen := map[string]bool{}
	var out []ProseRead
	for _, t := range w.Threads {
		if seen[t.Question] {
			continue
		}
		seen[t.Question] = true
		out = append(out, ProseRead{Probe: t.Question, Subject: t.Subject})
	}
	return out
}

// proseNormalize reduces a string to the words it is written in: lower
// case, letters and digits only, in order. Two addresses that differ
// anywhere else are different addresses, which is what a store keyed by an
// address does, and this round does not soften it — a looser rule would be
// a constant nobody measured.
func proseNormalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case !space:
			b.WriteRune(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// SubjectRight reports whether the write put the workload's subject in the
// subject slot. This is the #547 observation's own test: the sample there
// put the cat's name in `subject` and the same string in `value`, so the
// subject was the answer rather than what the fact is about.
func (p ProseWrite) SubjectRight() bool {
	return p.Parsed && proseNormalize(p.GotSubject) == proseNormalize(p.Subject)
}

// SubjectHoldsValue reports whether the value ended up inside the subject
// slot, and QuestionHoldsValue the same for the question slot. An address
// holding its own value can only be formed by something that already has
// the answer, so nothing asking the question will reach it.
func (p ProseWrite) SubjectHoldsValue() bool {
	return p.Parsed && Mentions(p.GotSubject, p.Value)
}

// QuestionHoldsValue reports whether the value leaked into the question.
func (p ProseWrite) QuestionHoldsValue() bool {
	return p.Parsed && Mentions(p.GotQuestion, p.Value)
}

// SelfAnswering reports whether the value reached either address slot.
func (p ProseWrite) SelfAnswering() bool {
	return p.SubjectHoldsValue() || p.QuestionHoldsValue()
}

// proseKey is the address as a store would hold it.
func proseKey(subject, question string) string {
	return proseNormalize(subject) + "\x00" + proseNormalize(question)
}

// ProseScore is what the round reports. Every rate is a Figure so the
// record cannot show a number the workload only appeared to produce.
type ProseScore struct {
	Model            string  `json:"model"`
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	PriceInPerM      float64 `json:"price_input_per_million_usd"`
	PriceOutPerM     float64 `json:"price_output_per_million_usd"`
	PriceRead        string  `json:"price_read_on"`
	CostUSD          float64 `json:"cost_usd"`

	Asked int `json:"asked"`
	Reads int `json:"reads"`

	// Parsed is the number a reader must not mistake for the result: it
	// says the response was the shape the schema demands, and says nothing
	// about whether a question will reach what it wrote.
	Parsed Figure `json:"parsed"`

	SubjectRight       Figure `json:"subject_right"`
	SubjectHoldsValue  Figure `json:"subject_holds_value"`
	QuestionHoldsValue Figure `json:"question_holds_value"`
	SelfAnswering      Figure `json:"self_answering"`

	// Reachable is the result: the paired probe, resolved by a call that
	// saw only the probe, landed on the address the write produced.
	Reachable     Figure `json:"reachable"`
	SubjectMatch  Figure `json:"subject_match"`
	QuestionMatch Figure `json:"question_match"`

	Wordings []CutScore `json:"wordings"`
	Classes  []CutScore `json:"classes"`

	// Examples are verbatim rows, the failures first. #547 asks for what
	// the failure looks like, and a rate cannot show that.
	Examples []ProseExample `json:"examples"`
}

// ProseExample is one row shown in the record verbatim.
type ProseExample struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Subject  string `json:"subject"`
	Question string `json:"question"`
	Value    string `json:"value"`
	Probe    string `json:"probe"`
	ReadAs   string `json:"read_as"`
}

// ScoreProse reads a committed round. It costs nothing.
func ScoreProse(run ProseRun) ProseScore {
	reads := map[string]ProseRead{}
	for _, r := range run.Reads {
		reads[r.Probe] = r
	}

	score := ProseScore{
		Model: run.Model, Calls: run.Calls,
		PromptTokens: run.PromptTokens, CompletionTokens: run.CompletionTokens,
		PriceInPerM: run.PriceInputPerMillionUSD, PriceOutPerM: run.PriceOutputPerMillion,
		PriceRead: run.PriceReadOn, CostUSD: run.CostUSD,
		Asked: len(run.Writes), Reads: len(run.Reads),
	}
	var parsed, right, subjectVal, questionVal, selfAns []float64
	var reachable, subjectMatch, questionMatch []float64
	wordings := map[string][]float64{}
	classes := map[string][]float64{}

	for _, p := range run.Writes {
		parsed = append(parsed, boolFloat(p.Parsed))
		right = append(right, boolFloat(p.SubjectRight()))
		subjectVal = append(subjectVal, boolFloat(p.SubjectHoldsValue()))
		questionVal = append(questionVal, boolFloat(p.QuestionHoldsValue()))
		selfAns = append(selfAns, boolFloat(p.SelfAnswering()))

		read, ok := reads[p.Probe]
		hitSubject := ok && p.Parsed && proseNormalize(p.GotSubject) == proseNormalize(read.GotSubject)
		hitQuestion := ok && p.Parsed && proseNormalize(p.GotQuestion) == proseNormalize(read.GotQuestion)
		hit := ok && p.Parsed &&
			proseKey(p.GotSubject, p.GotQuestion) == proseKey(read.GotSubject, read.GotQuestion)

		subjectMatch = append(subjectMatch, boolFloat(hitSubject))
		questionMatch = append(questionMatch, boolFloat(hitQuestion))
		reachable = append(reachable, boolFloat(hit))
		wordings[p.Wording] = append(wordings[p.Wording], boolFloat(hit))
		classes[p.Class] = append(classes[p.Class], boolFloat(hit))
	}

	score.Parsed = NewFigure(parsed)
	score.SubjectRight = NewFigure(right)
	score.SubjectHoldsValue = NewFigure(subjectVal)
	score.QuestionHoldsValue = NewFigure(questionVal)
	score.SelfAnswering = NewFigure(selfAns)
	score.Reachable = NewFigure(reachable)
	score.SubjectMatch = NewFigure(subjectMatch)
	score.QuestionMatch = NewFigure(questionMatch)
	score.Wordings = cutFrom(wordings, wordingNames)
	score.Classes = cutFrom(classes, classNames)
	score.Examples = proseExamples(run, reads)
	return score
}

// boolFloat is the 1/0 a rate is averaged from.
func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// proseExampleLimit bounds how many rows of each kind the record shows.
const proseExampleLimit = 3

// proseExamples picks rows to show verbatim: the failure kinds first, then
// a reachable row so a reader can see what the working shape looks like.
// Selection is first-in-workload-order, so it is reproducible and not a
// choice of the most striking row.
func proseExamples(run ProseRun, reads map[string]ProseRead) []ProseExample {
	kinds := []struct {
		name string
		pick func(ProseWrite, ProseRead, bool) bool
	}{
		{"subject holds the value", func(p ProseWrite, _ ProseRead, _ bool) bool { return p.SubjectHoldsValue() }},
		{"question holds the value", func(p ProseWrite, _ ProseRead, _ bool) bool { return p.QuestionHoldsValue() }},
		{"wrong subject", func(p ProseWrite, _ ProseRead, _ bool) bool { return p.Parsed && !p.SubjectRight() }},
		{"unreachable", func(p ProseWrite, r ProseRead, ok bool) bool {
			return p.Parsed && ok && proseKey(p.GotSubject, p.GotQuestion) != proseKey(r.GotSubject, r.GotQuestion)
		}},
		{"no write", func(p ProseWrite, _ ProseRead, _ bool) bool { return !p.Parsed }},
		{"reachable", func(p ProseWrite, r ProseRead, ok bool) bool {
			return p.Parsed && ok && proseKey(p.GotSubject, p.GotQuestion) == proseKey(r.GotSubject, r.GotQuestion)
		}},
	}
	var out []ProseExample
	for _, k := range kinds {
		shown := 0
		for _, p := range run.Writes {
			if shown >= proseExampleLimit {
				break
			}
			r, ok := reads[p.Probe]
			if !k.pick(p, r, ok) {
				continue
			}
			out = append(out, ProseExample{
				Kind:     k.name,
				Text:     p.Text,
				Subject:  p.GotSubject,
				Question: p.GotQuestion,
				Value:    p.GotValue,
				Probe:    p.Probe,
				ReadAs:   proseReadAs(r, ok),
			})
			shown++
		}
	}
	return out
}

// proseReadAs renders the address the read call placed a probe on.
func proseReadAs(r ProseRead, ok bool) string {
	if !ok {
		return ""
	}
	return r.GotSubject + " / " + r.GotQuestion
}

// Prose buys the round: one call per assertion through the shipped prompt
// and the shipped schema, then one call per distinct probe through the
// read instruction above.
func Prose(w Workload, cachePath string) (ProseRun, error) {
	key := os.Getenv(declareKeyEnv)
	if key == "" {
		return ProseRun{}, fmt.Errorf("prose round: %s is unset; it reaches the process through the environment and nothing else", declareKeyEnv)
	}
	writes := AskedProse(w)
	reads := AskedProseReads(w)
	if total := len(writes) + len(reads); total > proseCallLimit {
		return ProseRun{}, fmt.Errorf("prose round: %d calls exceeds the bound of %d", total, proseCallLimit)
	}

	client := &declareClient{key: key, http: &http.Client{Timeout: declareTimeout}}
	run := ProseRun{
		Model:                   proseModel,
		Seed:                    w.Seed,
		PriceInputPerMillionUSD: prosePriceIn,
		PriceOutputPerMillion:   prosePriceOut,
		PriceReadOn:             prosePriceRead,
	}
	cache, err := readDeclarationCache(cachePath)
	if err != nil {
		return ProseRun{}, err
	}
	var mu sync.Mutex
	var failure error

	instructions := agent.ComposeInstructions("")
	run.Calls = len(writes) + len(reads)

	work := make(chan int)
	var wg sync.WaitGroup
	for range declareConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				mu.Lock()
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				prompt := "Assertion: " + writes[i].Text
				if cached, ok := cache[prompt]; ok {
					mu.Lock()
					fillProseWrite(&writes[i], cached)
					run.Calls--
					mu.Unlock()
					continue
				}
				body, in, out, err := client.prose(context.Background(), instructions, writes[i].Text)
				mu.Lock()
				if err != nil {
					if failure == nil {
						failure = err
					}
				} else {
					fillProseWrite(&writes[i], body)
					cache[prompt] = body
					run.PromptTokens += in
					run.CompletionTokens += out
				}
				mu.Unlock()
			}
		}()
	}
	for i := range writes {
		work <- i
	}
	close(work)
	wg.Wait()
	if failure != nil {
		if err := writeDeclarationCache(cachePath, cache); err != nil {
			return ProseRun{}, err
		}
		return ProseRun{}, failure
	}

	readWork := make(chan int)
	var rg sync.WaitGroup
	for range declareConcurrency {
		rg.Add(1)
		go func() {
			defer rg.Done()
			for i := range readWork {
				mu.Lock()
				stop := failure != nil
				mu.Unlock()
				if stop {
					continue
				}
				prompt := "Probe: " + reads[i].Probe
				if cached, ok := cache[prompt]; ok {
					mu.Lock()
					fillProseRead(&reads[i], cached)
					run.Calls--
					mu.Unlock()
					continue
				}
				body, in, out, err := client.proseRead(context.Background(), reads[i].Probe)
				mu.Lock()
				if err != nil {
					if failure == nil {
						failure = err
					}
				} else {
					fillProseRead(&reads[i], body)
					cache[prompt] = body
					run.PromptTokens += in
					run.CompletionTokens += out
				}
				mu.Unlock()
			}
		}()
	}
	for i := range reads {
		readWork <- i
	}
	close(readWork)
	rg.Wait()
	if err := writeDeclarationCache(cachePath, cache); err != nil {
		return ProseRun{}, err
	}
	if failure != nil {
		return ProseRun{}, failure
	}

	run.CostUSD = proseCost(run.PromptTokens, run.CompletionTokens)
	if run.CostUSD > proseCostLimit {
		return ProseRun{}, fmt.Errorf("prose round: $%.4f exceeds the bound of $%.2f", run.CostUSD, proseCostLimit)
	}
	run.Writes = writes
	run.Reads = reads
	return run, nil
}

// fillProseWrite reads one response through the shipped parser, so
// "parsed" here means what it means in the running agent. A response the
// parser will not take is recorded as a row with no write rather than
// dropped, because a prompt that produces none is the finding.
func fillProseWrite(p *ProseWrite, body string) {
	reply, ops, _, err := agent.ParseWriteback(body)
	if err != nil || len(ops) == 0 {
		p.Reply = body
		return
	}
	op := ops[0]
	p.Ops = len(ops)
	if op.Address.Subject == "" || op.Address.Question == "" || op.Value == "" {
		p.Reply = body
		return
	}
	p.Parsed = true
	p.GotSubject = op.Address.Subject
	p.GotQuestion = op.Address.Question
	p.GotValue = op.Value
	_ = reply
}

// fillProseRead reads one read answer. An unparseable answer leaves the
// address empty, which counts as a probe the read step could not place
// rather than one it placed wrongly.
func fillProseRead(r *ProseRead, body string) {
	var decoded struct {
		Subject  string `json:"subject"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return
	}
	r.GotSubject = strings.TrimSpace(decoded.Subject)
	r.GotQuestion = strings.TrimSpace(decoded.Question)
}

// prose asks one assertion through the shipped instructions and the
// shipped response schema. The key goes in a header from the environment
// and reaches nothing else.
func (c *declareClient) prose(ctx context.Context, instructions, text string) (string, int, int, error) {
	body, err := json.Marshal(map[string]any{
		"model": proseModel,
		"messages": []map[string]string{
			{"role": "system", "content": instructions},
			{"role": "user", "content": text},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": proseMaxTokens,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "agent_response",
				"strict": true,
				"schema": json.RawMessage(agent.ResponseSchema),
			},
		},
	})
	if err != nil {
		return "", 0, 0, err
	}
	return c.askProse(ctx, body)
}

// proseRead asks one probe through the read instruction.
func (c *declareClient) proseRead(ctx context.Context, probe string) (string, int, int, error) {
	body, err := json.Marshal(map[string]any{
		"model": proseModel,
		"messages": []map[string]string{
			{"role": "system", "content": proseReadInstruction},
			{"role": "user", "content": "Question: " + probe},
		},
		"temperature":           c.temperature,
		"max_completion_tokens": proseReadMaxTokens,
		"response_format":       map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", 0, 0, err
	}
	return c.askProse(ctx, body)
}

// askProse posts one prepared body and returns the content with the tokens
// it cost.
func (c *declareClient) askProse(ctx context.Context, body []byte) (string, int, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, declareBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.do(ctx, req, body)
	if err != nil {
		return "", 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

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
		return "", 0, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, 0, fmt.Errorf("prose round: %s: %s", resp.Status, decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("prose round: no choices")
	}
	return decoded.Choices[0].Message.Content, decoded.Usage.PromptTokens, decoded.Usage.CompletionTokens, nil
}
