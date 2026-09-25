package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Shared fixture plumbing for the remaining file-operation and spend-meter
// tests. The addressed-memory experiment entry points were retired in #823.
const (
	recallProvider            = "openai-compatible"
	recallUpstream            = "https://api.minimax.io/v1"
	recallModelName           = "MiniMax-M2"
	recallKeyEnv              = "MINIMAX_KEY"
	recallInputUSDPerMillion  = .30
	recallOutputUSDPerMillion = 1.20
)

type turnRecord struct {
	Events        []map[string]any `json:"events,omitempty"`
	DurationMS    int64            `json:"duration_ms"`
	Message       string           `json:"message"`
	RecallOutcome string           `json:"recall_outcome"`
	RecalledIDs   []string         `json:"recalled_ids"`
	RecalledCount int              `json:"recalled_count"`
	Response      string           `json:"response"`
	Ops           []any            `json:"ops,omitempty"`
	Failed        string           `json:"failed,omitempty"`
}

func writeMeteredConfig(t *testing.T, baseURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	body := fmt.Sprintf("model:\n  provider: %s\n  base-url: %s\n  model: %s\n  api-key-env: %s\n  timeout: 300s\n", recallProvider, baseURL, recallModelName, recallKeyEnv)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o600))
}

func informationCalls(meter *spendMeter) []meteredCall {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return append([]meteredCall(nil), meter.calls...)
}

const recallTurnTimeout = 15 * time.Minute

func runRecallTurn(t *testing.T, binary, root, workspaceAddr, configPath, message string) turnRecord {
	t.Helper()
	cmd, baseURL, exited := startAgentBinary(t, binary, root, workspaceAddr, configPath)
	events := subscribeSSE(t, baseURL+"/agents/"+binaryAgentID+"/events")
	record := collectRecallTurn(t, baseURL, events, message)
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-exited:
	case <-time.After(30 * time.Second):
		t.Fatal("agent did not exit on SIGTERM")
	}
	return record
}
func collectRecallTurn(t *testing.T, baseURL string, events <-chan map[string]any, message string) turnRecord {
	t.Helper()
	started := time.Now()
	resp := postJSON(t, baseURL+"/agents/"+binaryAgentID+"/messages", `{"content":`+strconvQuote(message)+`}`)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	record := turnRecord{Message: message}
	deadline := time.After(recallTurnTimeout)
	for done := false; !done; {
		select {
		case event, open := <-events:
			require.True(t, open, "event stream closed before turn ended")
			record.Events = append(record.Events, event)
			switch event["kind"] {
			case "recall":
				record.RecallOutcome, _ = event["recall_outcome"].(string)
				record.RecalledCount = intField(event["recalled_count"])
				record.RecalledIDs = stringsField(event["recalled_ids"])
			case "writeback":
				record.Ops, _ = event["ops"].([]any)
			case "turn_finished":
				record.Response, _ = event["response"].(string)
				done = true
			case "turn_failed":
				record.Failed, _ = event["error"].(string)
				done = true
			}
		case <-deadline:
			record.Failed = "turn did not finish within " + recallTurnTimeout.String()
			done = true
		}
	}
	record.DurationMS = time.Since(started).Milliseconds()
	return record
}
func intField(v any) int { n, _ := v.(float64); return int(n) }
func stringsField(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

type scriptedAnswer struct{ Subject, Question, Value, Reply string }

func startScriptedEndpoint(t *testing.T, answer scriptedAnswer) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		name, arguments := "memory_write", fmt.Sprintf(`{"reply":%s,"memory_ops":[{"address":{"subject":%s,"question":%s,"restriction":null},"value":%s,"operation":null}]}`, strconvQuote(answer.Reply), strconvQuote(answer.Subject), strconvQuote(answer.Question), strconvQuote(answer.Value))
		if len(request.Tools) == 1 && request.Tools[0].Function.Name == "memory_search_terms" {
			name = "memory_search_terms"
			arguments = `{"phrases":["` + answer.Subject + `"]}`
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":%s,"arguments":%s}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`, strconvQuote(name), strconvQuote(arguments))
	}))
	t.Cleanup(server.Close)
	return server.URL
}
