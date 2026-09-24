package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
)

type episodicSeed struct{ interrupted bool }

func (s episodicSeed) Summarize(context.Context, []string) (string, error) {
	if s.interrupted {
		return "", errors.New("fixture leaves a pending checkpoint")
	}
	return "The current project codename is Blue Cedar. The preferred tea is jasmine.", nil
}

type episodicEvents struct {
	retrieved       []string
	inspectedSource bool
}

func (e *episodicEvents) Emit(_ context.Context, event agent.Event) {
	if event.Kind == agent.EventRecall {
		e.retrieved = event.RecalledIDs
	}
	if event.Kind == agent.EventToolCall && event.ToolName == agent.MemoryInspectTool && !event.ToolFailed && strings.Contains(event.ToolResult, "Silver Pine") {
		e.inspectedSource = true
	}
}

type episodicNoTools struct{}

func (episodicNoTools) List() []tool.Descriptor { return nil }

func (episodicNoTools) Call(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("probe has no external tools")
}

// TestEpisodicMemory_LiveProbe uses scripted history only to reach a batch
// boundary; startup synthesis, recall, inspection decisions, and answer are live.
func TestEpisodicMemory_LiveProbe(t *testing.T) {
	if os.Getenv("GAGENT_EPISODIC_MEMORY_PROBE") != "1" {
		t.Skip("live episodic probe requires explicit opt-in")
	}
	key := os.Getenv(recallKeyEnv)
	require.NotEmpty(t, key)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path)
	require.NoError(t, err)
	var seed []agent.SourceMessage
	for i := 0; i < 56; i++ {
		content := "Routine status check."
		if i == 0 {
			content = "The project codename is Blue Cedar. My preferred tea is jasmine."
		}
		seed = append(seed, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	require.NoError(t, store.RecordTurn(ctx, seed, episodicSeed{}))
	seed = nil
	for i := 0; i < 8; i++ {
		content := "Routine status check."
		if i == 0 {
			content = "Correction: the current project codename is Silver Pine. Blue Cedar is obsolete."
		}
		seed = append(seed, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	require.Error(t, store.RecordTurn(ctx, seed, episodicSeed{interrupted: true}))
	require.NoError(t, store.Close())
	meter := startSpendMeter(t, recallUpstream, key, spendCeiling{Calls: 8, InputTokens: 20000, OutputTokens: 4000, USD: .02, InputUSDPerMillion: recallInputUSDPerMillion, OutputUSDPerMillion: recallOutputUSDPerMillion})
	// Reserve a conservative byte-based input allowance and a hard output maximum
	// before forwarding; usage alone can only stop after an over-budget response.
	limiter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		spent := meter.Spend()
		if spent.Calls >= 8 || spent.OutputTokens >= 4000 || spent.InputTokens+int64(len(body))+1024 > 20000 {
			http.Error(w, "probe budget reservation refused", http.StatusPaymentRequired)
			return
		}
		var request map[string]json.RawMessage
		if json.Unmarshal(body, &request) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		request["max_tokens"], _ = json.Marshal(min(int64(800), 4000-spent.OutputTokens))
		body, _ = json.Marshal(request)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, meter.BaseURL()+"/chat/completions", strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "construct request", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer limiter.Close()
	model, err := openai.New(openai.Config{BaseURL: limiter.URL, Model: recallModelName, APIKey: "local-probe", Timeout: 90 * time.Second})
	require.NoError(t, err)
	events := &episodicEvents{}
	factory := gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Tools: episodicNoTools{}, MaxHistoryMessages: 2}
	runtime, runErr := factory.New(ctx, "probe", events)
	reply := ""
	if runErr == nil {
		reply, runErr = runtime.Session.Run(ctx, "What is the current project codename? Inspect the linked memory evidence and quote the exact correction message, then answer through memory_write. If the evidence is unavailable, say so.")
		require.NoError(t, runtime.Close(ctx))
	}
	store, err = sqlite.OpenConversation(path)
	require.NoError(t, err)
	defer store.Close()
	mids, err := store.RecentMid(ctx, 1)
	require.NoError(t, err)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	midCorrected := len(mids) == 1 && strings.Contains(mids[0].Summary, "Silver Pine")
	report := map[string]any{"model": recallModelName, "scripted_setup_sources": 56, "pending_correction_sources": 8, "spend": meter.Spend(), "mid_contains_correction": midCorrected, "retrieved_ids": events.retrieved, "exact_correction_source_inspected": events.inspectedSource, "answer_contains_current_value": strings.Contains(reply, "Silver Pine"), "runtime_failed": runErr != nil, "pending_source_count": len(pending), "limits": "8 calls, 20000 input tokens, 4000 output tokens, no retries; byte-based input reservation may stop early"}
	if destination := os.Getenv("GAGENT_EPISODIC_MEMORY_REPORT"); destination != "" {
		writeJSON(t, destination, report)
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	t.Log(string(encoded))
	assert.LessOrEqual(t, meter.Spend().Calls, 8)
	assert.LessOrEqual(t, meter.Spend().InputTokens, int64(20000))
	assert.LessOrEqual(t, meter.Spend().OutputTokens, int64(4000))
	// Collection success is separate from model quality; the report retains failures.
}
