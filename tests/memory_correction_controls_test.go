package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func correctionEndpoint(omitWrite bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request correctionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		current, frame := "", ""
		for _, m := range request.Messages {
			if m.Role == "user" {
				current = m.Content
			}
			if m.Role == "system" && strings.HasPrefix(m.Content, "## Current task\n") {
				frame = m.Content
			}
		}
		subject, question := "", ""
		switch {
		case strings.Contains(current, "Mira"):
			subject = "mira"
			question = "office"
		case strings.Contains(current, "Ivo"):
			subject = "ivo"
			question = "locker code"
		case strings.Contains(current, "Neri"):
			subject = "neri"
			question = "locker code"
		}
		name := "memory_read"
		args := map[string]any{"subject": subject, "question": question}
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "memory_read" {
			name = "memory_write"
			reply := "OK"
			ops := []any{}
			if strings.Contains(current, "Remember this") || strings.Contains(current, "Correction") {
				value := regexp.MustCompile(`[A-Z]+-[0-9]+`).FindString(current)
				restriction := ""
				if subject == "mira" {
					restriction = "weekdays"
					if strings.Contains(current, "on weekends") {
						restriction = "weekends"
					}
				}
				if !omitWrite {
					ops = append(ops, map[string]any{"address": map[string]any{"subject": subject, "question": question, "restriction": restriction}, "value": value, "operation": "replace"})
				}
			} else if strings.Contains(current, "What is") {
				reply = "UNKNOWN"
				_, recalled, _ := strings.Cut(frame, "## Recalled memories\n")
				for _, line := range strings.Split(recalled, "\n") {
					if !strings.Contains(line, subject+" — ") {
						continue
					}
					if subject == "mira" {
						restriction := "weekdays"
						if strings.Contains(current, "weekend") {
							restriction = "weekends"
						}
						if !strings.Contains(line, "("+restriction+")") {
							continue
						}
					}
					if value := regexp.MustCompile(`[A-Z]+-[0-9]+`).FindString(line); value != "" {
						reply = value
					}
				}
			}
			args = map[string]any{"reply": reply, "memory_ops": ops}
		}
		arguments, err := json.Marshal(args)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		response := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "correction-call", "type": "function", "function": map[string]any{"name": name, "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}}
		w.Header().Set("Content-Type", "application/json")
		if err = json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
}

func correctionOverlay(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../internal/agent/memory/recall.go")
	require.NoError(t, err)
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	needle := "if !address.Placed() {"
	require.Equal(t, 1, strings.Count(string(original), needle))
	mutated := strings.Replace(string(original), needle, "if true {", 1)
	replacement := filepath.Join(t.TempDir(), "recall.go")
	require.NoError(t, os.WriteFile(replacement, []byte(mutated), 0600))
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	correctionJSON(t, overlay, map[string]any{"Replace": map[string]string{path: replacement}})
	return overlay
}

func TestMemoryCorrection_Free(t *testing.T) {
	binary := correctionBuild(t, "")
	dir := t.TempDir()
	if destination := os.Getenv("GAGENT_CORRECTION_FREE_RESULTS"); destination != "" {
		require.NoError(t, os.Mkdir(destination, 0700))
		dir = destination
	}
	for _, arm := range []string{"intact", "recall-removed", "write-omitted"} {
		t.Run(arm, func(t *testing.T) {
			runDir := filepath.Join(dir, arm)
			executable := binary
			if arm == "recall-removed" {
				executable = correctionBuild(t, correctionOverlay(t))
			}
			correctionSource(t, executable, runDir)
			proxy := &correctionProxy{handler: correctionEndpoint(arm == "write-omitted")}
			server := httptest.NewServer(proxy)
			defer server.Close()
			rows := correctionRun(t, executable, proxy, server.URL, runDir)
			correctionJSON(t, filepath.Join(runDir, "calls.json"), proxy.calls)
			require.Len(t, rows, 10)
			counts := map[string]int{}
			for _, row := range rows {
				counts[row.Verdict]++
				switch arm {
				case "intact":
					require.Equal(t, "accepted", row.Verdict, "%+v", row)
				case "recall-removed":
					if row.Expected == "UNKNOWN" {
						require.Equal(t, "accepted", row.Verdict)
					} else {
						require.Equal(t, "recall failure", row.Verdict, "%+v", row)
					}
				case "write-omitted":
					require.Equal(t, "invalid premise", row.Verdict, "%+v", row)
				}
			}
			t.Logf("%s: %v, %d model calls (scripted; USD 0)", arm, counts, len(proxy.calls))
		})
	}
}

func TestMemoryCorrection_RejectsBrokenEvidence(t *testing.T) {
	memory := correctionMemory{ID: 7, Subject: "ivo", Question: "locker code", Value: "LARCH-731"}
	request := correctionRequest{Messages: []correctionMessage{{Role: "system", Content: "instructions"}, {Role: "system", Content: "## Current task\nWhat is Ivo's locker code?\n\n## Recalled memories\n- [7] (1h): ivo — locker code: LARCH-731\n"}, {Role: "user", Content: correctionProbes[2].Prompt}}}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	turn := correctionTurn{Label: "control", Answer: "LARCH-731", Before: correctionStore{Active: []correctionMemory{memory}}, Calls: []correctionCall{{Request: body}}, Events: []json.RawMessage{json.RawMessage(`{"kind":"recall","recalled_ids":["7"]}`)}}
	require.Equal(t, "accepted", correctionJudge(turn, correctionProbes[2], nil).Verdict)
	tests := []struct {
		name, want string
		change     func(*correctionTurn)
	}{
		{"answer from history", "invalid premise", func(turn *correctionTurn) {
			request.Messages[0].Content = "Ivo's locker code is LARCH-731"
			body, err := json.Marshal(request)
			require.NoError(t, err)
			turn.Calls = []correctionCall{{Request: body}}
		}},
		{"oracle answer without recall event", "recall failure", func(turn *correctionTurn) { turn.Events = nil }},
		{"oracle answer without stored fact", "recall failure", func(turn *correctionTurn) { turn.Before.Active = nil }},
		{"incorrect answer", "reasoning failure", func(turn *correctionTurn) { turn.Answer = "CEDAR-214" }},
		{"missing model input", "invalid premise", func(turn *correctionTurn) { turn.Calls = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := turn
			test.change(&changed)
			require.Equal(t, test.want, correctionJudge(changed, correctionProbes[2], nil).Verdict)
		})
	}
}

func TestMemoryCorrection_SpendGate(t *testing.T) {
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, err := io.Copy(io.Discard, r.Body)
		require.NoError(t, err)
		_, err = w.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
		require.NoError(t, err)
	})
	proxy := &correctionProxy{handler: handler}
	body := []byte(`{"messages":[]}`)
	for i := 0; i < 2; i++ {
		if i == 1 {
			proxy.reserved = correctionUSD
		}
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, httptest.NewRequest("POST", "/chat/completions", bytes.NewReader(body)))
		require.Equal(t, []int{200, 402}[i], response.Code)
	}
	require.Equal(t, 1, calls, "rejected request must not reach upstream")
	require.Equal(t, body, proxy.calls[0].Request)
}

func TestMemoryCorrection_ProxyCapsOutputAndStopsOnUnverifiableUsage(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		var request struct {
			MaxTokens int `json:"max_tokens"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, correctionOutput, request.MaxTokens)
		require.Equal(t, "Bearer inert-control", r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"choices":[]}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()
	proxy := &correctionProxy{upstream: upstream.URL, key: "inert-control"}
	for _, want := range []int{200, 402} {
		recorder := httptest.NewRecorder()
		proxy.ServeHTTP(recorder, httptest.NewRequest("POST", "/chat/completions", strings.NewReader(`{"messages":[]}`)))
		require.Equal(t, want, recorder.Code)
	}
	require.Equal(t, 1, upstreamCalls)
	require.Equal(t, "usage absent or reservation premise invalid", proxy.calls[0].Failure)
	require.JSONEq(t, `{"messages":[],"max_tokens":2048}`, string(proxy.calls[0].Request))
	require.Equal(t, `{"choices":[]}`, string(proxy.calls[0].Response))
}

func TestMemoryCorrection_UnstatedProbeRejectsInventedMemory(t *testing.T) {
	request := correctionRequest{Messages: []correctionMessage{
		{Role: "system", Content: "## Current task\nWhat is Neri's locker code?\n\n## Recalled memories\nNo relevant memories."},
		{Role: "user", Content: correctionProbes[3].Prompt},
	}}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	turn := correctionTurn{Label: "unstated", Answer: "UNKNOWN", Calls: []correctionCall{{Request: body}}, Events: []json.RawMessage{json.RawMessage(`{"kind":"recall","recalled_ids":[]}`)}}
	require.Equal(t, "accepted", correctionJudge(turn, correctionProbes[3], nil).Verdict)
	unrelated := turn
	unrelated.Before.Active = []correctionMemory{{ID: 7, Subject: "ivo", Question: "locker code", Value: "LARCH-731"}}
	unrelated.Events = []json.RawMessage{json.RawMessage(`{"kind":"recall","recalled_ids":["7"]}`)}
	unrelatedRequest := request
	unrelatedRequest.Messages = append([]correctionMessage(nil), request.Messages...)
	unrelatedRequest.Messages[0].Content = "## Current task\nWhat is Neri's locker code?\n\n## Recalled memories\n- [7]: ivo — locker code: LARCH-731"
	unrelatedBody, err := json.Marshal(unrelatedRequest)
	require.NoError(t, err)
	unrelated.Calls = []correctionCall{{Request: unrelatedBody}}
	require.Equal(t, "accepted", correctionJudge(unrelated, correctionProbes[3], nil).Verdict, "unrelated recall does not invent a Neri fact")
	tests := []struct {
		name   string
		change func(*correctionTurn)
	}{
		{"missing recall event", func(turn *correctionTurn) { turn.Events = nil }},
		{"invented active fact", func(turn *correctionTurn) {
			turn.Before.Active = []correctionMemory{{ID: 9, Subject: "Neri", Question: "locker code", Value: "PINE-419"}}
		}},
		{"recalled invented fact", func(turn *correctionTurn) {
			turn.Events = []json.RawMessage{json.RawMessage(`{"kind":"recall","recalled_ids":["9"]}`)}
			recalled := request
			recalled.Messages = append([]correctionMessage(nil), request.Messages...)
			recalled.Messages[0].Content = "## Current task\nWhat is Neri's locker code?\n\n## Recalled memories\n- [9]: Neri — locker code: PINE-419"
			body, err := json.Marshal(recalled)
			require.NoError(t, err)
			turn.Calls = []correctionCall{{Request: body}}
		}},
		{"injected fact without recall IDs", func(turn *correctionTurn) {
			request.Messages[0].Content = "## Current task\nWhat is Neri's locker code?\n\n## Recalled memories\n- [9]: Neri — locker code: PINE-419"
			body, err := json.Marshal(request)
			require.NoError(t, err)
			turn.Calls = []correctionCall{{Request: body}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := turn
			test.change(&changed)
			row := correctionJudge(changed, correctionProbes[3], nil)
			require.True(t, row.AnswerCorrect)
			require.Equal(t, "invalid premise", row.Verdict)
			require.NotEmpty(t, row.Invalid)
		})
	}
}
