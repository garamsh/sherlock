package tests

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/message"
)

var fileOperationTasks = []struct{ message, path, content string }{
	{"Read seed.txt and create copy.txt with exactly the same content, then read copy.txt and list the workspace to verify the result. Do not send external messages.", "copy.txt", "first line\n한글 second line\n"},
	{"Replace copy.txt with exactly replacement followed by a newline, then read it to verify the result. Do not send external messages.", "copy.txt", "replacement\n"},
	{"Create an empty file named empty.txt, then read it and list the workspace to verify that it exists and is empty. Do not send external messages.", "empty.txt", ""},
	{"Empty copy.txt while keeping the file, then read it to verify the result. Do not send external messages.", "copy.txt", ""},
}

type fileOperationRow struct {
	Invalid           []string          `json:"invalid"`
	Outbox            []message.Message `json:"outbox"`
	Repetition        int               `json:"repetition"`
	Stage             int               `json:"stage"`
	Turn              turnRecord        `json:"turn"`
	Before            map[string]string `json:"before"`
	After             map[string]string `json:"after"`
	ArtifactCorrect   bool              `json:"artifact_correct"`
	VerifiedRead      bool              `json:"verified_read"`
	VerifiedList      bool              `json:"verified_list"`
	SuccessfulWrites  int               `json:"successful_writes"`
	UnsuccessfulCalls int               `json:"unsuccessful_calls"`
	BudgetRefusals    int               `json:"budget_refusals"`
	CallsByTool       map[string]int    `json:"calls_by_tool"`
}

func scoreFileOperations(row *fileOperationRow, path, content string) {
	body, exists := row.After[path]
	row.ArtifactCorrect = exists && body == content
	row.CallsByTool = map[string]int{}
	for _, event := range row.Turn.Events {
		if event["kind"] != "tool_call" {
			continue
		}
		name, _ := event["tool_name"].(string)
		row.CallsByTool[name]++
		result, _ := event["tool_result"].(string)
		if event["tool_failed"] == true {
			row.UnsuccessfulCalls++
			if strings.Contains(result, "budget of") {
				row.BudgetRefusals++
			}
			continue
		}
		input, ok := event["tool_input"].(map[string]any)
		if !ok {
			continue
		}
		operation := strings.TrimPrefix(name, "file_")
		if name == "files" {
			operation, _ = input["operation"].(string)
		}
		if name != "files" && name != "file_read" && name != "file_write" && name != "file_list" {
			continue
		}
		var output struct {
			Content      *string
			BytesWritten *int `json:"bytes_written"`
			Entries      []struct {
				Name string
				Dir  bool
			}
		}
		if json.Unmarshal([]byte(result), &output) != nil {
			continue
		}
		switch operation {
		case "write":
			if input["path"] == path && input["content"] == content && output.BytesWritten != nil && *output.BytesWritten == len(content) {
				row.SuccessfulWrites++
			}
		case "read":
			if input["path"] == path && output.Content != nil && *output.Content == content && row.SuccessfulWrites > 0 {
				row.VerifiedRead = true
			}
		case "list":
			if input["path"] == "." && row.SuccessfulWrites > 0 {
				for _, entry := range output.Entries {
					if entry.Name == path && !entry.Dir {
						row.VerifiedList = true
					}
				}
			}
		}
	}
}

// TestFileOperations collects real-agent evidence; a completed collection is not a model-success verdict.
func TestFileOperations(t *testing.T) {
	if os.Getenv("GAGENT_FILE_OPERATIONS_PAID") != "1" {
		t.Skip("file operation collection requires separate explicit opt-in")
	}
	dir := os.Getenv("GAGENT_FILE_OPERATIONS_RESULTS")
	require.NotEmpty(t, dir)
	require.NoError(t, os.Mkdir(dir, 0700), "refuse to replace evidence")
	key := os.Getenv(recallKeyEnv)
	require.NotEmpty(t, key)
	meter := startSpendMeter(t, recallUpstream, key, spendCeiling{Calls: 96, InputTokens: 800_000, OutputTokens: 80_000, USD: .4, InputUSDPerMillion: recallInputUSDPerMillion, OutputUSDPerMillion: recallOutputUSDPerMillion})
	require.Len(t, runFileOperations(t, meter, dir), 8)
}

func runFileOperations(t *testing.T, meter *spendMeter, dir string) []fileOperationRow {
	t.Helper()
	command := exec.Command("git", "status", "--porcelain", "--", "cmd", "internal", "tools", "go.mod", "go.sum")
	command.Dir = moduleRoot(t)
	output, err := command.CombinedOutput()
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(string(output)), "product sources must match the pinned revision")
	command = exec.Command("git", "rev-parse", "HEAD")
	command.Dir = moduleRoot(t)
	revision, err := command.Output()
	require.NoError(t, err)
	binary := buildGagentBinary(t)
	instrument := os.Getenv("GAGENT_FILE_OPERATIONS_INSTRUMENT")
	if instrument == "" {
		instrument = filepath.Join(moduleRoot(t), "tests/file_operations_test.go")
	}
	hashes := map[string]string{}
	paths := []string{binary, instrument}
	for _, name := range []string{"model_spend_meter", "e2e_binary", "workspace", "recall_specificity", "continuation", "recall_information", "gateway"} {
		paths = append(paths, filepath.Join(moduleRoot(t), "tests/"+name+"_test.go"))
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		hashes[path] = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	writeJSON(t, filepath.Join(dir, "source.json"), map[string]any{"revision": strings.TrimSpace(string(revision)), "sha256": hashes, "model": recallModelName, "repetitions": 2, "budget_note": "six calls per tool per turn; split tools have separate allowances"})

	var rows []fileOperationRow
	t.Cleanup(func() {
		meter.WriteTranscript(t, filepath.Join(dir, "calls.json"))
		writeJSON(t, filepath.Join(dir, "run.json"), map[string]any{"rows": rows, "spend": meter.Spend(), "refusals": meter.Refusals(), "semantic_review": "pending; scripted boundary tests do not establish model behavior"})
	})
	for repetition := 1; repetition <= 2; repetition++ {
		t.Run(fmt.Sprint(repetition), func(t *testing.T) {
			root, workspace := t.TempDir(), filepath.Join(t.TempDir(), "workspace")
			continuationRestore(t, workspace, map[string]string{"seed.txt": fileOperationTasks[0].content})
			cmd, addr, exited := startWorkspaceBinary(t, binary, workspace, emptyConfig(t, "workspace.yaml"))
			config := writeMeteredConfig(t, meter.BaseURL())
			for stage, task := range fileOperationTasks {
				meter.Label(fmt.Sprintf("%d/%d", repetition, stage+1))
				row := fileOperationRow{Repetition: repetition, Stage: stage + 1, Before: continuationFiles(t, workspace)}
				row.Invalid = fileOperationPremise(stage, row.Before)
				row.Turn = runRecallTurn(t, binary, root, addr, config, task.message)
				row.After = continuationFiles(t, workspace)
				row.Outbox = outboxMessages(t, root)
				if len(row.Outbox) != 0 {
					row.Invalid = append(row.Invalid, "task used messaging")
				}
				if row.After["seed.txt"] != fileOperationTasks[0].content {
					row.Invalid = append(row.Invalid, "source file changed")
				}
				scoreFileOperations(&row, task.path, task.content)
				rows = append(rows, row)
				writeJSON(t, filepath.Join(dir, fmt.Sprintf("%d-%d.json", repetition, stage+1)), row)
			}
			continuationStop(t, cmd, exited, syscall.SIGTERM, dir)
		})
	}
	return rows
}

func TestFileOperations_RejectsMissingEvidence(t *testing.T) {
	for _, name := range []string{"files", "file_write"} {
		write := map[string]any{"kind": "tool_call", "tool_name": name, "tool_input": map[string]any{"operation": "write", "path": "empty.txt", "content": ""}, "tool_result": `{"bytes_written":0}`}
		readName, listName := "file_read", "file_list"
		if name == "files" {
			readName, listName = "files", "files"
		}
		read := map[string]any{"kind": "tool_call", "tool_name": readName, "tool_input": map[string]any{"operation": "read", "path": "empty.txt"}, "tool_result": `{"content":""}`}
		list := map[string]any{"kind": "tool_call", "tool_name": listName, "tool_input": map[string]any{"operation": "list", "path": "."}, "tool_result": `{"entries":[{"name":"empty.txt","dir":false}]}`}
		row := fileOperationRow{After: map[string]string{"empty.txt": ""}, Turn: turnRecord{Events: []map[string]any{write, read, list}}}
		scoreFileOperations(&row, "empty.txt", "")
		assert.True(t, row.ArtifactCorrect)
		assert.True(t, row.VerifiedRead)
		assert.True(t, row.VerifiedList)
		assert.Equal(t, 1, row.SuccessfulWrites)
		for _, events := range [][]map[string]any{nil, {read, list}, {read, write}, {write}, {write, map[string]any{"kind": "tool_call", "tool_name": readName, "tool_input": read["tool_input"], "tool_result": `{}`}}} {
			broken := fileOperationRow{After: map[string]string{}, Turn: turnRecord{Events: events}}
			scoreFileOperations(&broken, "empty.txt", "")
			assert.False(t, broken.ArtifactCorrect)
			assert.False(t, broken.VerifiedRead)
		}
	}
}

func TestFileOperations_Free(t *testing.T) {
	t.Setenv(recallKeyEnv, "scripted")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string }
			Tools    []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		name, arguments := "memory_read", `{"subject":"files","question":"content"}`
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "memory_read" {
			stage, count := -1, 0
			for _, message := range request.Messages {
				if message.Role == "user" {
					for i, task := range fileOperationTasks {
						if strings.Contains(message.Content, task.message) {
							stage = i
							count = 0
						}
					}
				}
				if message.Role == "tool" {
					count++
				}
			}
			if stage < 0 {
				http.Error(w, "task not found", 400)
				return
			}
			task := fileOperationTasks[stage]
			type call struct{ operation, path, content string }
			sequence := []call{{"write", task.path, task.content}, {"read", task.path, ""}}
			if stage == 0 {
				sequence = append([]call{{"read", "seed.txt", ""}}, sequence...)
			}
			if stage == 0 || stage == 2 {
				sequence = append(sequence, call{"list", ".", ""})
			}
			name, arguments = "memory_write", `{"reply":"Verified.","memory_ops":[]}`
			if count < len(sequence) {
				next := sequence[count]
				name = "file_" + next.operation
				input := map[string]any{"path": next.path}
				if next.operation == "write" {
					input["content"] = next.content
				}
				for _, descriptor := range request.Tools {
					if descriptor.Function.Name == "files" {
						name = "files"
						input["operation"] = next.operation
					}
				}
				body, _ := json.Marshal(input)
				arguments = string(body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`, name, arguments)
	}))
	defer endpoint.Close()
	meter := startSpendMeter(t, endpoint.URL, "scripted", spendCeiling{Calls: 96, InputTokens: 800_000, OutputTokens: 80_000, USD: .4})
	dir := t.TempDir()
	if destination := os.Getenv("GAGENT_FILE_OPERATIONS_FREE_RESULTS"); destination != "" {
		require.NoError(t, os.Mkdir(destination, 0700))
		dir = destination
	}
	rows := runFileOperations(t, meter, dir)
	require.Len(t, rows, 8)
	for _, row := range rows {
		assert.Empty(t, row.Turn.Failed)
		assert.Empty(t, row.Invalid)
		assert.Empty(t, row.Outbox)
		assert.True(t, row.ArtifactCorrect)
		assert.True(t, row.VerifiedRead)
		if row.Stage == 1 || row.Stage == 3 {
			assert.True(t, row.VerifiedList)
		}
		assert.Equal(t, 1, row.SuccessfulWrites)
		assert.Zero(t, row.UnsuccessfulCalls)
		assert.Zero(t, row.BudgetRefusals)
	}
	assert.Empty(t, meter.Refusals())
	assert.Positive(t, meter.Spend().Calls)
	assert.Zero(t, meter.Spend().USD)
}

func fileOperationPremise(stage int, before map[string]string) []string {
	task := fileOperationTasks[stage]
	content, exists := before[task.path]
	if stage == 0 || stage == 2 {
		if exists {
			return []string{"creation target already exists"}
		}
	} else {
		expected := fileOperationTasks[0].content
		if stage == 3 {
			expected = fileOperationTasks[1].content
		}
		if !exists || content != expected {
			return []string{"replacement target lacks expected nonempty content"}
		}
	}
	if before["seed.txt"] != fileOperationTasks[0].content {
		return []string{"source premise changed"}
	}
	return nil
}

func TestFileOperations_RequiresExistingReplacement(t *testing.T) {
	for _, stage := range []int{1, 3} {
		before := map[string]string{"seed.txt": fileOperationTasks[0].content}
		assert.NotEmpty(t, fileOperationPremise(stage, before))
		before["copy.txt"] = ""
		assert.NotEmpty(t, fileOperationPremise(stage, before))
		before["copy.txt"] = fileOperationTasks[0].content
		if stage == 3 {
			before["copy.txt"] = fileOperationTasks[1].content
		}
		assert.Empty(t, fileOperationPremise(stage, before))
	}
}
