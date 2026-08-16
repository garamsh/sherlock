// Command harness measures a memory structure over a simulated decade.
//
// It builds a ten-year workload of sessions and turns with known ground
// truth, runs every structure behind one seam over it, and asks every
// planted fact at the end of every year. The structure this project ships —
// internal/memory's store, hybrid search, ranking function and after-turn
// lifecycle — is one of the structures behind that seam rather than the
// thing the harness is wired to, so the designs that come next can be
// measured against it without the harness changing.
//
// The whole run is offline on the shipped hashing embedder, calls no model,
// and costs nothing:
//
//	go run ./docs/experiments/memory-frames/harness -seed 7
//
// The -render flag rewrites the record's tables from the committed results
// instead of measuring, which is how every figure in ../README.md got
// there.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
)

// embedderName is the embedder every figure is produced under: the
// deterministic offline one cmd/gagent defaults to. Issue #435 measured
// what the hosted alternative costs the read path and which conclusions may
// be quoted across the two; this record does not repeat that measurement.
const embedderName = "hashing"

// Config is what the shipped frame ran under, read from the packages that
// declare it so the record cannot state a value the code has moved.
type Config struct {
	WeightVector           float64 `json:"weight_vector"`
	WeightFTS              float64 `json:"weight_fts"`
	RecencyHalfLifeHours   float64 `json:"recency_half_life_hours"`
	SupersessionSimilarity float64 `json:"supersession_similarity"`
	RetentionKeepSalience  float64 `json:"retention_keep_salience"`
	RetentionDropSalience  float64 `json:"retention_drop_salience"`
	RetentionWindowHours   float64 `json:"retention_window_hours"`
	ConsolidationThreshold int     `json:"consolidation_threshold"`
	SalienceAddEpisodic    float64 `json:"salience_add_episodic"`
	SalienceAddSemantic    float64 `json:"salience_add_semantic"`
	EmbeddingDim           int     `json:"embedding_dim"`
	DefaultCapacity        int     `json:"default_capacity"`
	RecallTokenBudget      int     `json:"default_recall_token_budget"`
	CharsPerToken          int     `json:"chars_per_token"`
	DocumentLimitBytes     int     `json:"document_limit_bytes"`
}

// Run is one seed's whole measurement.
type Run struct {
	Seed         int64          `json:"seed"`
	Start        string         `json:"start"`
	Embedder     string         `json:"embedder"`
	Digest       string         `json:"workload_digest"`
	Params       Params         `json:"params"`
	Config       Config         `json:"configuration"`
	Cost         Cost           `json:"cost"`
	Sweep        []SweepResult  `json:"declaration_error_sweep"`
	Declarer     *DeclarerScore `json:"declarer,omitempty"`
	Model        []ModelRun     `json:"model_runs,omitempty"`
	Bias         []BiasResult   `json:"join_bias,omitempty"`
	Stability    *Stability     `json:"stability,omitempty"`
	Disagreement *ModelRun      `json:"disagreement_run,omitempty"`
	Arity        Arity          `json:"arity"`
	Frames       []FrameResult  `json:"frames"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	seed := flag.Int64("seed", 7, "workload seed")
	out := flag.String("out", defaultOut(), "directory the results are written to")
	record := flag.String("record", defaultRecord(), "the record whose tables -render rewrites")
	render := flag.Bool("render", false, "rewrite the record's tables from the committed results and measure nothing")
	declare := flag.Bool("declare", false,
		"ask a model to declare the operation for every assertion. Spends money")
	resample := flag.Bool("resample", false,
		"ask the model the consequential assertions several times over, to see whether its answer is settled. Spends money")
	declarerCache := flag.String("declarer-cache", "",
		"file the declared operations are cached in, outside the repository")
	params := Params{}
	flag.IntVar(&params.Years, "years", 10, "the span the workload covers")
	flag.IntVar(&params.SessionsPerYear, "sessions-per-year", 36, "conversations in each year")
	flag.IntVar(&params.TurnsPerSession, "turns-per-session", 16, "turns in each conversation")
	flag.IntVar(&params.ThreadsPerClass, "threads-per-class", 40, "fact threads in each class")
	flag.Parse()

	resultPath := filepath.Join(*out, fmt.Sprintf("decade_seed%d.json", *seed))
	if *render {
		return renderRecord(resultPath, *record)
	}

	workload, err := GenerateWorkload(*seed, params)
	if err != nil {
		return fmt.Errorf("generate workload: %w", err)
	}
	if err := writeJSON(filepath.Join(*out, fmt.Sprintf("workload_seed%d.json", *seed)), workload); err != nil {
		return err
	}
	ctx := context.Background()
	declarationPath := filepath.Join(*out, fmt.Sprintf("declarations_seed%d.json", *seed))
	if *declare {
		declared, err := Declare(ctx, workload, *declarerCache)
		if err != nil {
			return err
		}
		if err := writeJSON(declarationPath, declared); err != nil {
			return err
		}
		fmt.Printf("declared %d assertions in %d paid calls: %d prompt and %d completion tokens, $%.4f\n",
			len(declared.Declarations), declared.Calls,
			declared.PromptTokens, declared.CompletionTokens, declared.CostUSD)
	}

	stabilityPath := filepath.Join(*out, fmt.Sprintf("stability_seed%d.json", *seed))
	if *resample {
		stable, err := Resample(ctx, workload, *declarerCache)
		if err != nil {
			return err
		}
		if err := writeJSON(stabilityPath, stable); err != nil {
			return err
		}
		fmt.Printf("resampled %d assertions %d times in %d paid calls: %d prompt and %d completion tokens, $%.4f\n",
			stable.Asked, stable.Samples, stable.Calls,
			stable.PromptTokens, stable.CompletionTokens, stable.CostUSD)
	}

	result, err := MeasureRun(ctx, workload)
	if err != nil {
		return err
	}
	declared, err := readCommittedDeclarations(declarationPath)
	if err != nil {
		return err
	}
	if declared != nil {
		scored := ScoreDeclarations(*declared)
		result.Declarer = &scored
		for _, frame := range []string{frameAttributeDeclared, frameIntervalDeclared} {
			modelRun, err := MeasureModelRun(ctx, workload, *declared, frame)
			if err != nil {
				return err
			}
			result.Model = append(result.Model, modelRun)
		}
		bias, err := MeasureJoinBias(ctx, workload, *declared)
		if err != nil {
			return err
		}
		result.Bias = bias
	}
	result.Stability, err = readCommittedStability(stabilityPath)
	if err != nil {
		return err
	}
	if result.Stability != nil && declared != nil {
		measured, err := Measure(ctx, workload, frameAttributeDeclared, armOracle,
			NewEmbedCounter(), result.Stability.Declaring(*declared))
		if err != nil {
			return err
		}
		run := ModelRun{Frame: frameAttributeDeclared, Model: result.Stability.Model, Years: measured.Years, Sizes: measured.Sizes}
		for _, direction := range directionNames {
			run.Damage = append(run.Damage, DirectionDamage{Direction: direction,
				Damage: MeasureDamage(declared.Flips(direction), measured.Probes, workload.Params.Years)})
		}
		result.Disagreement = &run
	}
	if err := writeJSON(resultPath, result); err != nil {
		return err
	}
	report(result)
	if err := renderRecord(resultPath, *record); err != nil {
		return err
	}
	fmt.Printf("\nrewrote the tables in %s\n", *record)
	return nil
}

// MeasureRun runs every frame in every arm over one workload and collects
// the run's cost alongside the results.
func MeasureRun(ctx context.Context, w Workload) (Run, error) {
	counter := NewEmbedCounter()
	result := Run{
		Seed:     w.Seed,
		Start:    w.Start,
		Embedder: embedderName,
		Digest:   w.TurnDigest,
		Params:   w.Params,
		Arity:    MeasureArity(w),
		Config: Config{
			WeightVector:           memory.WeightVector,
			WeightFTS:              memory.WeightFTS,
			RecencyHalfLifeHours:   memory.RecencyHalfLife.Hours(),
			SupersessionSimilarity: memory.SupersessionSimilarity,
			RetentionKeepSalience:  memory.RetentionKeepSalience,
			RetentionDropSalience:  memory.RetentionDropSalience,
			RetentionWindowHours:   memory.RetentionWindow.Hours(),
			ConsolidationThreshold: memory.ConsolidationThreshold,
			SalienceAddEpisodic:    gateway.SalienceAddEpisodic,
			SalienceAddSemantic:    gateway.SalienceAddSemantic,
			EmbeddingDim:           memory.EmbeddingDim,
			DefaultCapacity:        memory.DefaultCapacity,
			RecallTokenBudget:      memory.DefaultRecallTokenBudget,
			CharsPerToken:          memory.CharsPerToken,
			DocumentLimitBytes:     documentLimit,
		},
	}
	for _, arm := range armNames {
		for _, frame := range frameNames {
			measured, err := Measure(ctx, w, frame, arm, counter, nil)
			if err != nil {
				return Run{}, fmt.Errorf("%s in the %s arm: %w", frame, arm, err)
			}
			result.Frames = append(result.Frames, measured)
			fmt.Printf("%s, %s arm: %d probes over %d years\n", frame, arm, len(measured.Probes), len(measured.Years))
		}
	}
	sweep, err := MeasureSweep(ctx, w, w.Seed)
	if err != nil {
		return Run{}, err
	}
	result.Sweep = sweep
	fmt.Printf("declaration-error sweep: %d runs\n", len(sweep))

	calls, distinct, tokens := counter.Totals()
	result.Cost = Cost{
		TurnsObserved:    w.Params.Turns * len(armNames) * len(frameNames),
		ContextsProduced: w.Params.Probes * len(armNames) * len(frameNames),
		ModelCalls:       0,
		EmbedCalls:       calls,
		DistinctTexts:    distinct,
		EmbeddedTokens:   tokens,
		PerTurnModel:     ProjectPerTurnModel(w.Params.Turns, w.MeanTurnChars()),
	}
	return result, nil
}

// readCommittedDeclarations reads the answers a paid run committed. A tree
// without that file measures nothing about a model and spends nothing: the
// free path cannot reach the provider.
func readCommittedDeclarations(path string) (*Declarations, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var declared Declarations
	if err := json.Unmarshal(body, &declared); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &declared, nil
}

// readCommittedStability reads the resampled answers a paid run committed
// and scores them, which costs nothing.
func readCommittedStability(path string) (*Stability, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var stable Stability
	if err := json.Unmarshal(body, &stable); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	scored := ScoreStability(stable)
	return &scored, nil
}

// defaultOut is the results directory beside this harness.
func defaultOut() string {
	return filepath.Join("docs", "experiments", "memory-frames", "results")
}

// defaultRecord is the record this harness writes its tables into.
func defaultRecord() string {
	return filepath.Join("docs", "experiments", "memory-frames", "README.md")
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// turnDigest is a fingerprint of the whole turn stream. The workload file
// commits the threads rather than the thousands of turns they are scattered
// through, so this is what a determinism check compares.
func turnDigest(turns []Turn) string {
	h := sha256.New()
	for _, t := range turns {
		fmt.Fprintf(h, "%d|%d|%s|%s|", t.Index, t.Day, t.At.UTC().Format("2006-01-02T15:04:05Z"), t.Text)
		if t.Fact != nil {
			fmt.Fprintf(h, "%s|%s|%s", t.Fact.Subject, t.Fact.Attribute, t.Fact.Value)
		}
		fmt.Fprintln(h)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MeanTurnChars is the average turn length, which the cost projection needs
// and the record's limits read against.
func (w Workload) MeanTurnChars() int {
	if len(w.Turns) == 0 {
		return 0
	}
	var total int
	for _, t := range w.Turns {
		total += len(t.Text)
	}
	return total / len(w.Turns)
}

// report prints the per-year recall of every frame in every arm, which is
// what a run is read for while it is running.
func report(r Run) {
	fmt.Printf("\nseed %d, %s embedder, %d turns over %d years, %d of them carrying a fact\n",
		r.Seed, r.Embedder, r.Params.Turns, r.Params.Years, r.Params.FactTurns)
	for _, f := range r.Frames {
		fmt.Printf("\n%s, %s arm\n", f.Frame, f.Arm)
		fmt.Println("year  probes  recall            stale             context chars")
		for _, y := range f.Years {
			fmt.Printf("%4d  %6d  %.3f +/-%.3f  %.3f +/-%.3f  %8.1f\n",
				y.Year, y.Probes, y.Recall.Mean, y.Recall.Half, y.Stale.Mean, y.Stale.Half, y.ContextChars.Mean)
		}
	}
}
