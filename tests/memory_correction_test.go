package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const correctionFillers = 48
const correctionCalls = 260
const correctionUSD = 2.00
const correctionOutput = 2048

var correctionPlants = []string{
	"Remember this durable fact: Ivo's locker code is LARCH-731.",
	"Remember this durable fact: Mira's office on weekdays is CEDAR-214.",
	"Remember this additional durable fact: Mira's office on weekends is QUARTZ-806. Her weekday office is unchanged.",
	"Correction: Mira's weekday office is now BASALT-592, replacing her former weekday office. Her weekend office is unchanged.",
}

const correctionReversal = "Correction again: Mira's weekday office has returned to CEDAR-214, replacing the intervening weekday office. Her weekend office is unchanged."

type correctionProbe struct{ Name, Prompt, Value string }

var correctionUnstatedName = regexp.MustCompile(`(?i)\bneri\b`)

var correctionProbes = []correctionProbe{
	{"corrected", "What is Mira's current weekday office?", "BASALT-592"},
	{"coexisting", "What is Mira's current weekend office?", "QUARTZ-806"},
	{"old", "What is Ivo's locker code?", "LARCH-731"},
	{"unstated", "What is Neri's locker code?", "UNKNOWN"},
}

type correctionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type correctionRequest struct {
	Messages []correctionMessage `json:"messages"`
	Tools    []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}
type correctionCall struct {
	Label        string `json:"label"`
	Request      []byte `json:"request_bytes"`
	Response     []byte `json:"response_bytes"`
	Status       int    `json:"status"`
	DurationMS   int64  `json:"duration_ms"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Failure      string `json:"failure,omitempty"`
}
type correctionProxy struct {
	mu            sync.Mutex
	label         string
	calls         []correctionCall
	reserved      float64
	upstream, key string
	handler       http.Handler
}

func (p *correctionProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	call := correctionCall{Label: p.label, Request: body}
	started := time.Now()
	defer func() { call.DurationMS = time.Since(started).Milliseconds(); p.calls = append(p.calls, call) }()
	// Reserve a byte-based input bound and the enforced output cap before sending.
	reserve := float64(len(body)+4096)*0.30/1e6 + correctionOutput*1.20/1e6
	if len(p.calls) >= correctionCalls || p.reserved+reserve > correctionUSD {
		call.Status = 402
		call.Failure = "preflight spend ceiling"
		http.Error(w, call.Failure, http.StatusPaymentRequired)
		return
	}
	p.reserved += reserve
	if p.handler != nil {
		recorder := httptest.NewRecorder()
		forwarded := r.Clone(r.Context())
		forwarded.Body = io.NopCloser(bytes.NewReader(body))
		p.handler.ServeHTTP(recorder, forwarded)
		call.Status = recorder.Code
		call.Response = bytes.Clone(recorder.Body.Bytes())
	} else {
		var wire map[string]json.RawMessage
		if err = json.Unmarshal(body, &wire); err != nil {
			call.Failure = err.Error()
			http.Error(w, call.Failure, 400)
			return
		}
		wire["max_tokens"] = json.RawMessage(strconv.Itoa(correctionOutput))
		sent, marshalErr := json.Marshal(wire)
		if marshalErr != nil {
			call.Failure = marshalErr.Error()
			http.Error(w, call.Failure, 400)
			return
		}
		call.Request = sent
		request, requestErr := http.NewRequestWithContext(r.Context(), http.MethodPost, p.upstream+"/chat/completions", bytes.NewReader(sent))
		if requestErr != nil {
			call.Failure = requestErr.Error()
			http.Error(w, call.Failure, 500)
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+p.key)
		response, responseErr := (&http.Client{Timeout: 3 * time.Minute}).Do(request)
		if responseErr != nil {
			call.Failure = responseErr.Error()
			http.Error(w, call.Failure, http.StatusBadGateway)
			return
		}
		call.Status = response.StatusCode
		call.Response, err = io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			call.Failure = fmt.Sprintf("read=%v close=%v", err, closeErr)
		}
	}
	var usage struct {
		Usage struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err = json.Unmarshal(call.Response, &usage); err != nil {
		call.Failure = "invalid provider JSON: " + err.Error()
	}
	call.InputTokens = usage.Usage.Input
	call.OutputTokens = usage.Usage.Output
	if p.handler == nil && (call.InputTokens <= 0 || call.OutputTokens <= 0 || call.InputTokens > int64(len(body)+4096) || call.OutputTokens > correctionOutput) {
		call.Failure = "usage absent or reservation premise invalid"
		p.reserved = correctionUSD
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(call.Status)
	if _, err = w.Write(call.Response); err != nil {
		call.Failure = err.Error()
	}
}

func correctionJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0600))
}
func correctionBuild(t *testing.T, overlay string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "gagent")
	args := []string{"build", "-o", binary}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	args = append(args, "./cmd/gagent")
	command := exec.Command("go", args...)
	command.Dir = ".."
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	for _, name := range []string{"file_read", "file_write", "file_list", "shell_exec", "message_send", "github"} {
		toolDir := filepath.Join(filepath.Dir(binary), "tools", name)
		require.NoError(t, os.MkdirAll(toolDir, 0700))
		manifest, err := os.ReadFile(filepath.Join("..", "tools", name, "manifest.json"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0600))
		build := exec.Command("go", "build", "-o", filepath.Join(toolDir, name), "./tools/"+name)
		build.Dir = ".."
		output, err := build.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	return binary
}

type correctionProcess struct {
	command  *exec.Cmd
	exited   chan error
	stopOnce sync.Once
	URL      string
	Events   chan json.RawMessage
}

func (p *correctionProcess) stop(t *testing.T) {
	t.Helper()
	p.stopOnce.Do(func() {
		err := p.command.Process.Signal(syscall.SIGTERM)
		if err != nil {
			require.ErrorIs(t, err, os.ErrProcessDone)
		}
		select {
		case err = <-p.exited:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			require.NoError(t, p.command.Process.Kill())
			<-p.exited
			t.Error("agent required kill")
		}
	})
}
func correctionStart(t *testing.T, binary, root, endpoint, dir string) *correctionProcess {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0700))
	require.NoError(t, os.MkdirAll(dir, 0700))
	config := filepath.Join(dir, "agent.yaml")
	body := fmt.Sprintf("model:\n  provider: openai-compatible\n  model: MiniMax-M2\n  base-url: %s\n  api-key-env: CORRECTION_PROXY_KEY\n  timeout: 180s\n", endpoint)
	require.NoError(t, os.WriteFile(config, []byte(body), 0600))
	ego := filepath.Join(dir, "ego.md")
	require.NoError(t, os.WriteFile(ego, []byte("Remember durable facts and their corrections. Answer questions with only the requested value, or UNKNOWN when it is not known. Acknowledge other messages briefly. Do not use workspace tools for this conversation."), 0600))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	log, err := os.Create(filepath.Join(dir, "process.log"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Close()) })
	command := exec.Command(binary, "agent", "--config", config, "--ego-file", ego, "--addr", addr, "--agent-id", "correction", "--memory-path", filepath.Join(root, "memory.db"))
	// Only inert, explicitly named environment values reach the child.
	tools := filepath.Join(filepath.Dir(binary), "tools")
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "GAGENT_TOOLS_DIR=" + tools, "GH_TOKEN=free-control", "CORRECTION_PROXY_KEY=local-proxy"}
	command.Stdout = log
	command.Stderr = log
	require.NoError(t, command.Start())
	p := &correctionProcess{command: command, exited: make(chan error, 1), URL: "http://" + addr, Events: make(chan json.RawMessage, 256)}
	go func() { p.exited <- command.Wait() }()
	t.Cleanup(func() { p.stop(t) })
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		response, e := client.Get(p.URL + "/")
		if e != nil {
			return false
		}
		require.NoError(t, response.Body.Close())
		return true
	}, 10*time.Second, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL+"/agents/correction/events", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	go func() {
		defer close(p.Events)
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				select {
				case p.Events <- json.RawMessage(data):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return p
}

type correctionStore struct {
	Active  []correctionMemory `json:"active"`
	Archive []correctionMemory `json:"archive"`
}
type correctionMemory struct {
	ID           int64         `json:"id"`
	Subject      string        `json:"subject"`
	Question     string        `json:"question"`
	Restriction  string        `json:"restriction"`
	Value        string        `json:"value"`
	SupersededBy sql.NullInt64 `json:"superseded_by"`
	ValidTo      sql.NullInt64 `json:"valid_to"`
	Importance   float64       `json:"importance_score"`
	AccessCount  int64         `json:"access_count"`
	LastAccessed sql.NullInt64 `json:"last_accessed"`
	Timestamp    int64         `json:"timestamp"`
	Source       string        `json:"source"`
}

func correctionRead(t *testing.T, root string) correctionStore {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(root, "memory.db")+"?mode=ro")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var store correctionStore
	for i, table := range []string{"memories_active", "memories_archive"} {
		rows, err := db.Query("SELECT id,subject,question,restriction,value,superseded_by,valid_to,importance_score,access_count,last_accessed,timestamp,source FROM " + table + " ORDER BY id")
		require.NoError(t, err)
		memories := []correctionMemory{}
		for rows.Next() {
			var m correctionMemory
			require.NoError(t, rows.Scan(&m.ID, &m.Subject, &m.Question, &m.Restriction, &m.Value, &m.SupersededBy, &m.ValidTo, &m.Importance, &m.AccessCount, &m.LastAccessed, &m.Timestamp, &m.Source))
			memories = append(memories, m)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		if i == 0 {
			store.Active = memories
		} else {
			store.Archive = memories
		}
	}
	return store
}
func correctionHas(rows []correctionMemory, value string) bool {
	for _, row := range rows {
		if strings.Contains(row.Value, value) {
			return true
		}
	}
	return false
}
func correctionPremise(store correctionStore, current, retired string) []string {
	var failures []string
	for _, value := range []string{current, "QUARTZ-806", "LARCH-731"} {
		if !correctionHas(store.Active, value) {
			failures = append(failures, "active value missing: "+value)
		}
	}
	if correctionHas(store.Active, retired) {
		failures = append(failures, "obsolete value active: "+retired)
	}
	if !correctionHas(store.Archive, retired) {
		failures = append(failures, "obsolete value not archived: "+retired)
	}
	for _, old := range store.Archive {
		if !strings.Contains(old.Value, retired) {
			continue
		}
		linked := false
		for _, now := range store.Active {
			if strings.Contains(now.Value, current) && old.Subject == now.Subject && old.Question == now.Question && old.Restriction == now.Restriction && old.SupersededBy.Valid && old.SupersededBy.Int64 == now.ID && old.ValidTo.Valid {
				linked = true
			}
		}
		if !linked {
			failures = append(failures, "correction did not archive the same address with a successor")
		}
	}
	for _, now := range store.Active {
		if !strings.Contains(now.Value, current) {
			continue
		}
		for _, co := range store.Active {
			if strings.Contains(co.Value, "QUARTZ-806") && now.Subject == co.Subject && now.Question == co.Question && now.Restriction == co.Restriction {
				failures = append(failures, "coexisting fact shares corrected address")
			}
		}
	}
	return failures
}

type correctionTurn struct {
	Label, Prompt, Answer, Failure string
	PID                            int
	DurationMS                     int64
	Events                         []json.RawMessage
	Before, After                  correctionStore
	Calls                          []correctionCall
}

func correctionRunTurn(t *testing.T, p *correctionProcess, proxy *correctionProxy, root, dir, label, prompt string) correctionTurn {
	t.Helper()
	proxy.mu.Lock()
	proxy.label = label
	first := len(proxy.calls)
	proxy.mu.Unlock()
	turn := correctionTurn{Label: label, Prompt: prompt, PID: p.command.Process.Pid, Before: correctionRead(t, root)}
	started := time.Now()
	body, err := json.Marshal(map[string]string{"content": prompt})
	require.NoError(t, err)
	response, err := http.Post(p.URL+"/agents/correction/messages", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, 202, response.StatusCode)
	require.NoError(t, response.Body.Close())
	timer := time.NewTimer(4 * time.Minute)
	defer timer.Stop()
	for done := false; !done; {
		select {
		case event, open := <-p.Events:
			require.True(t, open, "event stream closed")
			turn.Events = append(turn.Events, event)
			var parsed struct{ Kind, Response, Error string }
			require.NoError(t, json.Unmarshal(event, &parsed))
			switch parsed.Kind {
			case "turn_finished":
				turn.Answer = parsed.Response
				done = true
			case "turn_failed":
				turn.Failure = parsed.Error
				done = true
			}
		case <-timer.C:
			t.Fatal("turn timeout")
		}
	}
	turn.DurationMS = time.Since(started).Milliseconds()
	turn.After = correctionRead(t, root)
	proxy.mu.Lock()
	turn.Calls = append([]correctionCall(nil), proxy.calls[first:]...)
	proxy.mu.Unlock()
	correctionJSON(t, filepath.Join(dir, label+".json"), turn)
	return turn
}

type correctionJudgment struct {
	Case, Expected, Answer, Verdict             string
	Invalid                                     []string
	RecalledIDs                                 []string
	HistoryAbsent, RecallPresent, AnswerCorrect bool
	DurationMS                                  int64
}

func correctionJudge(turn correctionTurn, probe correctionProbe, premise []string) correctionJudgment {
	row := correctionJudgment{Case: turn.Label, Expected: probe.Value, Answer: turn.Answer, Invalid: append([]string(nil), premise...), DurationMS: turn.DurationMS}
	row.AnswerCorrect = strings.EqualFold(strings.TrimSpace(turn.Answer), probe.Value)
	if turn.Failure != "" {
		row.Invalid = append(row.Invalid, "turn failed: "+turn.Failure)
	}
	recallObserved := false
	for _, event := range turn.Events {
		var e struct {
			Kind string
			IDs  []string `json:"recalled_ids"`
		}
		if json.Unmarshal(event, &e) == nil && e.Kind == "recall" {
			row.RecalledIDs = e.IDs
			recallObserved = true
		}
	}
	if probe.Value == "UNKNOWN" {
		if !recallObserved {
			row.Invalid = append(row.Invalid, "unstated control has no observed recall event")
		}
		for _, memory := range turn.Before.Active {
			if correctionUnstatedName.MatchString(strings.Join([]string{memory.Subject, memory.Question, memory.Restriction, memory.Value}, " ")) {
				row.Invalid = append(row.Invalid, "unstated control already has an active Neri fact")
			}
		}

	}
	found := false
	for _, call := range turn.Calls {
		if call.Failure != "" {
			row.Invalid = append(row.Invalid, call.Failure)
		}
		var request correctionRequest
		if json.Unmarshal(call.Request, &request) != nil {
			row.Invalid = append(row.Invalid, "request undecodable")
			continue
		}
		if len(request.Tools) == 1 && request.Tools[0].Function.Name == "memory_read" {
			continue
		}
		if found {
			continue
		}
		found = true
		frame := -1
		for i, m := range request.Messages {
			if m.Role == "system" && strings.HasPrefix(m.Content, "## Current task\n") {
				frame = i
			}
		}
		if frame < 0 {
			row.Invalid = append(row.Invalid, "reasoning frame missing")
			continue
		}
		row.HistoryAbsent = true
		for _, m := range request.Messages[:frame] {
			for _, value := range []string{"CEDAR-214", "BASALT-592", "QUARTZ-806", "LARCH-731"} {
				if strings.Contains(m.Content, value) {
					row.HistoryAbsent = false
				}
			}
		}
		if !row.HistoryAbsent {
			row.Invalid = append(row.Invalid, "answer-bearing history remains; restart or compaction premise invalid")
		}
		_, recall, ok := strings.Cut(request.Messages[frame].Content, "## Recalled memories\n")
		if probe.Value == "UNKNOWN" && correctionUnstatedName.MatchString(recall) {
			row.Invalid = append(row.Invalid, "unstated control reasoning frame contains Neri recall evidence")
		}
		for _, memory := range turn.Before.Active {
			if correctionHas([]correctionMemory{memory}, probe.Value) && strings.Contains(recall, probe.Value) && strings.Contains(recall, "["+strconv.FormatInt(memory.ID, 10)+"]") {
				for _, id := range row.RecalledIDs {
					if id == strconv.FormatInt(memory.ID, 10) {
						row.RecallPresent = ok
					}
				}
			}
		}
	}
	if !found {
		row.Invalid = append(row.Invalid, "no reasoning request")
	}
	row.Verdict = "accepted"
	switch {
	case len(row.Invalid) > 0:
		row.Verdict = "invalid premise"
	case probe.Value != "UNKNOWN" && !row.RecallPresent:
		row.Verdict = "recall failure"
	case !row.AnswerCorrect:
		row.Verdict = "reasoning failure"
	}
	return row
}

func correctionSnapshot(t *testing.T, from, to string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(to, 0700))
	db, err := sql.Open("sqlite", "file:"+filepath.Join(from, "memory.db")+"?mode=ro")
	require.NoError(t, err)
	_, err = db.Exec("VACUUM INTO ?", filepath.Join(to, "memory.db"))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.Equal(t, correctionRead(t, from), correctionRead(t, to), "snapshot must retain actual agent-authored state")
}

func correctionCopy(t *testing.T, from, to string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(to, 0700))
	body, err := os.ReadFile(filepath.Join(from, "memory.db"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(to, "memory.db"), body, 0600))
}
func correctionPlantFailures(turn correctionTurn, value string) []string {
	if turn.Failure != "" {
		return []string{turn.Label + ": turn/writeback failure: " + turn.Failure}
	}
	if correctionHas(turn.After.Active, value) {
		return nil
	}
	for _, event := range turn.Events {
		var e struct {
			Kind string
			Ops  []struct{ Value string }
		}
		if json.Unmarshal(event, &e) != nil || e.Kind != "writeback" {
			continue
		}
		for _, op := range e.Ops {
			if strings.Contains(op.Value, value) {
				return []string{turn.Label + ": store outcome missing applied value " + value + "; inspect subsequent operations"}
			}
		}
	}
	return []string{turn.Label + ": write declaration omitted intended value " + value}
}

func correctionRun(t *testing.T, binary string, proxy *correctionProxy, endpoint, dir string) []correctionJudgment {
	t.Helper()
	root := t.TempDir()
	var rows []correctionJudgment
	p := correctionStart(t, binary, root, endpoint, filepath.Join(dir, "prepare"))
	var plantFailures []string
	for i, prompt := range correctionPlants {
		turn := correctionRunTurn(t, p, proxy, root, dir, fmt.Sprintf("plant-%d", i), prompt)
		value := []string{"LARCH-731", "CEDAR-214", "QUARTZ-806", "BASALT-592"}[i]
		plantFailures = append(plantFailures, correctionPlantFailures(turn, value)...)
	}
	for phase := 0; phase < 2; phase++ {
		current, retired := "BASALT-592", "CEDAR-214"
		if phase == 1 {
			p = correctionStart(t, binary, root, endpoint, filepath.Join(dir, "reversal"))
			turn := correctionRunTurn(t, p, proxy, root, dir, "reversal", correctionReversal)
			plantFailures = append(plantFailures, correctionPlantFailures(turn, "CEDAR-214")...)
			current, retired = retired, current
		}
		premise := append(append([]string(nil), plantFailures...), correctionPremise(correctionRead(t, root), current, retired)...)
		for i := 0; i < correctionFillers; i++ {
			turn := correctionRunTurn(t, p, proxy, root, dir, fmt.Sprintf("phase-%d/filler-%02d", phase, i), fmt.Sprintf("Transient check %d: reply OK. This is not a durable fact.", i))
			if turn.Failure != "" {
				premise = append(premise, "filler turn failed")
				break
			}
		}
		premise = append(premise, correctionPremise(correctionRead(t, root), current, retired)...)
		probe := correctionProbes[2]
		if phase == 1 {
			probe = correctionProbes[0]
			probe.Value = current
		}
		frozen := filepath.Join(dir, fmt.Sprintf("phase-%d/frozen", phase))
		correctionSnapshot(t, root, frozen)
		label := fmt.Sprintf("phase-%d/compacted-%s", phase, probe.Name)
		turn := correctionRunTurn(t, p, proxy, root, dir, label, probe.Prompt)
		rows = append(rows, correctionJudge(turn, probe, premise))
		p.stop(t)
		correctionCopy(t, frozen, root)
		for _, probe := range correctionProbes {
			if probe.Name == "corrected" {
				probe.Value = current
			}
			probeRoot := t.TempDir()
			correctionCopy(t, frozen, probeRoot)
			label := fmt.Sprintf("phase-%d/restart-%s", phase, probe.Name)
			process := correctionStart(t, binary, probeRoot, endpoint, filepath.Join(dir, label))
			turn := correctionRunTurn(t, process, proxy, probeRoot, dir, label, probe.Prompt)
			require.NotEqual(t, p.command.Process.Pid, process.command.Process.Pid)
			row := correctionJudge(turn, probe, premise)
			rows = append(rows, row)
			process.stop(t)
		}
	}
	correctionJSON(t, filepath.Join(dir, "judgments.json"), rows)
	return rows
}

func correctionSource(t *testing.T, binary, dir string) {
	t.Helper()
	command := exec.Command("git", "rev-parse", "HEAD")
	output, err := command.Output()
	require.NoError(t, err)
	hashes := map[string]string{}
	for _, path := range []string{"tests/memory_correction_test.go", "tests/memory_correction_controls_test.go"} {
		body, err := os.ReadFile(filepath.Join("..", path))
		require.NoError(t, err)
		hashes[path] = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	body, err := os.ReadFile(binary)
	require.NoError(t, err)
	correctionJSON(t, filepath.Join(dir, "source.json"), map[string]any{"revision": strings.TrimSpace(string(output)), "instrument_sha256": hashes, "binary_sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "plants": correctionPlants, "reversal": correctionReversal, "probes": correctionProbes, "fillers_per_phase": correctionFillers, "case_count": 10, "model": "MiniMax-M2", "call_ceiling": correctionCalls, "usd_ceiling": correctionUSD, "max_output_tokens": correctionOutput, "input_usd_per_million": 0.30, "output_usd_per_million": 1.20})
}

func TestMemoryCorrection_Paid(t *testing.T) {
	if os.Getenv("GAGENT_CORRECTION_PAID") != "1" {
		t.Skip("PM-only paid measurement is disabled")
	}
	key := os.Getenv("MINIMAX_KEY")
	require.NotEmpty(t, key)
	dir := os.Getenv("GAGENT_CORRECTION_RESULTS")
	require.NotEmpty(t, dir)
	require.NoError(t, os.Mkdir(dir, 0700))
	command := exec.Command("git", "status", "--porcelain", "--untracked-files=all", "--", "cmd", "internal", "tools", "go.mod", "go.sum", "tests/memory_correction_test.go", "tests/memory_correction_controls_test.go")
	command.Dir = ".."
	output, err := command.Output()
	require.NoError(t, err)
	require.Empty(t, string(output), "freeze source before paid measurement")
	binary := correctionBuild(t, "")
	correctionSource(t, binary, dir)
	proxy := &correctionProxy{upstream: "https://api.minimax.io/v1", key: key}
	server := httptest.NewServer(proxy)
	defer server.Close()
	t.Cleanup(func() { correctionJSON(t, filepath.Join(dir, "calls.json"), proxy.calls) })
	rows := correctionRun(t, binary, proxy, server.URL, dir)
	t.Logf("recorded %d cases; semantic answer adjudication remains PM-owned", len(rows))
}
