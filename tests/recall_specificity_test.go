package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// The bound this run is refused past, stated here so it is read before
// the first request leaves the machine rather than off the invoice
// afterwards. Forty turns — ten plants and thirty probes — at the three
// model calls a turn makes when nothing goes wrong is 120 calls; the
// ceiling leaves room for a turn that takes an extra iteration and stops
// the act loop's own budget (agent.DefaultMaxIterations) from becoming
// the only limit. The rates are MiniMax's published MiniMax-M2 prices on
// the run date and are what the meter charged at, not a quote.
const (
	recallCallCeiling         = 150
	recallInputTokenCeiling   = 1_200_000
	recallOutputTokenCeiling  = 300_000
	recallUSDCeiling          = 0.29
	recallInputUSDPerMillion  = 0.30
	recallOutputUSDPerMillion = 1.20
)

// recallModel is the endpoint this experiment ran against. The provider
// value names the wire protocol and the base URL picks the vendor
// (ADR 0004).
const (
	recallProvider  = "openai-compatible"
	recallUpstream  = "https://api.minimax.io/v1"
	recallModelName = "MiniMax-M2"
	recallKeyEnv    = "MINIMAX_KEY"
)

// recallRunEnv opts a machine into the paid family. `make ci` never sets
// it, so a contributor's run and CI's cost nothing; a run without it
// skips rather than measuring a mock, because the mock model answers the
// same scripted turn to every question and a curve over it would be a
// curve over the script.
const recallRunEnv = "GAGENT_PAID_MODEL_TESTS"

// recallResultsDir is where the run writes what it bought, relative to
// the module root. The run creates it; the tree holds it once a run has
// produced results to put in it.
const recallResultsDir = "docs/experiments/recall-specificity/results"

// recallTurnCallGuard is how many model calls the first plant turn may
// take before the run stops. Three is the working shape — resolve the
// address, act, end on the writeback — and a turn past this has found a
// call pattern that is not working, which is reported rather than
// repeated forty times.
const recallTurnCallGuard = 6

// recallTurnTimeout bounds one turn end to end. A reasoning model
// answering three calls behind a 300-second per-call ceiling is the
// worst case this has to sit above.
const recallTurnTimeout = 15 * time.Minute

// recallProbe is one planted fact and the three questions asked of it.
// Every fact is a recent change about one person, so the same three
// question shapes fit all ten and only the specificity moves:
//
//   - L1 names the subject and the attribute.
//   - L2 names the subject and implies the attribute. It is one template
//     with the name substituted, so between L1 and L2 exactly one thing
//     changes.
//   - L3 implies both. It names the change rather than the person, and
//     each fact's change is unique in the planted set, so the question
//     has exactly one answer in the store.
//
// Value is the token the answer has to carry. The ten are distinct and
// none contains another, so the word-boundary match that scores them
// cannot credit one fact's value to another's probe.
type recallProbe struct {
	Subject   string
	Statement string
	Value     string
	L1        string
	L2        string
	L3        string
}

var recallProbes = []recallProbe{
	{
		Subject: "Sarah", Statement: "Sarah moved to Lisbon last month.", Value: "Lisbon",
		L1: "Where does Sarah live now?",
		L2: "Has anything changed for Sarah?",
		L3: "Didn't someone move to a new city recently?",
	},
	{
		Subject: "Marcus", Statement: "Marcus started at Kestrel Robotics in March.", Value: "Kestrel Robotics",
		L1: "Which company does Marcus work for now?",
		L2: "Has anything changed for Marcus?",
		L3: "Didn't someone start at a new company recently?",
	},
	{
		Subject: "Priya", Statement: "Priya switched her research to glaciology this year.", Value: "glaciology",
		L1: "What does Priya research now?",
		L2: "Has anything changed for Priya?",
		L3: "Didn't someone change their research field recently?",
	},
	{
		Subject: "Tomas", Statement: "Tomas took up the double bass in April.", Value: "double bass",
		L1: "Which instrument does Tomas play now?",
		L2: "Has anything changed for Tomas?",
		L3: "Didn't someone take up an instrument recently?",
	},
	{
		Subject: "Noor", Statement: "Noor adopted a greyhound in February.", Value: "greyhound",
		L1: "What pet does Noor have now?",
		L2: "Has anything changed for Noor?",
		L3: "Didn't someone adopt a pet recently?",
	},
	{
		Subject: "Elias", Statement: "Elias bought a Vanguard estate car in January.", Value: "Vanguard",
		L1: "What car does Elias drive now?",
		L2: "Has anything changed for Elias?",
		L3: "Didn't someone buy a new car recently?",
	},
	{
		Subject: "Hana", Statement: "Hana started learning Finnish in September.", Value: "Finnish",
		L1: "Which language is Hana learning now?",
		L2: "Has anything changed for Hana?",
		L3: "Didn't someone start learning a language recently?",
	},
	{
		Subject: "Rafael", Statement: "Rafael transferred to the Halcyon team in June.", Value: "Halcyon",
		L1: "Which team is Rafael on now?",
		L2: "Has anything changed for Rafael?",
		L3: "Didn't someone transfer to a different team recently?",
	},
	{
		Subject: "Ingrid", Statement: "Ingrid enrolled in a naval architecture course in August.", Value: "naval architecture",
		L1: "What is Ingrid studying now?",
		L2: "Has anything changed for Ingrid?",
		L3: "Didn't someone enrol in a course recently?",
	},
	{
		Subject: "Yusuf", Statement: "Yusuf started rowing with the Thameside club in May.", Value: "Thameside",
		L1: "Which rowing club did Yusuf join?",
		L2: "Has anything changed for Yusuf?",
		L3: "Didn't someone join a sports club recently?",
	},
}

// turnRecord is what one turn against the running agent produced, read
// off the monitoring stream (ADR 0007) rather than reconstructed.
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

// heldMemory is one row the store held after the plant phase, read
// through the shipped service rather than the file.
type heldMemory struct {
	ID          int64  `json:"id"`
	Subject     string `json:"subject"`
	Question    string `json:"question"`
	Restriction string `json:"restriction,omitempty"`
	Value       string `json:"value"`
}

// probeRow is one scored probe.
type probeRow struct {
	Level           string   `json:"level"`
	Subject         string   `json:"subject"`
	Question        string   `json:"question"`
	Value           string   `json:"value"`
	Placed          bool     `json:"placed"`
	RecallOutcome   string   `json:"recall_outcome"`
	ReadAddress     string   `json:"read_address"`
	RecalledCount   int      `json:"recalled_count"`
	PlantedInjected bool     `json:"planted_injected"`
	AnswerCarries   bool     `json:"answer_carries_value"`
	Response        string   `json:"response"`
	RecalledIDs     []string `json:"recalled_ids"`
	Failed          string   `json:"failed,omitempty"`
}

// TestRecallFromImpliedInput measures what the running agent recalls as
// the question says less: it plants ten facts through the real binary,
// one per turn, and asks each of them back at three levels of
// specificity against the store those turns wrote (issue #783).
//
// It is the experiment's run and not a guard on behaviour. It spends the
// operator's tokens, so it needs GAGENT_PAID_MODEL_TESTS and a key, and
// `make ci` sets neither. It writes every figure, every scored probe and
// every answer it bought into recallResultsDir, so the record written
// from them, and any later scoring of them, reads what the run bought
// rather than buying it again.
//
// Every turn is its own agent process against one memory file. A rolling
// session would let a probe answer out of the turn that planted the
// fact, which is the confound the whole question rests on: what is
// measured is what the store returns, not what the conversation
// remembers.
func TestRecallFromImpliedInput(t *testing.T) {
	if os.Getenv(recallRunEnv) != "1" {
		t.Skipf("paid run: set %s=1 and %s to measure against the live endpoint", recallRunEnv, recallKeyEnv)
	}
	key := os.Getenv(recallKeyEnv)
	require.NotEmpty(t, key, "%s must hold the endpoint's key", recallKeyEnv)

	meter := startSpendMeter(t, recallUpstream, key, spendCeiling{
		Calls:               recallCallCeiling,
		InputTokens:         recallInputTokenCeiling,
		OutputTokens:        recallOutputTokenCeiling,
		USD:                 recallUSDCeiling,
		InputUSDPerMillion:  recallInputUSDPerMillion,
		OutputUSDPerMillion: recallOutputUSDPerMillion,
	})
	resultsDir := filepath.Join(moduleRoot(t), recallResultsDir)
	require.NoError(t, os.MkdirAll(resultsDir, 0o755))
	runRecallSpecificity(t, meter, recallProbes, resultsDir)
}

// runRecallSpecificity is the run itself: plant, snapshot, probe at
// three levels, score, commit. It takes the meter rather than starting
// one so the same run can be driven against a scripted endpoint for
// nothing, which is what says the pattern works before it is paid for.
func runRecallSpecificity(t *testing.T, meter *spendMeter, probes []recallProbe, resultsDir string) {
	t.Helper()
	binary := buildGagentBinary(t)
	configPath := writeMeteredConfig(t, meter.BaseURL())
	_, workspaceAddr, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))

	t.Cleanup(func() {
		meter.WriteTranscript(t, filepath.Join(resultsDir, "calls_minimax-m2.json"))
		t.Logf("spend: %+v refusals: %v", meter.Spend(), meter.Refusals())
	})

	// Plant: one fact per turn, into one store.
	plantRoot := t.TempDir()
	plants := make([]turnRecord, 0, len(probes))
	for i, probe := range probes {
		label := fmt.Sprintf("plant/%s", probe.Subject)
		meter.Label(label)
		record := runRecallTurn(t, binary, plantRoot, workspaceAddr, configPath, probe.Statement)
		t.Logf("%s: outcome=%q ops=%d spend=%+v", label, record.RecallOutcome, len(record.Ops), meter.Spend())
		require.Empty(t, record.Failed, "plant turn %d (%s) failed", i, probe.Subject)
		plants = append(plants, record)
		if i == 0 {
			// The first turn is where a call pattern that does not work
			// shows: a provider answering in prose never reaches the
			// reserved writeback, and the act loop spends its whole
			// iteration budget finding that out (issue #615). One turn
			// is what this run is willing to pay to learn it.
			require.LessOrEqual(t, meter.CallsFor(label), recallTurnCallGuard,
				"the first plant turn took %d calls: the loop is not ending on the writeback, so the run stops here rather than repeating it forty times",
				meter.CallsFor(label))
			require.NotEmpty(t, readPlantedStore(t, plantRoot),
				"the first plant turn stored nothing: the write path is not reaching the store, and every probe below would be scored against an empty one")
		}
	}
	held := readPlantedStore(t, plantRoot)
	writeJSON(t, filepath.Join(resultsDir, "plants_minimax-m2.json"), map[string]any{
		"turns": plants,
		"held":  held,
	})

	// Probe: each level over the same planted store, from a copy of it,
	// so a probe's own writeback cannot reach the next probe's read.
	levels := []struct {
		name string
		ask  func(recallProbe) string
	}{
		{"L1", func(p recallProbe) string { return p.L1 }},
		{"L2", func(p recallProbe) string { return p.L2 }},
		{"L3", func(p recallProbe) string { return p.L3 }},
	}
	var rows []probeRow
	for _, level := range levels {
		for _, probe := range probes {
			root := t.TempDir()
			copyStore(t, plantRoot, root)
			label := fmt.Sprintf("%s/%s", level.name, probe.Subject)
			meter.Label(label)
			question := level.ask(probe)
			record := runRecallTurn(t, binary, root, workspaceAddr, configPath, question)
			row := scoreProbe(level.name, probe, question, record,
				plantedIDs(held, probe), meter.ReadAddress(label))
			t.Logf("%s: outcome=%q recalled=%d injected=%v carries=%v spend=%+v",
				label, row.RecallOutcome, row.RecalledCount, row.PlantedInjected,
				row.AnswerCarries, meter.Spend())
			rows = append(rows, row)
		}
	}
	writeJSON(t, filepath.Join(resultsDir, "probes_minimax-m2.json"), rows)
	writeSummary(t, filepath.Join(resultsDir, "summary.md"), rows, meter.Spend())
}

// TestRecallFromImpliedInput_AgainstAScriptedEndpoint runs the same
// procedure against an endpoint that answers from a script, so what the
// paid run depends on — a fresh agent process per turn, the planted
// store copied under each probe, the outcome read off the monitoring
// stream, and the scoring — is exercised for nothing before it is
// exercised for money. It measures no recall: the script answers the
// same address to every question.
func TestRecallFromImpliedInput_AgainstAScriptedEndpoint(t *testing.T) {
	// The agent resolves its key from the environment at startup
	// (ADR 0003) and refuses to start without one, whatever the endpoint
	// behind the base URL is. The scripted endpoint authenticates
	// nothing; what matters here is that a value reaches the meter.
	t.Setenv(recallKeyEnv, "scripted")
	probe := recallProbes[0]
	upstream := startScriptedEndpoint(t, scriptedAnswer{
		Subject:  probe.Subject,
		Question: "where does " + probe.Subject + " live",
		Value:    probe.Value,
		Reply:    probe.Subject + " lives in " + probe.Value + ".",
	})
	meter := startSpendMeter(t, upstream, "scripted", spendCeiling{
		Calls: recallCallCeiling, InputTokens: recallInputTokenCeiling,
		OutputTokens: recallOutputTokenCeiling, USD: recallUSDCeiling,
		InputUSDPerMillion: recallInputUSDPerMillion, OutputUSDPerMillion: recallOutputUSDPerMillion,
	})
	resultsDir := t.TempDir()

	runRecallSpecificity(t, meter, []recallProbe{probe}, resultsDir)

	var rows []probeRow
	body, err := os.ReadFile(filepath.Join(resultsDir, "probes_minimax-m2.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &rows))
	require.Len(t, rows, 3, "one probe at each of the three levels")
	for _, row := range rows {
		require.Empty(t, row.Failed)
		require.True(t, row.Placed, "%s: the scripted read step placed an address", row.Level)
		require.Equal(t, "addressed", row.RecallOutcome, row.Level)
		require.True(t, row.PlantedInjected, "%s: the planted memory was injected", row.Level)
		require.True(t, row.AnswerCarries, "%s: the reply carries the planted value", row.Level)
	}
	require.Empty(t, meter.Refusals(), "the scripted run stays inside the ceiling")
	require.Positive(t, meter.Spend().Calls)
}

// scriptedAnswer is what startScriptedEndpoint answers with: one address
// for every read step, and one writeback for every act step.
type scriptedAnswer struct {
	Subject  string
	Question string
	Value    string
	Reply    string
}

// startScriptedEndpoint serves the chat-completions protocol from a
// script and returns its base URL. It answers whichever reserved call
// the advertised tool set asks for, so a turn resolves an address and
// then ends on the writeback without a model.
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
		name, arguments := "memory_write", fmt.Sprintf(
			`{"reply":%s,"memory_ops":[{"address":{"subject":%s,"question":%s,"restriction":null},"value":%s,"operation":null}]}`,
			strconvQuote(answer.Reply), strconvQuote(answer.Subject),
			strconvQuote(answer.Question), strconvQuote(answer.Value))
		if len(request.Tools) == 1 && request.Tools[0].Function.Name == "memory_read" {
			name, arguments = "memory_read", fmt.Sprintf(`{"subject":%s,"question":%s}`,
				strconvQuote(answer.Subject), strconvQuote(answer.Question))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":%s,"arguments":%s}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`,
			strconvQuote(name), strconvQuote(arguments))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// writeMeteredConfig writes the agent config the run drives: the shipped
// MiniMax section with the base URL pointed at the meter, and a per-call
// ceiling wide enough for a reasoning model — a short one returns an
// empty answer under a true success (#615's neighbouring trap).
func writeMeteredConfig(t *testing.T, baseURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gagent.yaml")
	body := fmt.Sprintf("model:\n  provider: %s\n  base-url: %s\n  model: %s\n  api-key-env: %s\n  timeout: 300s\n",
		recallProvider, baseURL, recallModelName, recallKeyEnv)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// runRecallTurn runs one message through a freshly started agent and
// returns what the monitoring stream reported, then stops the process.
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
	resp := postJSON(t, baseURL+"/agents/"+binaryAgentID+"/messages",
		`{"content":`+strconvQuote(message)+`}`)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	record := turnRecord{Message: message}
	deadline := time.After(recallTurnTimeout)
	for done := false; !done; {
		select {
		case event, open := <-events:
			require.True(t, open, "event stream closed before the turn ended")
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

// readPlantedStore reads every active memory the plant phase left, at
// each planted subject, through the shipped service. Nothing is
// searched: the subjects are the ones the plants named, and what the
// store returns under them is what the writeback addressed.
func readPlantedStore(t *testing.T, root string) []heldMemory {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(root, "memory.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	active, err := store.Active(context.Background())
	require.NoError(t, err)
	out := make([]heldMemory, 0, len(active))
	for _, m := range active {
		out = append(out, heldMemory{
			ID: m.ID, Subject: m.Address.Subject, Question: m.Address.Question,
			Restriction: m.Address.Restriction, Value: m.Value,
		})
	}
	return out
}

// plantedIDs names the memories that carry a probe's value, which is
// what "the read step returned the planted fact" is scored against. A
// fact the plant turn wrote at two addresses has two.
func plantedIDs(held []heldMemory, probe recallProbe) []string {
	match := valuePattern(probe.Value)
	var out []string
	for _, m := range held {
		if match.MatchString(m.Value) {
			out = append(out, fmt.Sprintf("%d", m.ID))
		}
	}
	return out
}

// scoreProbe turns one turn's stream into the row the curve is drawn
// from. Placed is ADR 0019's distinction: every outcome but `unplaced`
// reached an address, whether or not anything was held there.
func scoreProbe(level string, probe recallProbe, question string, record turnRecord, planted []string, readAddress string) probeRow {
	injected := false
	for _, id := range record.RecalledIDs {
		for _, want := range planted {
			if id == want {
				injected = true
			}
		}
	}
	return probeRow{
		Level: level, Subject: probe.Subject, Question: question, Value: probe.Value,
		Placed:          record.RecallOutcome != "" && record.RecallOutcome != "unplaced",
		RecallOutcome:   record.RecallOutcome,
		ReadAddress:     readAddress,
		RecalledCount:   record.RecalledCount,
		PlantedInjected: injected,
		AnswerCarries:   valuePattern(probe.Value).MatchString(record.Response),
		Response:        record.Response,
		RecalledIDs:     record.RecalledIDs,
		Failed:          record.Failed,
	}
}

// valuePattern matches a planted value on word boundaries and without
// case, the rule #371 required after `fish` was credited to
// "allergic to shellfish".
func valuePattern(value string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(value) + `\b`)
}

// copyStore copies the planted memory file into a fresh root. The store
// runs in WAL mode, so its side files are copied where a run left them.
func copyStore(t *testing.T, from, to string) {
	t.Helper()
	for _, name := range []string{"memory.db", "memory.db-wal", "memory.db-shm"} {
		body, err := os.ReadFile(filepath.Join(from, name))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(to, name), body, 0o600))
	}
}

// writeSummary writes the tables the record quotes, so the prose and the
// result files cannot disagree about a figure.
func writeSummary(t *testing.T, path string, rows []probeRow, cost spend) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "| level | probes | placed | addressed | widened | empty | unplaced | planted fact injected | answer carries the value |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|\n")
	for _, level := range []string{"L1", "L2", "L3"} {
		var n, placed, injected, carried int
		outcomes := map[string]int{}
		for _, row := range rows {
			if row.Level != level {
				continue
			}
			n++
			outcomes[row.RecallOutcome]++
			if row.Placed {
				placed++
			}
			if row.PlantedInjected {
				injected++
			}
			if row.AnswerCarries {
				carried++
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n", level, n, placed,
			outcomes["addressed"], outcomes["widened"], outcomes["empty"], outcomes["unplaced"],
			injected, carried)
	}
	fmt.Fprintf(&b, "\ncalls %d, input tokens %d, output tokens %d, USD %.4f\n",
		cost.Calls, cost.InputTokens, cost.OutputTokens, cost.USD)
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	t.Log("\n" + b.String())
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o600))
}

func intField(v any) int {
	n, _ := v.(float64)
	return int(n)
}

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
