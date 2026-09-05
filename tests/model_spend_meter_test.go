package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spendCeiling is what a paid run may spend before the meter in front of
// the provider refuses the next call. It is stated by the caller before
// the first request leaves the machine: a bound discovered from the
// invoice afterwards is not a bound.
//
// Four ceilings rather than one, because the two that need no price hold
// when the price is wrong. Calls and tokens are counted from what the
// provider reports; USD is derived from them at the rates below, which
// are what the run was charged at in the record and not a quote.
type spendCeiling struct {
	Calls               int
	InputTokens         int64
	OutputTokens        int64
	USD                 float64
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
}

// meteredCall is one chat-completions request the meter handled, kept so
// scoring reads what the run bought instead of buying it again. The
// prompts are not kept: every request carries the same instructions and
// the same tool schemas, which the tree already holds, so what is
// recorded is the model's side plus enough of the request to say which
// step made it.
type meteredCall struct {
	Seq              int               `json:"seq"`
	Label            string            `json:"label"`
	Step             string            `json:"step"`
	Model            string            `json:"model"`
	Tools            []string          `json:"tools"`
	LastMessage      string            `json:"last_message"`
	Status           int               `json:"status"`
	FinishReason     string            `json:"finish_reason"`
	Content          string            `json:"content"`
	ToolCalls        []meteredToolCall `json:"tool_calls,omitempty"`
	PromptTokens     int64             `json:"prompt_tokens"`
	CompletionTokens int64             `json:"completion_tokens"`
	DurationMS       int64             `json:"duration_ms"`
	Refused          string            `json:"refused,omitempty"`
}

// meteredToolCall is one call the model made, as it arrived on the wire.
type meteredToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// spendMeter is a metering proxy standing between the agent binary and
// the provider: it counts what every call costs, refuses the next one
// once a ceiling is reached, and keeps the model's answers.
//
// It is a proxy rather than a wrapper around the client because the
// agent under test is a separate process; the only seam between it and
// the provider is the base URL, which is configuration (ADR 0004).
type spendMeter struct {
	upstream string
	apiKey   string
	ceiling  spendCeiling
	server   *httptest.Server
	client   *http.Client

	mu       sync.Mutex
	label    string
	calls    []meteredCall
	inTokens int64
	outToken int64
}

// startSpendMeter starts the meter in front of upstream and returns it.
// The server is closed by test cleanup.
func startSpendMeter(t *testing.T, upstream, apiKey string, ceiling spendCeiling) *spendMeter {
	t.Helper()
	m := &spendMeter{
		upstream: strings.TrimSuffix(upstream, "/"),
		apiKey:   apiKey,
		ceiling:  ceiling,
		client:   &http.Client{Timeout: 10 * time.Minute},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	return m
}

// BaseURL is what the agent's model.base-url is pointed at.
func (m *spendMeter) BaseURL() string { return m.server.URL }

// Label names the turn the next calls belong to, so the transcript can
// be read per turn.
func (m *spendMeter) Label(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.label = label
}

// serve forwards one chat-completions request, or refuses it. The
// refusal comes before the request leaves the machine, which is the
// whole of what makes the ceiling a bound.
func (m *spendMeter) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if refusal := m.reserve(body); refusal != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": refusal})
		return
	}

	started := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		m.upstream+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	resp, err := m.client.Do(req)
	if err != nil {
		m.record(body, nil, 0, time.Since(started), err.Error())
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	m.record(body, answer, resp.StatusCode, time.Since(started), "")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(answer)
}

// reserve returns the ceiling this call would pass, or empty to forward
// it. A ceiling already reached refuses; one call's tokens are the
// granularity, because a request's cost is not known until it answers.
func (m *spendMeter) reserve(body []byte) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	spent := m.spentLocked()
	var reason string
	switch {
	case spent.Calls >= m.ceiling.Calls:
		reason = fmt.Sprintf("call ceiling reached: %d of %d calls", spent.Calls, m.ceiling.Calls)
	case spent.InputTokens >= m.ceiling.InputTokens:
		reason = fmt.Sprintf("input-token ceiling reached: %d of %d", spent.InputTokens, m.ceiling.InputTokens)
	case spent.OutputTokens >= m.ceiling.OutputTokens:
		reason = fmt.Sprintf("output-token ceiling reached: %d of %d", spent.OutputTokens, m.ceiling.OutputTokens)
	case spent.USD >= m.ceiling.USD:
		reason = fmt.Sprintf("USD ceiling reached: %.4f of %.4f", spent.USD, m.ceiling.USD)
	default:
		return ""
	}
	m.calls = append(m.calls, m.entryLocked(body, nil, 0, 0, reason))
	return reason
}

// record files one answered call and charges it to the run.
func (m *spendMeter) record(body, answer []byte, status int, took time.Duration, failure string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entryLocked(body, answer, status, took, failure)
	m.inTokens += entry.PromptTokens
	m.outToken += entry.CompletionTokens
	m.calls = append(m.calls, entry)
}

// entryLocked builds the transcript row for one call. The caller holds
// the lock.
func (m *spendMeter) entryLocked(body, answer []byte, status int, took time.Duration, failure string) meteredCall {
	entry := meteredCall{
		Seq:        len(m.calls) + 1,
		Label:      m.label,
		Status:     status,
		DurationMS: took.Milliseconds(),
		Refused:    failure,
	}
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err == nil {
		entry.Model = request.Model
		for _, t := range request.Tools {
			entry.Tools = append(entry.Tools, t.Function.Name)
		}
		if n := len(request.Messages); n > 0 {
			entry.LastMessage = request.Messages[n-1].Content
		}
	}
	// The read step advertises the reserved read tool and nothing else,
	// so which of a turn's calls it is reads off the advertised set
	// rather than off a counter the runner keeps (ADR 0019).
	entry.Step = "act"
	if len(entry.Tools) == 1 && entry.Tools[0] == "memory_read" {
		entry.Step = "read"
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(answer, &decoded); err == nil {
		entry.PromptTokens = decoded.Usage.PromptTokens
		entry.CompletionTokens = decoded.Usage.CompletionTokens
		if len(decoded.Choices) > 0 {
			choice := decoded.Choices[0]
			entry.FinishReason = choice.FinishReason
			entry.Content = choice.Message.Content
			for _, call := range choice.Message.ToolCalls {
				entry.ToolCalls = append(entry.ToolCalls, meteredToolCall{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				})
			}
		}
	}
	return entry
}

// spend is what the run has cost so far.
type spend struct {
	Calls        int     `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	USD          float64 `json:"usd"`
}

// Spend reports the run's cost to date.
func (m *spendMeter) Spend() spend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spentLocked()
}

func (m *spendMeter) spentLocked() spend {
	answered := 0
	for _, c := range m.calls {
		if c.Refused == "" {
			answered++
		}
	}
	usd := float64(m.inTokens)/1e6*m.ceiling.InputUSDPerMillion +
		float64(m.outToken)/1e6*m.ceiling.OutputUSDPerMillion
	return spend{Calls: answered, InputTokens: m.inTokens, OutputTokens: m.outToken, USD: usd}
}

// Refusals reports the calls the meter refused, which is how a run that
// hit a ceiling is told from one that finished.
func (m *spendMeter) Refusals() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, c := range m.calls {
		if c.Refused != "" {
			out = append(out, c.Refused)
		}
	}
	return out
}

// ReadAddress returns the arguments the read step's reserved call
// carried for a labelled turn, or empty where the step made no call —
// which is the unplaced outcome as the provider produced it (ADR 0019).
func (m *spendMeter) ReadAddress(label string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.Label != label || c.Step != "read" {
			continue
		}
		for _, call := range c.ToolCalls {
			if call.Name == "memory_read" {
				return call.Arguments
			}
		}
	}
	return ""
}

// WriteTranscript commits the calls the run bought.
func (m *spendMeter) WriteTranscript(t *testing.T, path string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	body, err := json.MarshalIndent(struct {
		Ceiling spendCeiling  `json:"ceiling"`
		Spend   spend         `json:"spend"`
		Calls   []meteredCall `json:"calls"`
	}{m.ceiling, m.spentLocked(), m.calls}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o600))
}

// TestSpendMeter_RefusesPastEachCeiling is the evidence that the bound
// is a bound: the meter refuses the call that would pass a ceiling
// before it leaves the machine, rather than reporting the overspend
// afterwards. Each case is paired with the control that isolates the
// ceiling being tested — the identical calls under a ceiling wide enough
// to admit them, which is what fails if something other than the ceiling
// is doing the refusing.
func TestSpendMeter_RefusesPastEachCeiling(t *testing.T) {
	upstream := startScriptedEndpoint(t, scriptedAnswer{
		Subject: "Sarah", Question: "where does Sarah live", Value: "Lisbon", Reply: "Lisbon.",
	})
	// The scripted endpoint reports 100 prompt and 20 completion tokens
	// per call, so two calls cost 200, 40, and $0.000108.
	wide := spendCeiling{
		Calls: 100, InputTokens: 100_000, OutputTokens: 100_000, USD: 100,
		InputUSDPerMillion: recallInputUSDPerMillion, OutputUSDPerMillion: recallOutputUSDPerMillion,
	}
	narrow := map[string]func(spendCeiling) spendCeiling{
		"calls":         func(c spendCeiling) spendCeiling { c.Calls = 2; return c },
		"input tokens":  func(c spendCeiling) spendCeiling { c.InputTokens = 200; return c },
		"output tokens": func(c spendCeiling) spendCeiling { c.OutputTokens = 40; return c },
		"USD":           func(c spendCeiling) spendCeiling { c.USD = 0.0001; return c },
	}
	for name, narrowed := range narrow {
		t.Run(name, func(t *testing.T) {
			refused := postThreeMeteredCalls(t, upstream, narrowed(wide))
			assert.Equal(t, []int{200, 200, http.StatusPaymentRequired}, refused,
				"the third call passes the %s ceiling and is refused before it is sent", name)

			admitted := postThreeMeteredCalls(t, upstream, wide)
			assert.Equal(t, []int{200, 200, 200}, admitted,
				"the same three calls are forwarded under a ceiling that admits them")
		})
	}
}

// postThreeMeteredCalls sends three identical chat-completions requests
// through a meter on ceiling and returns the status of each.
func postThreeMeteredCalls(t *testing.T, upstream string, ceiling spendCeiling) []int {
	t.Helper()
	meter := startSpendMeter(t, upstream, "scripted", ceiling)
	var got []int
	for range 3 {
		resp := postJSON(t, meter.BaseURL()+"/chat/completions", `{"model":"scripted","messages":[]}`)
		got = append(got, resp.StatusCode)
		resp.Body.Close()
	}
	return got
}
