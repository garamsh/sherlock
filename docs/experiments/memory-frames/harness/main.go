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

// shippedConfig reads the configuration this record measures under out
// of the memory package, so a run records what the code held rather than
// what someone typed. A committed run keeps the values it read; the
// check that they are still the code's is a test, because reading them
// at run time only binds the record to the code on the day it ran.
func shippedConfig() Config {
	return Config{
		WeightVector:           memory.WeightVector,
		WeightFTS:              memory.WeightFTS,
		RecencyHalfLifeHours:   memory.RecencyHalfLife.Hours(),
		SupersessionSimilarity: memory.SupersessionSimilarity,
		RetentionKeepSalience:  memory.RetentionKeepSalience,
		RetentionDropSalience:  memory.RetentionDropSalience,
		RetentionWindowHours:   memory.RetentionWindow.Hours(),
		ConsolidationThreshold: memory.ConsolidationThreshold,
		SalienceAddEpisodic:    memory.SalienceAddEpisodic,
		SalienceAddSemantic:    memory.SalienceAddSemantic,
		EmbeddingDim:           memory.EmbeddingDim,
		DefaultCapacity:        memory.DefaultCapacity,
		RecallTokenBudget:      memory.DefaultRecallTokenBudget,
		CharsPerToken:          memory.CharsPerToken,
		DocumentLimitBytes:     documentLimit,
	}
}

// Run is one seed's whole measurement.
type Run struct {
	Seed        int64         `json:"seed"`
	Start       string        `json:"start"`
	Embedder    string        `json:"embedder"`
	Digest      string        `json:"workload_digest"`
	Params      Params        `json:"params"`
	Config      Config        `json:"configuration"`
	Cost        Cost          `json:"cost"`
	Sweep       []SweepResult `json:"declaration_error_sweep"`
	Constrained []Constrained `json:"constrained,omitempty"`
	Shapes      []ShapeRun    `json:"shapes,omitempty"`

	Declarer     *DeclarerScore     `json:"declarer,omitempty"`
	Addresser    *AddressScore      `json:"addresser,omitempty"`
	Prose        *ProseScore        `json:"prose,omitempty"`
	ProseBefore  *ProseScore        `json:"prose_before,omitempty"`
	Model        []ModelRun         `json:"model_runs,omitempty"`
	Bias         []BiasResult       `json:"join_bias,omitempty"`
	Stability    *Stability         `json:"stability,omitempty"`
	Disagreement *ModelRun          `json:"disagreement_run,omitempty"`
	Arity        Arity              `json:"arity"`
	Identity     Identity           `json:"identity"`
	Questions    QuestionAddressing `json:"question_addressing"`
	Frames       []FrameResult      `json:"frames"`

	// Finding is the study in which no frame is handed the address. It is
	// its own file for the reason the shapes are, and is folded in here only
	// to render.
	Finding *FindingStudy `json:"-"`
}

// runFiles are the files one seed's studies read and write. Their names
// differ only in the seed, so they are named in one place rather than
// rebuilt wherever one of them is opened.
type runFiles struct {
	workload     string
	declarations string
	addresses    string
	prose        string
	proseBefore  string
	stability    string
	resolutions  string
	decade       string
	constrained  string
	shapes       string
	finding      string
	record       string
}

// filesFor names every file of one seed's run.
func filesFor(out, record string, seed int64) runFiles {
	named := func(kind string) string {
		return filepath.Join(out, fmt.Sprintf("%s_seed%d.json", kind, seed))
	}
	return runFiles{
		workload: named("workload"), declarations: named("declarations"),
		addresses: named("addresses"), prose: named("prose"),
		proseBefore: named("prose_baseline"),
		stability:   named("stability"), resolutions: named("resolutions"),
		decade: named("decade"), constrained: named("constrained"),
		shapes: named("harder"), finding: named("finding"), record: record,
	}
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
	prose := flag.Bool("prose", false,
		"put every assertion through the shipped contract prose and the shipped response schema, "+
			"and every probe through a read step, to measure whether a fact lands where its question looks. Spends money")
	address := flag.Bool("address", false,
		"ask a model for the question every assertion answers, which is where a question-shaped address comes from. Spends money")
	harder := flag.Bool("harder", false,
		"run the conversation-shape study instead of the decade measurement")
	extract := flag.Bool("extract", false,
		"have a model read every message of every shape. Spends money")
	finding := flag.Bool("finding", false,
		"run the finding study, in which no frame is handed the address, instead of the decade measurement")
	resolve := flag.Bool("resolve", false,
		"have a model turn every question into an address. Spends money")
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

	files := filesFor(*out, *record, *seed)
	if *render {
		return renderRecord(files)
	}

	if *harder {
		return runHarder(ctx0(), *seed, files, *extract, *declarerCache)
	}
	if *finding {
		return runFinding(ctx0(), *seed, params, files, *resolve, *declarerCache)
	}

	workload, err := GenerateWorkload(*seed, params)
	if err != nil {
		return fmt.Errorf("generate workload: %w", err)
	}
	if err := writeJSON(files.workload, workload); err != nil {
		return err
	}
	ctx := context.Background()
	declarationPath := files.declarations
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

	if *prose {
		measured, err := Prose(workload, *declarerCache)
		if err != nil {
			return err
		}
		if err := writeJSON(files.prose, measured); err != nil {
			return err
		}
		fmt.Printf("put %d assertions and %d probes through the shipped prose in %d paid calls: %d prompt and %d completion tokens, $%.4f\n",
			len(measured.Writes), len(measured.Reads), measured.Calls,
			measured.PromptTokens, measured.CompletionTokens, measured.CostUSD)
	}

	if *address {
		addressed, err := AskToAddress(ctx, workload, *declarerCache)
		if err != nil {
			return err
		}
		if err := writeJSON(files.addresses, addressed); err != nil {
			return err
		}
		fmt.Printf("addressed %d assertions in %d paid calls: %d prompt and %d completion tokens, $%.4f\n",
			len(addressed.Addressed), addressed.Calls,
			addressed.PromptTokens, addressed.CompletionTokens, addressed.CostUSD)
	}

	stabilityPath := files.stability
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
		for _, frame := range []string{frameAttributeDeclared, frameIntervalDeclared, frameMem0} {
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
	// The constrained runs are their own file: they are a different
	// question asked of the same workload, and keeping them out of the
	// decade file is what keeps that file exactly what it was.
	if err := writeJSON(files.constrained, result.Constrained); err != nil {
		return err
	}
	unbounded := result
	unbounded.Constrained = nil
	if err := writeJSON(files.decade, unbounded); err != nil {
		return err
	}
	report(result)
	if err := renderRecord(files); err != nil {
		return err
	}
	fmt.Printf("\nrewrote the tables in %s\n", *record)
	return nil
}

// runFinding measures the finding study and writes it beside the decade run
// rather than into it. It is the same workload asked a different way, so
// its file says which figures compare to which and no row of the decade
// run's results moves.
func runFinding(ctx context.Context, seed int64, params Params, files runFiles,
	pay bool, cachePath string) error {
	workload, err := GenerateWorkload(seed, params)
	if err != nil {
		return fmt.Errorf("generate workload: %w", err)
	}
	if pay {
		resolved, err := Resolve(ctx, workload, cachePath)
		if err != nil {
			return err
		}
		if err := writeJSON(files.resolutions, resolved); err != nil {
			return err
		}
		fmt.Printf("resolved %d questions in %d paid calls: %d prompt and %d completion tokens, $%.4f\n",
			len(resolved.Resolutions), resolved.Calls,
			resolved.PromptTokens, resolved.CompletionTokens, resolved.CostUSD)
	}
	resolutions, err := readCommittedResolutions(files.resolutions)
	if err != nil {
		return err
	}
	var resolving Resolving
	if resolutions != nil {
		resolving = resolutions.Resolving()
	}
	study, err := MeasureFinding(ctx, workload, resolving)
	if err != nil {
		return err
	}
	if resolutions != nil {
		scored := ScoreResolutions(*resolutions)
		study.Resolution = &scored
	}
	if err := writeJSON(files.finding, study); err != nil {
		return err
	}
	return renderRecord(files)
}

// readCommittedResolutions reads the answers a paid run committed. A tree
// without that file measures no resolved form and spends nothing.
func readCommittedResolutions(path string) (*Resolutions, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var resolved Resolutions
	if err := json.Unmarshal(body, &resolved); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &resolved, nil
}

// MeasureRun runs every frame in every arm over one workload and collects
// the run's cost alongside the results.
func MeasureRun(ctx context.Context, w Workload) (Run, error) {
	counter := NewEmbedCounter()
	result := Run{
		Seed:      w.Seed,
		Start:     w.Start,
		Embedder:  embedderName,
		Digest:    w.TurnDigest,
		Params:    w.Params,
		Arity:     MeasureArity(w),
		Questions: MeasureQuestions(w),
		Config:    shippedConfig(),
	}
	identity, err := MeasureIdentity(ctx, w, counter)
	if err != nil {
		return Run{}, err
	}
	result.Identity = identity
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
	constrained, err := MeasureConstrained(ctx, w)
	if err != nil {
		return Run{}, err
	}
	result.Constrained = constrained
	fmt.Printf("constrained runs: %d\n", len(constrained))

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

// ctx0 is the run's context. The harness takes no cancellation of its own.
func ctx0() context.Context { return context.Background() }

// runHarder measures the conversation-shape study and writes it beside the
// decade run rather than into it: it is a different workload, so its figures
// are comparable to each other and not to that one.
func runHarder(ctx context.Context, seed int64, files runFiles, pay bool, cachePath string) error {
	extractions := map[string]Extraction{}
	for _, shape := range shapeNames {
		path := filepath.Join(filepath.Dir(files.shapes), fmt.Sprintf("extraction_%s_seed%d.json", shape, seed))
		if pay {
			w, err := GenerateWorkload(seed, harderParams(shape))
			if err != nil {
				return err
			}
			extracted, err := Extract(ctx, w, cachePath)
			if err != nil {
				return err
			}
			if err := writeJSON(path, extracted); err != nil {
				return err
			}
			fmt.Printf("%s: read %d messages in %d paid calls, %d prompt and %d completion tokens, $%.4f\n",
				shape, len(extracted.Messages), extracted.Calls,
				extracted.PromptTokens, extracted.CompletionTokens, extracted.CostUSD)
		}
		body, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var extracted Extraction
		if err := json.Unmarshal(body, &extracted); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		extractions[shape] = extracted
	}
	shapes, err := MeasureShapes(ctx, seed, extractions)
	if err != nil {
		return err
	}
	if err := writeJSON(files.shapes, shapes); err != nil {
		return err
	}
	fmt.Printf("measured %d shapes\n", len(shapes))
	return renderRecord(files)
}

// readCommittedProse reads the prose round a paid run committed and scores
// it, which costs nothing.
func readCommittedProse(path string) (*ProseScore, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var run ProseRun
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	scored := ScoreProse(run)
	return &scored, nil
}

// readCommittedAddresses reads the addresses a paid run committed and scores
// them against the rule's, which costs nothing.
func readCommittedAddresses(path string) (*AddressScore, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var addressed Addresses
	if err := json.Unmarshal(body, &addressed); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	scored := ScoreAddresses(addressed)
	return &scored, nil
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
		for _, written := range t.Written {
			fmt.Fprintf(h, "%s|%s|%s", written.Subject, written.Attribute, written.Value)
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
