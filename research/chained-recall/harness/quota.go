package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// errQuotaPaused is what the proxy answers while the key's usage limit
// holds, without sending anything upstream.
const errQuotaPaused = "study proxy paused: provider usage limit (2056); probing every 30 minutes"

// probeInterval is the least time between two probes of a paused key.
const probeInterval = 30 * time.Minute

// usageLimit reports whether a response is the provider's usage-limit
// refusal: HTTP 429 carrying code 2056, in base_resp or in the error text.
func usageLimit(status int, body []byte) bool {
	if status != http.StatusTooManyRequests {
		return false
	}
	var doc struct {
		BaseResp struct {
			StatusCode int `json:"status_code"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(body, &doc) == nil && doc.BaseResp.StatusCode == 2056 {
		return true
	}
	return bytes.Contains(body, []byte("2056"))
}

// quotaGate holds every request while the key's usage limit holds. The
// first 2056 closes it; a prober then sends one minimal ledgered request
// every probeInterval and opens the gate on the first success.
type quotaGate struct {
	mu     sync.Mutex
	closed bool
	probe  func(context.Context) (int, []byte)
	log    *os.File
	wait   time.Duration
}

func (g *quotaGate) paused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// close shuts the gate and starts the prober, once per closure.
func (g *quotaGate) close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()
	g.note("paused on 2056")
	go g.probeUntilOpen()
}

func (g *quotaGate) probeUntilOpen() {
	for {
		time.Sleep(g.wait)
		status, body := g.probe(context.Background())
		g.note(fmt.Sprintf("probe: HTTP %d", status))
		if status == http.StatusOK {
			g.mu.Lock()
			g.closed = false
			g.mu.Unlock()
			g.note("resumed")
			return
		}
		if !usageLimit(status, body) && status != http.StatusTooManyRequests {
			g.note(fmt.Sprintf("probe answered HTTP %d without the usage-limit code; still paused", status))
		}
	}
}

func (g *quotaGate) note(text string) {
	line := time.Now().UTC().Format(time.RFC3339) + " " + text
	log.Print(line)
	if g.log != nil {
		_, _ = g.log.WriteString(line + "\n")
	}
}

// probeRequest is the minimal chat request a probe sends.
var probeRequest = []byte(`{"model":"MiniMax-M2","messages":[{"role":"user","content":"Reply with the word ok."}]}`)

// probeKey sends probeRequest through the ledger, attributed to phase
// "probe", with a small output bound.
func (p *proxy) probeKey(ctx context.Context) (int, []byte) {
	body, err := withMaxTokens(probeRequest, 64)
	if err != nil {
		return http.StatusBadRequest, nil
	}
	rec := call{Arm: "study", Episode: "quota", Phase: "probe", Kind: "probe", Attempt: 1, RequestSHA256: sha(body)}
	reserve := reservation(body, 64)
	if err := p.ledger.reserve(reserve); err != nil {
		rec.Time, rec.Status, rec.Error = time.Now().UTC(), http.StatusPaymentRequired, err.Error()
		p.record(rec)
		return http.StatusPaymentRequired, nil
	}
	started := time.Now()
	status, resp := p.send(ctx, body)
	rec.Time, rec.Status, rec.LatencyMS, rec.ReservedUSD = started.UTC(), status, time.Since(started).Milliseconds(), reserve
	var doc struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(resp, &doc) == nil {
		rec.PromptTokens, rec.CompletionTokens = doc.Usage.PromptTokens, doc.Usage.CompletionTokens
	}
	if status != http.StatusOK {
		rec.Error = string(resp)
		if len(rec.Error) > 500 {
			rec.Error = rec.Error[:500]
		}
	}
	rec.USD = cost(rec.PromptTokens, rec.CompletionTokens)
	p.ledger.settle(reserve, rec.USD)
	p.record(rec)
	return status, resp
}
