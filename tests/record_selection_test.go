package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const selectionFixture = "docs/experiments/recall-specificity/results/information-2026-09-21"

func selectionCases() []informationCase {
	var cases []informationCase
	for repetition := range 2 {
		for _, arm := range []string{"keys", "records"} {
			for _, c := range []informationCase{
				{Name: "explicit", Question: "What language did Hana start learning?"},
				{Name: "anonymous", Question: "Who started learning a language, and which language was it?"},
				{Name: "absent", Question: "Who won a fencing tournament?"},
				{Name: "club", Question: "Who joined a club?"},
			} {
				c.Name = fmt.Sprintf("%d/%s/%s", repetition+1, arm, c.Name)
				c.Arm = arm
				cases = append(cases, c)
			}
		}
	}
	for _, arm := range []string{"keys", "records"} {
		cases = append(cases, informationCase{Name: "control/" + arm + "/empty", Arm: arm, Empty: true, Question: "Who started learning a language, and which language was it?"})
	}
	return cases
}

func buildSelectionBinaries(t *testing.T, dir string) map[string]string {
	t.Helper()
	keys := buildInformationBinary(t, "grounded", dir)
	original := filepath.Join(moduleRoot(t), "internal/agent/memory/recall.go")
	source, err := os.ReadFile(filepath.Join(dir, "grounded-recall.go"))
	require.NoError(t, err)
	replacement, err := os.ReadFile(filepath.Join(moduleRoot(t), "tests/testdata/record-selection-recall.txt"))
	require.NoError(t, err)
	start := strings.Index(string(source), "// Recall implements")
	end := strings.Index(string(source), "// render turns")
	require.Greater(t, start, 0)
	require.Greater(t, end, start)
	changed := string(source[:start]) + string(replacement) + string(source[end:])
	changed = strings.Replace(changed, "import (", "import (\n\"bytes\"\n\"encoding/json\"\n\"io\"", 1)
	destination := filepath.Join(dir, "records-recall.go")
	require.NoError(t, os.WriteFile(destination, []byte(changed), 0600))
	overlay := filepath.Join(dir, "records-overlay.json")
	replacements := map[string]string{original: destination}
	for _, path := range []string{"internal/agent/service.go", "internal/gateway/runtime.go"} {
		replacements[filepath.Join(moduleRoot(t), path)] = filepath.Join(dir, "grounded-"+filepath.Base(path))
	}
	writeJSON(t, overlay, map[string]any{"Replace": replacements})
	for _, pair := range []struct{ name, before, after string }{
		{"keys-recall", original, filepath.Join(dir, "grounded-recall.go")},
		{"records-versus-keys", filepath.Join(dir, "grounded-recall.go"), destination},
		{"history", filepath.Join(moduleRoot(t), "internal/agent/service.go"), filepath.Join(dir, "grounded-service.go")},
		{"store-wiring", filepath.Join(moduleRoot(t), "internal/gateway/runtime.go"), filepath.Join(dir, "grounded-runtime.go")},
	} {
		diff := exec.Command("diff", "-u", pair.before, pair.after)
		output, err := diff.CombinedOutput()
		require.NotEmpty(t, output)
		require.Error(t, err)
		require.Equal(t, 1, diff.ProcessState.ExitCode(), "%s", output)
		require.NoError(t, os.WriteFile(filepath.Join(dir, pair.name+".diff"), output, 0600))
	}
	binary := filepath.Join(dir, "gagent-records")
	command := exec.Command("go", "build", "-overlay", overlay, "-o", binary, "./cmd/gagent")
	command.Dir = moduleRoot(t)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return map[string]string{"keys": keys, "records": binary}
}

func readSelectionFixture(t *testing.T) []heldMemory {
	t.Helper()
	root := filepath.Join(moduleRoot(t), selectionFixture)
	manifest, err := os.ReadFile(filepath.Join(root, "sha256.txt"))
	require.NoError(t, err)
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		require.Len(t, fields, 2)
		body, err := os.ReadFile(filepath.Join(root, fields[1]))
		require.NoError(t, err)
		require.Equal(t, fields[0], fmt.Sprintf("%x", sha256.Sum256(body)), fields[1])
	}
	frozen := t.TempDir()
	copyStore(t, filepath.Join(root, "frozen"), frozen)
	held := readPlantedStore(t, frozen)
	var published []heldMemory
	body, err := os.ReadFile(filepath.Join(root, "held.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &published))
	require.ElementsMatch(t, published, held)
	require.ElementsMatch(t, []heldMemory{
		{ID: 1, Subject: "Hana", Question: "when did Hana start learning Finnish", Value: "September"},
		{ID: 2, Subject: "Yusuf", Question: "when did Yusuf join the Thameside rowing club", Value: "May"},
		{ID: 3, Subject: "Mira", Question: "which club did Mira join", Restriction: "in June", Value: "the Hilltop chess club"},
	}, held, "published plant semantics must exist before measurement")
	return held
}

type selectionRow struct {
	Case                informationCase `json:"case"`
	HeldBefore          []heldMemory    `json:"held_before_probe"`
	Turn                turnRecord      `json:"turn"`
	Calls               []meteredCall   `json:"calls"`
	Selection           string          `json:"selection_status"`
	SelectedIDs         []string        `json:"selected_ids"`
	ExpectedIDs         []string        `json:"expected_ids"`
	SelectionComplete   bool            `json:"selection_complete"`
	EvidenceComplete    bool            `json:"evidence_complete"`
	ActExposure         string          `json:"act_exposure"`
	ActReached          bool            `json:"act_reached"`
	TaskFinished        bool            `json:"task_finished"`
	Invalid             []string        `json:"invalid_premises"`
	SemanticCorrectness string          `json:"semantic_correctness"`
	UnsupportedClaims   string          `json:"unsupported_claims"`
}

func selectionEvidenceComplete(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for _, id := range expected {
		if !slices.Contains(actual, id) {
			return false
		}
	}
	return true
}

func observeSelection(c informationCase, held []heldMemory, turn turnRecord, calls []meteredCall) selectionRow {
	row := selectionRow{Case: c, HeldBefore: held, Turn: turn, Selection: "missing response", SemanticCorrectness: "pending adjudication", UnsupportedClaims: "pending adjudication"}
	if !c.Empty && !strings.HasSuffix(c.Name, "/absent") {
		row.ExpectedIDs = []string{"1"}
		if strings.HasSuffix(c.Name, "/club") {
			row.ExpectedIDs = []string{"2", "3"}
		}
	}
	reads := 0
	for _, call := range calls {
		if call.Label != c.Name {
			continue
		}
		row.Calls = append(row.Calls, call)
		if call.Step == "act" {
			row.ActReached = true
			continue
		}
		reads++
		if call.Status != http.StatusOK || call.Refused != "" {
			row.Selection = "transport or spend failure"
			continue
		}
		if len(call.ToolCalls) == 0 {
			row.Selection = "prose without selection"
			continue
		}
		row.Selection = "invalid selection"
		if c.Arm == "records" {
			if len(call.ToolCalls) != 1 || call.ToolCalls[0].Name != "memory_read" {
				continue
			}
			var selection struct {
				IDs []int64 `json:"record_ids"`
			}
			decoder := json.NewDecoder(strings.NewReader(call.ToolCalls[0].Arguments))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&selection) != nil || selection.IDs == nil || decoder.Decode(new(any)) != io.EOF {
				continue
			}
			row.Selection = "empty selection"
			for _, id := range selection.IDs {
				found := slices.ContainsFunc(held, func(m heldMemory) bool { return m.ID == id })
				if !found {
					row.Selection = "unknown ID"
					break
				}
				if slices.Contains(row.SelectedIDs, fmt.Sprint(id)) {
					row.Selection = "duplicate ID"
					break
				}
				row.SelectedIDs = append(row.SelectedIDs, fmt.Sprint(id))
				row.Selection = "existing records"
			}
		} else {
			for _, tc := range call.ToolCalls {
				if tc.Name != "memory_read" {
					continue
				}
				var address struct{ Subject, Question string }
				if json.Unmarshal([]byte(tc.Arguments), &address) != nil {
					continue
				}
				row.Selection = "unoffered key"
				for _, m := range held {
					if m.Subject == address.Subject && m.Question == address.Question {
						row.SelectedIDs = append(row.SelectedIDs, fmt.Sprint(m.ID))
						row.Selection = "offered key"
					}
				}
				break
			}
		}
	}
	if row.Selection != "existing records" && row.Selection != "offered key" {
		row.SelectedIDs = nil
	}
	row.ActExposure = informationExposure(calls, c.Name, "act")
	for _, event := range turn.Events {
		if event["kind"] == "turn_finished" {
			row.TaskFinished = true
		}
	}
	row.SelectionComplete = (row.Selection == "existing records" || row.Selection == "offered key" || row.Selection == "empty selection" || (c.Arm == "keys" && row.Selection == "prose without selection")) && selectionEvidenceComplete(row.ExpectedIDs, row.SelectedIDs)
	row.EvidenceComplete = selectionEvidenceComplete(row.ExpectedIDs, turn.RecalledIDs)
	if reads != 1 {
		row.Invalid = append(row.Invalid, "expected one resolver request")
	}
	read := informationExposure(calls, c.Name, "read")
	if !strings.Contains(read, "Stored catalogue") {
		row.Invalid = append(row.Invalid, "missing catalogue")
	}
	for _, call := range row.Calls {
		if call.Step != "read" {
			continue
		}
		var request struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Parameters json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if json.Unmarshal(call.Request, &request) != nil || request.Model != recallModelName || len(request.Tools) != 1 {
			row.Invalid = append(row.Invalid, "incorrect model or selection tool")
			continue
		}
		hasIDs := bytes.Contains(request.Tools[0].Function.Parameters, []byte(`"record_ids"`))
		if hasIDs != (c.Arm == "records") {
			row.Invalid = append(row.Invalid, "wrong selection contract")
		}
	}
	for _, m := range held {
		line := fmt.Sprintf("%d | %s | %s | %s | %s", m.ID, m.Subject, m.Question, m.Restriction, m.Value)
		if !strings.Contains(read, line) {
			row.Invalid = append(row.Invalid, "catalogue omitted record")
		}
	}
	return row
}

func runSelectionProbe(t *testing.T, meter *spendMeter, binaries map[string]string, addr, config string, c informationCase, frozen []heldMemory) selectionRow {
	t.Helper()
	root := t.TempDir()
	if !c.Empty {
		copyStore(t, filepath.Join(moduleRoot(t), selectionFixture, "frozen"), root)
	}
	before := readPlantedStore(t, root)
	if c.Empty {
		require.Empty(t, before)
	} else {
		require.ElementsMatch(t, frozen, before, "pre-probe records changed")
	}
	meter.Label(c.Name)
	turn := runRecallTurn(t, binaries[c.Arm], root, addr, config, c.Question)
	return observeSelection(c, before, turn, informationCalls(meter))
}

func runSelectionComparison(t *testing.T, meter *spendMeter, dir string) []selectionRow {
	t.Helper()
	started := time.Now()
	rows := []selectionRow{}
	status := "partial"
	t.Cleanup(func() {
		meter.WriteTranscript(t, filepath.Join(dir, "calls.json"))
		writeJSON(t, filepath.Join(dir, "run.json"), map[string]any{"status": status, "elapsed_ms": time.Since(started).Milliseconds(), "spend": meter.Spend(), "refusals": meter.Refusals(), "probes": rows, "semantic_review": "pending"})
	})
	clean := exec.Command("git", "diff", "--exit-code", "HEAD", "--", "cmd", "internal", "tools", "go.mod", "go.sum")
	clean.Dir = moduleRoot(t)
	output, err := clean.CombinedOutput()
	require.NoError(t, err, "%s", output)
	frozen := readSelectionFixture(t)
	writeJSON(t, filepath.Join(dir, "held.json"), frozen)
	binaries := buildSelectionBinaries(t, dir)
	hashes := map[string]string{}
	for name, path := range binaries {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	revision := exec.Command("git", "rev-parse", "HEAD")
	revision.Dir = moduleRoot(t)
	sha, err := revision.Output()
	require.NoError(t, err)
	sources := map[string]string{}
	for _, path := range []string{"tests/record_selection_test.go", "tests/testdata/record-selection-recall.txt", "tests/recall_information_test.go", "tests/recall_specificity_test.go", "tests/model_spend_meter_test.go", "internal/agent/memory/recall.go", "internal/agent/service.go", "internal/gateway/runtime.go"} {
		body, err := os.ReadFile(filepath.Join(moduleRoot(t), path))
		require.NoError(t, err)
		sources[path] = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	writeJSON(t, filepath.Join(dir, "source.json"), map[string]any{"revision": strings.TrimSpace(string(sha)), "fixture": selectionFixture, "model": recallModelName, "binary_sha256": hashes, "source_sha256": sources, "variants": "keys: existing grounded overlay; records: same history and complete record rows, record_ids schema/instruction/validation and direct selected-record render; exact compiler inputs retained beside this file"})
	config := writeMeteredConfig(t, meter.BaseURL())
	for _, c := range selectionCases() {
		t.Run(c.Name, func(t *testing.T) {
			_, addr, _ := startWorkspaceBinary(t, binaries["keys"], filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))
			rows = append(rows, runSelectionProbe(t, meter, binaries, addr, config, c, frozen))
			writeJSON(t, filepath.Join(dir, "probes.json"), rows)
		})
	}
	require.Len(t, rows, 18)
	status = "collected; semantic review pending"
	for _, row := range rows {
		if len(row.Invalid) > 0 {
			status = "invalid premises; semantic review pending"
		}
	}
	return rows
}

func TestRecordSelection(t *testing.T) {
	if os.Getenv(recallRunEnv) != "1" {
		t.Skip("paid comparison requires explicit opt-in and instrument review")
	}
	dir := os.Getenv("GAGENT_SELECTION_RESULTS")
	require.NotEmpty(t, dir, "set a new results directory")
	require.True(t, filepath.IsAbs(dir), "use an absolute results directory")
	require.NoError(t, os.Mkdir(dir, 0700), "refuse to overwrite any run")
	key := os.Getenv(recallKeyEnv)
	require.NotEmpty(t, key)
	meter := startSpendMeter(t, recallUpstream, key, spendCeiling{Calls: recallCallCeiling, InputTokens: recallInputTokenCeiling, OutputTokens: recallOutputTokenCeiling, USD: recallUSDCeiling, InputUSDPerMillion: recallInputUSDPerMillion, OutputUSDPerMillion: recallOutputUSDPerMillion})
	runSelectionComparison(t, meter, dir)
}

func startSelectionEndpoint(t *testing.T, failure string) string {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name       string          `json:"name"`
					Parameters json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		all := ""
		for _, m := range request.Messages {
			all += m.Content + "\n"
		}
		name, args := "memory_write", `{"reply":"Unknown.","memory_ops":[]}`
		if len(request.Tools) == 1 && request.Tools[0].Function.Name == "memory_read" {
			records := strings.Contains(string(request.Tools[0].Function.Parameters), "record_ids")
			task, catalogue, _ := strings.Cut(request.Messages[len(request.Messages)-1].Content, "Stored catalogue")
			name = "memory_read"
			args = `{"subject":"Hana","question":"when did Hana start learning Finnish"}`
			if records {
				args = `{"record_ids":[1]}`
			}
			if strings.Contains(task, "joined a club") {
				args = `{"subject":"Mira","question":"which club did Mira join"}`
				if records {
					args = `{"record_ids":[2,3]}`
				}
			}
			if strings.Contains(task, "fencing") || !strings.Contains(catalogue, "1 | Hana") {
				if records {
					args = `{"record_ids":[]}`
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"No relevant record."}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
					return
				}
			}
			if records {
				switch failure {
				case "unknown":
					args = `{"record_ids":[1,999]}`
				case "duplicate":
					args = `{"record_ids":[1,1]}`
				case "null":
					args = `{"record_ids":null}`
				case "keys":
					args = `{"subject":"Hana","question":"when did Hana start learning Finnish"}`
				case "prose":
					fmt.Fprint(w, `{"choices":[{"message":{"content":"Hana learns Finnish."}}]}`)
					return
				case "transport":
					http.Error(w, `{"error":"scripted outage"}`, http.StatusServiceUnavailable)
					return
				}
			}
		} else {
			if strings.Contains(all, "Hana — when did Hana start learning Finnish") {
				args = `{"reply":"Hana started learning Finnish.","memory_ops":[]}`
			}
			if strings.Contains(all, "Yusuf —") && strings.Contains(all, "Mira —") {
				args = `{"reply":"Yusuf and Mira joined clubs.","memory_ops":[]}`
			} else if strings.Contains(all, "Mira —") {
				args = `{"reply":"Mira joined a club.","memory_ops":[]}`
			}
		}
		if name == "memory_write" {
			args = string(writebackStep(args).ToolCalls[0].Input)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`, name, args)
	}))
	t.Cleanup(endpoint.Close)
	return endpoint.URL
}

func TestRecordSelection_Free(t *testing.T) {
	t.Setenv(recallKeyEnv, "scripted")
	meter := startSpendMeter(t, startSelectionEndpoint(t, ""), "scripted", spendCeiling{Calls: 100, InputTokens: 100000, OutputTokens: 100000, USD: 1})
	dir := t.TempDir()
	if destination := os.Getenv("GAGENT_SELECTION_FREE_RESULTS"); destination != "" {
		require.NoError(t, os.Mkdir(destination, 0700))
		dir = destination
	}
	rows := runSelectionComparison(t, meter, dir)
	require.Len(t, rows, 18)
	for _, row := range rows {
		require.Empty(t, row.Invalid, row.Case.Name)
		require.Empty(t, row.Turn.Failed, row.Case.Name)
		require.True(t, row.ActReached, row.Case.Name)
		require.True(t, row.TaskFinished, row.Case.Name)
		require.Len(t, row.Calls, 2, row.Case.Name)
		require.NotEmpty(t, row.Calls[0].Request)
		require.NotEmpty(t, row.Calls[0].Response)
		require.NotContains(t, row.ActExposure, "Stored catalogue")
		for _, m := range row.HeldBefore {
			question := m.Question
			if m.Restriction != "" {
				question += " (" + m.Restriction + ")"
			}
			line := fmt.Sprintf("%s — %s: %s", m.Subject, question, m.Value)
			if slices.Contains(row.Turn.RecalledIDs, fmt.Sprint(m.ID)) {
				require.Contains(t, row.ActExposure, line)
			} else {
				require.NotContains(t, row.ActExposure, line)
			}
		}
		if row.Case.Arm == "records" {
			require.Contains(t, string(row.Calls[0].Request), "record_ids")
			require.NotContains(t, string(row.Calls[0].Request), "Copy the question verbatim")
			require.True(t, row.EvidenceComplete, row.Case.Name)
			require.True(t, row.SelectionComplete, row.Case.Name)
			if strings.HasSuffix(row.Case.Name, "/club") {
				require.ElementsMatch(t, []string{"2", "3"}, row.SelectedIDs)
				require.Contains(t, row.ActExposure, "Yusuf —")
				require.Contains(t, row.ActExposure, "Mira —")
			}
			if row.Case.Empty || strings.HasSuffix(row.Case.Name, "/absent") {
				require.Equal(t, "empty selection", row.Selection)
			}
		} else {
			require.Contains(t, string(row.Calls[0].Request), "Copy the question verbatim")
			require.NotContains(t, string(row.Calls[0].Request), "record_ids")
			if strings.HasSuffix(row.Case.Name, "/club") {
				require.False(t, row.EvidenceComplete, "one club actor is incomplete")
				require.ElementsMatch(t, []string{"3"}, row.Turn.RecalledIDs)
			}
		}
	}
	require.Empty(t, meter.Refusals())
}

func TestRecordSelection_RejectsInvalidSelections(t *testing.T) {
	t.Setenv(recallKeyEnv, "scripted")
	binaries := buildSelectionBinaries(t, t.TempDir())
	frozen := readSelectionFixture(t)
	for _, tc := range []struct{ name, status string }{
		{"", "existing records"}, {"unknown", "unknown ID"}, {"duplicate", "duplicate ID"}, {"null", "invalid selection"}, {"keys", "invalid selection"}, {"prose", "prose without selection"}, {"transport", "transport or spend failure"},
	} {
		name := tc.name
		if name == "" {
			name = "accepted"
		}
		t.Run(name, func(t *testing.T) {
			meter := startSpendMeter(t, startSelectionEndpoint(t, tc.name), "scripted", spendCeiling{Calls: 20, InputTokens: 100000, OutputTokens: 100000, USD: 1})
			_, addr, _ := startWorkspaceBinary(t, binaries["keys"], filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))
			row := runSelectionProbe(t, meter, binaries, addr, writeMeteredConfig(t, meter.BaseURL()), informationCase{Name: name, Arm: "records", Question: "What language did Hana start learning?"}, frozen)
			require.Equal(t, tc.status, row.Selection)
			require.NotEmpty(t, row.Calls)
			if tc.name == "" {
				require.Empty(t, row.Turn.Failed)
				require.True(t, row.EvidenceComplete)
				require.True(t, row.ActReached)
				require.True(t, row.TaskFinished)
			} else {
				require.NotEmpty(t, row.Turn.Failed)
				require.Empty(t, row.Turn.RecalledIDs, "reject the entire invalid selection")
				require.False(t, row.ActReached, "resolver prose must not count as act behavior")
				require.False(t, row.TaskFinished)
			}
		})
	}
}
