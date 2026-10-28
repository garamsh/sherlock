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
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// price is the pinned provider's rate in USD per token, used only to
// reserve a request's worst case; what a call cost is OpenRouter's own
// figure.
type price struct{ input, output float64 }

// bytesPerToken under-counts tokens per byte, so a reservation covers the
// input it stands for.
const bytesPerToken = 3

// maxTokens is the output bound the proxy adds to a request that carries
// none; production sends none. A reservation charges the whole bound.
var maxTokens = map[string]int{"synthesis": 1024, "act": 2048}

// call is one attempt as the ledger records it. Request bodies are not
// kept: the runner records what each call was shown.
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
	CachedUSD        float64   `json:"cached_usd,omitempty"`
	LatencyMS        int64     `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ReservedUSD      float64   `json:"reserved_usd"`
	USD              float64   `json:"usd"`
	CostReported     bool      `json:"cost_reported"`
	RequestSHA256    string    `json:"request_sha256"`
	ResponseModel    string    `json:"response_model,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	ResponseID       string    `json:"response_id,omitempty"`
	FinishReason     string    `json:"finish_reason,omitempty"`
}

// ledger reserves each request's worst case against the ceiling before it
// is sent and settles it at its reported cost.
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

func (p price) reservation(body []byte, outputBound int) float64 {
	return float64(len(body))/bytesPerToken*p.input + float64(outputBound)*p.output
}

// requestKind names a chat request by the one reserved tool it offers: a
// synthesis offers memory_summary alone; anything else is an act call.
func requestKind(body []byte) string {
	var doc struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &doc) == nil && len(doc.Tools) == 1 && doc.Tools[0].Function.Name == "memory_summary" {
		return "synthesis"
	}
	return "act"
}

// pinned rewrites a request for the study: the one model, routed only to
// the one provider with no fallback, an output bound where it has none,
// and usage accounting on, so the response reports what it cost. It does
// not require the provider to support every parameter: production sends
// parallel_tool_calls, which MiniMax-M2's endpoint at the study's price
// does not list, and OpenRouter then finds no endpoint.
func pinned(body []byte, model, provider string, bound int) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	doc["model"], _ = json.Marshal(model)
	doc["provider"], _ = json.Marshal(map[string]any{"only": []string{provider}, "allow_fallbacks": false})
	doc["usage"] = json.RawMessage(`{"include":true}`)
	if _, ok := doc["max_tokens"]; !ok {
		doc["max_tokens"] = json.RawMessage(fmt.Sprint(bound))
	}
	return json.Marshal(doc)
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type proxyConfig struct {
	upstream, key, dir string
	ceiling            float64
	model, provider    string
	price              price
}

// proxy forwards the arms' chat requests to OpenRouter under the ledger.
// A synthesis request identical to one already answered is answered from
// the cache, and the ledger charges the arm it served what the first
// answer cost, so both arms are charged for their whole run.
type proxy struct {
	cfg    proxyConfig
	ledger *ledger
	client *http.Client

	logMu sync.Mutex
	log   io.Writer

	cacheMu  sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]chan struct{}
	cacheLog io.Writer
}

type cacheEntry struct {
	Key      string          `json:"key"`
	USD      float64         `json:"usd"`
	Response json.RawMessage `json:"response"`
}

func newProxy(cfg proxyConfig) (*proxy, func(), error) {
	if err := os.MkdirAll(cfg.dir, 0o755); err != nil {
		return nil, nil, err
	}
	callsPath := filepath.Join(cfg.dir, "calls.jsonl")
	cachePath := filepath.Join(cfg.dir, "synthesis-cache.jsonl")
	spent, err := loadSpent(callsPath)
	if err != nil {
		return nil, nil, err
	}
	cache, err := loadCache(cachePath)
	if err != nil {
		return nil, nil, err
	}
	calls, err := os.OpenFile(callsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	cacheLog, err := os.OpenFile(cachePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		calls.Close()
		return nil, nil, err
	}
	p := &proxy{
		cfg: cfg, ledger: &ledger{ceiling: cfg.ceiling, spent: spent},
		client: &http.Client{Timeout: 5 * time.Minute},
		log:    calls, cache: cache, inflight: map[string]chan struct{}{}, cacheLog: cacheLog,
	}
	return p, func() { calls.Close(); cacheLog.Close() }, nil
}

func loadCache(path string) (map[string]cacheEntry, error) {
	out := map[string]cacheEntry{}
	err := eachLine(path, func(line []byte) error {
		var e cacheEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		out[e.Key] = e
		return nil
	})
	return out, err
}

// loadSpent sums what an earlier run of the ledger settled, so a restarted
// proxy keeps counting against the same ceiling.
func loadSpent(path string) (float64, error) {
	var total float64
	err := eachLine(path, func(line []byte) error {
		var c call
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		total += c.USD
		return nil
	})
	return total, err
}

func eachLine(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		if err := fn(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
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
		if entry, done := p.cached(base.RequestSHA256); entry != nil {
			base.Time, base.Cached, base.Status, base.CachedUSD = time.Now().UTC(), true, http.StatusOK, entry.USD
			p.record(base)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(entry.Response)
			return
		} else if done != nil {
			defer done()
		}
	}
	out, err := pinned(body, p.cfg.model, p.cfg.provider, maxTokens[kind])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status, resp, usd := p.forward(r.Context(), base, out)
	if kind == "synthesis" && status == http.StatusOK && summarized(resp) {
		p.store(base.RequestSHA256, resp, usd)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// cached returns a stored synthesis, or, when none is stored, a function
// the caller runs once it has stored one; a second identical request waits
// for the first instead of paying for it again.
func (p *proxy) cached(key string) (*cacheEntry, func()) {
	for {
		p.cacheMu.Lock()
		if entry, ok := p.cache[key]; ok {
			p.cacheMu.Unlock()
			return &entry, nil
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

func (p *proxy) store(key string, resp []byte, usd float64) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	entry := cacheEntry{Key: key, USD: usd, Response: resp}
	p.cache[key] = entry
	line, _ := json.Marshal(entry)
	_, _ = p.cacheLog.Write(append(line, '\n'))
}

// summarized reports whether a synthesis response carries exactly one
// memory_summary call, the only answer the production summarizer accepts.
func summarized(resp []byte) bool {
	var doc struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(resp, &doc) != nil || len(doc.Choices) != 1 {
		return false
	}
	calls := doc.Choices[0].Message.ToolCalls
	return len(calls) == 1 && calls[0].Function.Name == "memory_summary"
}

// attemptsLimit bounds how often one request is sent.
var attemptsLimit = 6

// retryable is a status worth another attempt: rate limiting and server
// faults, never a refusal of the request itself.
func retryable(status int) bool { return status == http.StatusTooManyRequests || status >= 500 }

// backoff is the wait before attempt n+1: 5 s doubling, at most a minute.
func backoff(n int) time.Duration {
	return min(5*time.Second<<(n-1), time.Minute)
}

// upstreamResponse is what the ledger reads from an OpenRouter answer.
type upstreamResponse struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Choices  []struct {
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// forward sends one request, retrying on a transport error, rate limit or
// server fault. An answer with HTTP 200 that carries an error and no
// choice, which OpenRouter returns when the provider fails mid-stream, is
// a server fault. Every attempt is reserved first and logged.
func (p *proxy) forward(ctx context.Context, base call, body []byte) (int, []byte, float64) {
	reserve := p.cfg.price.reservation(body, maxTokens[base.Kind])
	var status int
	var resp []byte
	var usd float64
	for attempt := 1; attempt <= attemptsLimit; attempt++ {
		rec := base
		rec.Attempt, rec.ReservedUSD = attempt, reserve
		if err := p.ledger.reserve(reserve); err != nil {
			rec.Time, rec.Status, rec.Error = time.Now().UTC(), http.StatusPaymentRequired, err.Error()
			p.record(rec)
			return http.StatusPaymentRequired, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`), 0
		}
		started := time.Now()
		status, resp = p.send(ctx, body)
		rec.Time, rec.LatencyMS = started.UTC(), time.Since(started).Milliseconds()
		var doc upstreamResponse
		if json.Unmarshal(resp, &doc) == nil {
			rec.ResponseID, rec.ResponseModel, rec.Provider = doc.ID, doc.Model, doc.Provider
			rec.PromptTokens, rec.CompletionTokens = doc.Usage.PromptTokens, doc.Usage.CompletionTokens
			if len(doc.Choices) > 0 {
				rec.FinishReason = doc.Choices[0].FinishReason
			}
			if doc.Error != nil {
				rec.Error = fmt.Sprintf("%v: %s", doc.Error.Code, doc.Error.Message)
				if status == http.StatusOK && len(doc.Choices) == 0 {
					status = http.StatusBadGateway
				}
			}
			if doc.Usage.Cost != nil {
				rec.USD, rec.CostReported = *doc.Usage.Cost, true
			}
		} else if status != http.StatusOK {
			rec.Error = strings.TrimSpace(string(resp))
		}
		if !rec.CostReported {
			rec.USD = float64(rec.PromptTokens)*p.cfg.price.input + float64(rec.CompletionTokens)*p.cfg.price.output
		}
		rec.Status = status
		usd = rec.USD
		p.ledger.settle(reserve, rec.USD)
		p.record(rec)
		if !retryable(status) || attempt == attemptsLimit {
			break
		}
		select {
		case <-ctx.Done():
			return status, resp, usd
		case <-time.After(backoff(attempt)):
		}
	}
	return status, resp, usd
}

func (p *proxy) send(ctx context.Context, body []byte) (int, []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.upstream, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadGateway, []byte(`{"error":{"message":` + quote(err.Error()) + `}}`)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.key)
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
