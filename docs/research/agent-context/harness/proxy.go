package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Prices are MiniMax-M2's standard rates, the ones the earlier studies used.
const (
	inputUSDPerToken  = 0.30 / 1e6
	outputUSDPerToken = 1.20 / 1e6
	// bytesPerToken under-counts tokens per byte, so a reservation covers
	// the input it stands for.
	bytesPerToken = 3
)

// maxTokens is the output bound the proxy adds to each request; production
// sends none. A reservation charges the whole bound.
var maxTokens = map[string]int{"synthesis": 2048, "understand": 2048, "act": 4096}

// call is one attempt as the ledger records it. Request bodies are not kept:
// the runner records what each call was shown.
type call struct {
	Time             time.Time `json:"time"`
	Arm              string    `json:"arm"`
	Episode          string    `json:"episode"`
	Phase            string    `json:"phase"`
	Kind             string    `json:"kind"`
	Attempt          int       `json:"attempt"`
	Status           int       `json:"status"`
	Error            string    `json:"error,omitempty"`
	Cached           bool      `json:"cached,omitempty"`
	LatencyMS        int64     `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ReservedUSD      float64   `json:"reserved_usd"`
	USD              float64   `json:"usd"`
	RequestSHA256    string    `json:"request_sha256"`
	ResponseModel    string    `json:"response_model,omitempty"`
	ResponseID       string    `json:"response_id,omitempty"`
	FinishReason     string    `json:"finish_reason,omitempty"`
}

// ledger reserves each request's worst case against the ceiling before it
// is sent and settles it at its reported usage.
type ledger struct {
	mu       sync.Mutex
	ceiling  float64
	spent    float64
	reserved float64
}

var errBudget = errors.New("budget ceiling reached")

func (l *ledger) reserve(usd float64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.spent+l.reserved+usd > l.ceiling {
		return fmt.Errorf("%w: spent %.4f, reserved %.4f, request %.4f, ceiling %.2f", errBudget, l.spent, l.reserved, usd, l.ceiling)
	}
	l.reserved += usd
	return nil
}

func (l *ledger) settle(reserved, actual float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reserved -= reserved
	l.spent += actual
}

func (l *ledger) state() (spent, reserved float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent, l.reserved
}

// reservation is a request's worst-case cost: every input byte counted as
// a third of a token and the whole output bound.
func reservation(body []byte, outputBound int) float64 {
	return float64(len(body))/bytesPerToken*inputUSDPerToken + float64(outputBound)*outputUSDPerToken
}

func cost(prompt, completion int) float64 {
	return float64(prompt)*inputUSDPerToken + float64(completion)*outputUSDPerToken
}

// requestKind names what a chat request is for by the one reserved tool it
// offers; anything else is an act call.
func requestKind(body []byte) string {
	var doc struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &doc) == nil && len(doc.Tools) == 1 {
		switch doc.Tools[0].Function.Name {
		case "memory_summary":
			return "synthesis"
		case "memory_understand":
			return "understand"
		}
	}
	return "act"
}

// withMaxTokens adds the output bound to a request that has none.
func withMaxTokens(body []byte, bound int) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if _, ok := doc["max_tokens"]; !ok {
		doc["max_tokens"] = json.RawMessage(fmt.Sprint(bound))
	}
	return json.Marshal(doc)
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// proxy forwards the arms' chat requests to the provider under the ledger.
// A synthesis request identical to one already answered is answered from
// the cache: the same batch seeded at the same place in every arm and
// episode is synthesized once.
type proxy struct {
	upstream string
	key      string
	ledger   *ledger
	client   *http.Client

	logMu sync.Mutex
	log   io.Writer

	cacheMu  sync.Mutex
	cache    map[string][]byte
	inflight map[string]chan struct{}
	cacheLog io.Writer
}

type cacheEntry struct {
	Key      string          `json:"key"`
	Response json.RawMessage `json:"response"`
}

func loadCache(path string) (map[string][]byte, error) {
	out := map[string][]byte{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var e cacheEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, err
		}
		out[e.Key] = e.Response
	}
	return out, scanner.Err()
}

// loadSpent sums what an earlier run of the ledger settled, so a restarted
// proxy keeps counting against the same ceiling.
func loadSpent(path string) (float64, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var total float64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var c call
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			return 0, err
		}
		total += c.USD
	}
	return total, scanner.Err()
}

// route reads /<arm>/<episode>/<phase>/v1/chat/completions.
func route(path string) (arm, episode, phase string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 || parts[3] != "v1" || parts[4] != "chat" || parts[5] != "completions" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/budget" {
		spent, reserved := p.ledger.state()
		_ = json.NewEncoder(w).Encode(map[string]float64{"spent": spent, "reserved": reserved, "ceiling": p.ledger.ceiling})
		return
	}
	arm, episode, phase, ok := route(r.URL.Path)
	if !ok || r.Method != http.MethodPost {
		http.Error(w, "unknown route", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kind := requestKind(body)
	base := call{Arm: arm, Episode: episode, Phase: phase, Kind: kind, RequestSHA256: sha(body)}
	if kind == "synthesis" {
		if cached, done := p.cached(base.RequestSHA256); cached != nil {
			base.Time, base.Cached, base.Status = time.Now().UTC(), true, http.StatusOK
			p.record(base)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cached)
			return
		} else if done != nil {
			defer done()
		}
	}
	out, err := withMaxTokens(body, maxTokens[kind])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status, resp := p.forward(r.Context(), base, out)
	if kind == "synthesis" && status == http.StatusOK {
		p.store(base.RequestSHA256, resp)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// cached returns a stored synthesis, or, when none is stored, a function
// the caller runs once it has stored one; a second identical request waits
// for the first instead of paying for it again.
func (p *proxy) cached(key string) ([]byte, func()) {
	for {
		p.cacheMu.Lock()
		if resp, ok := p.cache[key]; ok {
			p.cacheMu.Unlock()
			return resp, nil
		}
		wait, busy := p.inflight[key]
		if !busy {
			ch := make(chan struct{})
			p.inflight[key] = ch
			p.cacheMu.Unlock()
			return nil, func() {
				p.cacheMu.Lock()
				delete(p.inflight, key)
				p.cacheMu.Unlock()
				close(ch)
			}
		}
		p.cacheMu.Unlock()
		<-wait
	}
}

func (p *proxy) store(key string, resp []byte) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache[key] = resp
	line, _ := json.Marshal(cacheEntry{Key: key, Response: resp})
	_, _ = p.cacheLog.Write(append(line, '\n'))
}

// retryable is a status worth one more attempt: rate limiting and server
// faults, never a refusal of the request itself.
func retryable(status int) bool { return status == http.StatusTooManyRequests || status >= 500 }

// forward sends one request, retrying twice on a transport error, rate
// limit or server fault. Every attempt is reserved first and logged.
func (p *proxy) forward(ctx context.Context, base call, body []byte) (int, []byte) {
	reserve := reservation(body, maxTokens[base.Kind])
	var status int
	var resp []byte
	for attempt := 1; attempt <= 3; attempt++ {
		rec := base
		rec.Attempt, rec.ReservedUSD = attempt, reserve
		if err := p.ledger.reserve(reserve); err != nil {
			rec.Time, rec.Status, rec.Error = time.Now().UTC(), http.StatusPaymentRequired, err.Error()
			p.record(rec)
			return http.StatusPaymentRequired, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`)
		}
		started := time.Now()
		status, resp = p.send(ctx, body)
		rec.Time, rec.Status, rec.LatencyMS = started.UTC(), status, time.Since(started).Milliseconds()
		var doc struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			BaseResp struct {
				StatusCode int    `json:"status_code"`
				StatusMsg  string `json:"status_msg"`
			} `json:"base_resp"`
		}
		if json.Unmarshal(resp, &doc) == nil {
			rec.ResponseID, rec.ResponseModel = doc.ID, doc.Model
			rec.PromptTokens, rec.CompletionTokens = doc.Usage.PromptTokens, doc.Usage.CompletionTokens
			if len(doc.Choices) > 0 {
				rec.FinishReason = doc.Choices[0].FinishReason
			}
			if doc.BaseResp.StatusCode != 0 {
				rec.Error = fmt.Sprintf("base_resp %d: %s", doc.BaseResp.StatusCode, doc.BaseResp.StatusMsg)
			}
		} else if status != http.StatusOK {
			rec.Error = strings.TrimSpace(string(resp))
		}
		rec.USD = cost(rec.PromptTokens, rec.CompletionTokens)
		p.ledger.settle(reserve, rec.USD)
		p.record(rec)
		if !retryable(status) {
			return status, resp
		}
		select {
		case <-ctx.Done():
			return status, resp
		case <-time.After(time.Duration(attempt*attempt) * 3 * time.Second):
		}
	}
	return status, resp
}

func (p *proxy) send(ctx context.Context, body []byte) (int, []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.upstream, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadGateway, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.key)
	resp, err := p.client.Do(req)
	if err != nil {
		return http.StatusBadGateway, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return http.StatusBadGateway, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`)
	}
	return resp.StatusCode, data
}

func (p *proxy) record(c call) {
	line, _ := json.Marshal(c)
	p.logMu.Lock()
	defer p.logMu.Unlock()
	_, _ = p.log.Write(append(line, '\n'))
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
