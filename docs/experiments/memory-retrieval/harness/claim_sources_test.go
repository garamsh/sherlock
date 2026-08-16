package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The claim register. ../README.md states figures; ../results/ and this
// package hold what produced them. Every figure in the record is read out
// of the record and compared with the committed value behind it, so the
// two cannot drift: change a published figure or a result file without
// the other and a check fails.
//
// Three registers, after harness/round4/claim_sources.py in the
// memory-creation experiment:
//
//	checks     a figure read out of the README and the value it must equal
//	unsourced  figures in the README that no committed file carries
//	corpus     the committed corpus against the generator that claims it
//
// Reads committed files only; makes no API call and needs no key.

const (
	readmePath = "../README.md"
	resultsDir = "../results"
	corpusPath = "../results/corpus_seed7.json"
)

type register struct {
	t      *testing.T
	readme string
	checks int
}

func newRegister(t *testing.T) *register {
	t.Helper()
	body, err := os.ReadFile(readmePath)
	require.NoError(t, err)
	return &register{t: t, readme: string(body)}
}

// anchor returns the capture groups of the one place the pattern matches
// the record. A pattern matching twice, or not at all, fails: the record
// has moved and the check no longer reads what it names.
func (r *register) anchor(pattern string) []string {
	r.t.Helper()
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(r.readme, -1)
	if len(matches) != 1 {
		r.t.Fatalf("anchor matched %d times, expected 1: %s", len(matches), pattern)
	}
	return matches[0][1:]
}

// report states how many figures the register read out of the record.
func (r *register) report() {
	r.t.Helper()
	r.t.Logf("%d figures read from README.md and compared to their committed source", r.checks)
}

// number compares one figure stated in the record with the value behind
// it, at the precision the record states it to.
func (r *register) number(claim, stated string, found float64) {
	r.t.Helper()
	r.checks++
	places := 0
	if dot := strings.IndexByte(stated, '.'); dot >= 0 {
		places = len(stated) - dot - 1
	}
	want, err := strconv.ParseFloat(strings.ReplaceAll(stated, ",", ""), 64)
	require.NoError(r.t, err, claim)
	assert.Equal(r.t, want, halfUp(found, places),
		"%s: the record says %s, its source holds %v", claim, stated, found)
}

// halfUp rounds away from zero, the direction a reader assumes.
func halfUp(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	rounded := math.Floor(math.Abs(value)*factor+0.5) / factor
	if value < 0 {
		return -rounded
	}
	return rounded
}

func loadRun(t *testing.T) Run {
	t.Helper()
	return loadRunFor(t, embedderHashing)
}

// loadRunFor reads the committed retrieval run for one embedder. Every
// result file names the embedder that produced it, so no figure can be
// read without knowing which one it came from.
func loadRunFor(t *testing.T, embedder string) Run {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(resultsDir, "retrieval_seed7_"+embedder+".json"))
	require.NoError(t, err)
	var run Run
	require.NoError(t, json.Unmarshal(body, &run))
	return run
}

func loadCorpus(t *testing.T) Corpus {
	t.Helper()
	body, err := os.ReadFile(corpusPath)
	require.NoError(t, err)
	var corpus Corpus
	require.NoError(t, json.Unmarshal(body, &corpus))
	return corpus
}

func TestCommittedCorpus_IsWhatTheGeneratorProduces(t *testing.T) {
	generated, err := GenerateCorpus(7, probeCount, storeSizes[len(storeSizes)-1], storeSizes[0])
	require.NoError(t, err)
	assert.Equal(t, generated, loadCorpus(t),
		"results/corpus_seed7.json is not what seed 7 generates; re-run the harness")
}

func TestCommittedFigures_AreWhatTheProbeRowsProduce(t *testing.T) {
	for _, arm := range loadRun(t).Arms {
		for _, size := range arm.Sizes {
			assert.Equal(t, Summarize(size), size.Figures,
				"%s at %d: the stored figures are not the ones its probe rows produce", arm.Name, size.Size)
		}
	}
}

func TestRecordMethod_MatchesItsCommittedSource(t *testing.T) {
	r := newRegister(t)
	corpus := loadCorpus(t)
	run := loadRun(t)

	a := r.anchor(`([\d,]+) facts over ([\d,]+) subject-topic pairs: (\d+) subjects, (\d+) topics, each pair holding (\d+) to (\d+) facts`)
	r.number("corpus facts", a[0], float64(len(corpus.Items)))
	r.number("subject-topic pairs", a[1], float64(len(pairsIn(corpus))))
	r.number("subjects", a[2], float64(len(subjects)))
	r.number("topics", a[3], float64(len(topics)))
	r.number("facts per pair, least", a[4], float64(minFacts))
	r.number("facts per pair, most", a[5], float64(maxFacts))

	a = r.anchor(`(\d+) of those pairs are probed\. Their ([\d,]+) facts`)
	r.number("probes", a[0], float64(len(corpus.Probes)))
	r.number("planted answers", a[1], float64(run.Arms[0].Sizes[0].RelevantPlanted))

	a = r.anchor(`all of them sit inside the first ([\d,]+) items`)
	r.number("smallest store", a[0], float64(storeSizes[0]))

	a = r.anchor(`derives from a writeback op — (\d+\.\d+), (\d+\.\d+), (\d+\.\d+) — and ages are drawn over a (\d+)-day window`)
	r.number("salience, episodic add", a[0], salienceAddEpisodic)
	r.number("salience, semantic add", a[1], salienceAddSemantic)
	r.number("salience, update", a[2], salienceUpdate)
	r.number("age window in days", a[3], float64(ageWindowHours)/24)

	a = r.anchor(`among the (\d+) planted values, (\w+) pairs contain each other`)
	r.number("planted values", a[0], float64(valueSlots()))
	r.number("substring pairs", numberWord(t, a[1]), float64(len(substringPairs())))

	a = r.anchor(`and (\d+) of the ([\d,]+) facts carry one of them`)
	r.number("facts carrying a trap value", a[0], float64(trapFacts(corpus)))
	r.number("facts in the trap statement", a[1], float64(len(corpus.Items)))

	a = r.anchor(`a mean over the (\d+) probes`)
	r.number("probes behind each figure", a[0], float64(run.Probes))

	a = r.anchor(`archives a new memory's most similar predecessor at cosine (\d+\.\d+) and above`)
	r.number("supersession threshold", a[0], run.Config.SupersessionSimilarity)

	a = r.anchor(`appears under (\d+) other topics and its topic under (\d+) other subjects`)
	r.number("topics a subject appears under", a[0], float64(len(topics)-1))
	r.number("subjects a topic appears under", a[1], float64(len(subjects)-1))
	r.report()
}

func TestRecordFigures_MatchTheirCommittedSource(t *testing.T) {
	r := newRegister(t)
	run := loadRun(t)

	// | `hashing` | `mixed-30d` | 1,000 | 0.030 ±0.013 | ... | 29.6 ±5.8 |
	row := regexp.MustCompile("(?m)^\\| `([a-z0-9-]+)` \\| `([a-z0-9-]+)` \\| ([\\d,]+) \\|(.*)\\|$")
	rows := row.FindAllStringSubmatch(r.readme, -1)
	require.Len(t, rows, len(measuredEmbedders)*len(run.Arms)*len(run.StoreSizes),
		"the score table lost or gained a row")

	for _, match := range rows {
		size, err := strconv.Atoi(strings.ReplaceAll(match[3], ",", ""))
		require.NoError(t, err)
		embedded := loadRunFor(t, match[1])
		require.Equal(t, match[1], embedded.Embedder, "a row states an embedder its file does not")
		figures := figuresFor(t, embedded, match[2], size)

		var cells []string
		for _, cell := range strings.Split(match[4], "|") {
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(cell, "*", "")))
		}
		named := []struct {
			name string
			fig  Figure
		}{
			{"recall@1", figures.RecallAt1}, {"precision@1", figures.PrecisionAt1},
			{"recall@5", figures.RecallAt5}, {"precision@5", figures.PrecisionAt5},
			{"recall@10", figures.RecallAtLimit}, {"precision@10", figures.PrecisionAtLimit},
			{"recall@budget", figures.RecallAtBudget}, {"precision@budget", figures.PrecisionAtBudget},
			{"returned@budget", figures.ReturnedAtBudget},
			{"best rank", figures.BestRank},
		}
		require.Len(t, cells, len(named), "%s %s at %d: the row lost or gained a column",
			match[1], match[2], size)
		for i, want := range named {
			stated := strings.SplitN(cells[i], " ±", 2)
			require.Len(t, stated, 2, "%s %s at %d: %s states no interval",
				match[1], match[2], size, want.name)
			claim := fmt.Sprintf("%s %s at %d, %s", match[1], match[2], size, want.name)
			r.number(claim, stated[0], want.fig.Mean)
			r.number(claim+" half-width", stated[1], want.fig.Half)
		}
	}
	r.report()
}

func TestRecordFindings_MatchTheirCommittedSource(t *testing.T) {
	r := newRegister(t)
	run := loadRun(t)
	corpus := loadCorpus(t)
	flat := figuresFor(t, run, armFlat, 9_000)
	mixed := figuresFor(t, run, armMixed30d, 9_000)
	small := figuresFor(t, run, armMixed30d, 1_000)

	openai := loadRunFor(t, embedderOpenAI)
	openaiMixed := figuresFor(t, openai, armMixed30d, 9_000)

	a := r.anchor(`0.037 of the answers under ` + "`hashing`" + ` and (\d+\.\d+) under ` +
		"`openai`" + `, where one returned memory in (\d+) and one in (\d+)`)
	r.number("openai recall@10 at 9,000", a[0], openaiMixed.RecallAtLimit.Mean)
	r.number("hashing returned memories per answer", a[1], 1/mixed.PrecisionAtLimit.Mean)
	r.number("openai returned memories per answer", a[2], 1/openaiMixed.PrecisionAtLimit.Mean)

	a = r.anchor(`the best answer sits at rank (\d+\.\d+) under ` + "`hashing`" + ` and (\d+\.\d+) under ` +
		"`openai`" + ` — the top (\d+\.\d+)% and (\d+\.\d+)% of the store, against about ([\d,]+) if the order were random — while ` +
		"`DefaultSearchLimit`" + ` shows the top (\d+\.\d+)%`)
	r.number("hashing best rank at the largest store", a[0], mixed.BestRank.Mean)
	r.number("openai best rank at the largest store", a[1], openaiMixed.BestRank.Mean)
	r.number("hashing best rank as a share of the store", a[2], 100*mixed.BestRank.Mean/9_000)
	r.number("openai best rank as a share of the store", a[3], 100*openaiMixed.BestRank.Mean/9_000)
	r.number("best rank under a random order", a[4], randomBestRank(corpus, 9_000))
	r.number("the search limit as a share of the store", a[5], 100*float64(run.Config.DefaultSearchLimit)/9_000)

	a = r.anchor(`average (\d+\.\d+) hours old under ` + "`hashing`" + ` and (\d+\.\d+) under ` + "`openai`" +
		`, against a store averaging (\d+), and carry salience (\d+\.\d+) and (\d+\.\d+) against the store's (\d+\.\d+)`)
	r.number("hashing age of the returned memories", a[0], mixed.ReturnedAge.Mean)
	r.number("openai age of the returned memories", a[1], openaiMixed.ReturnedAge.Mean)
	r.number("age of the store", a[2], sizeFor(t, run, armMixed30d, 9_000).StoreAgeHours)
	r.number("hashing salience of the returned memories", a[3], mixed.ReturnedSalience.Mean)
	r.number("openai salience of the returned memories", a[4], openaiMixed.ReturnedSalience.Mean)
	r.number("salience of the store", a[5], sizeFor(t, run, armMixed30d, 9_000).StoreSalience)

	a = r.anchor(`takes recall@10 at 1,000 items from (\d+\.\d+) to (\d+\.\d+) under ` + "`hashing`" +
		` and from (\d+\.\d+) to (\d+\.\d+) under ` + "`openai`")
	r.number("hashing recall@10 at 1,000, as generated", a[0], small.RecallAtLimit.Mean)
	r.number("hashing recall@10 at 1,000, stretched", a[1], figuresFor(t, run, armMixed360d, 1_000).RecallAtLimit.Mean)
	r.number("openai recall@10 at 1,000, as generated", a[2], figuresFor(t, openai, armMixed30d, 1_000).RecallAtLimit.Mean)
	r.number("openai recall@10 at 1,000, stretched", a[3], figuresFor(t, openai, armMixed360d, 1_000).RecallAtLimit.Mean)

	a = r.anchor(`the same stretch takes it from (\d+\.\d+) to (\d+\.\d+) and from (\d+\.\d+) to (\d+\.\d+)`)
	r.number("hashing recall@10 at 9,000, as generated", a[0], mixed.RecallAtLimit.Mean)
	r.number("hashing recall@10 at 9,000, stretched", a[1], figuresFor(t, run, armMixed360d, 9_000).RecallAtLimit.Mean)
	r.number("openai recall@10 at 9,000, as generated", a[2], openaiMixed.RecallAtLimit.Mean)
	r.number("openai recall@10 at 9,000, stretched", a[3], figuresFor(t, openai, armMixed360d, 9_000).RecallAtLimit.Mean)

	a = r.anchor(`falls from (\d+\.\d+) to (\d+\.\d+) between 1,000 and 9,000 items, which the intervals separate; its ` +
		"`flat`" + ` fall from (\d+\.\d+) to (\d+\.\d+)`)
	r.number("hashing mixed-30d recall@10 at 1,000", a[0], small.RecallAtLimit.Mean)
	r.number("hashing mixed-30d recall@10 at 9,000", a[1], mixed.RecallAtLimit.Mean)
	r.number("hashing flat recall@10 at 1,000", a[2], figuresFor(t, run, armFlat, 1_000).RecallAtLimit.Mean)
	r.number("hashing flat recall@10 at 9,000", a[3], flat.RecallAtLimit.Mean)
	assertSeparated(t, "hashing mixed-30d over store size", small.RecallAtLimit, mixed.RecallAtLimit, true)
	assertSeparated(t, "hashing flat over store size",
		figuresFor(t, run, armFlat, 1_000).RecallAtLimit, flat.RecallAtLimit, false)

	a = r.anchor(`precision@10 at 9,000 items is (\d+\.\d+) under ` + "`hashing`" + ` and (\d+\.\d+) under ` + "`openai`")
	r.number("hashing precision@10 at 9,000", a[0], mixed.PrecisionAtLimit.Mean)
	r.number("openai precision@10 at 9,000", a[1], openaiMixed.PrecisionAtLimit.Mean)

	a = r.anchor(`the relevance term is bounded by (\d+\.\d+) in total`)
	r.number("bound on the relevance term", a[0], run.Config.WeightVector+run.Config.WeightFTS)

	a = r.anchor(`facts averaging (\d+\.\d+) characters against a (\d+)-character token`)
	r.number("content length of the corpus", a[0], sizeFor(t, run, armMixed30d, 9_000).StoreContentChars)
	r.number("characters per token", a[1], float64(run.Config.CharsPerToken))

	a = r.anchor(`that is (\d+\.\d+) to (\d+\.\d+) memories a turn against the (\d+) a search returns`)
	low, high := budgetSpread(run, openai)
	r.number("smallest returned@budget", a[0], low)
	r.number("largest returned@budget", a[1], high)
	r.number("what a search returns", a[2], float64(run.Config.DefaultSearchLimit))

	a = r.anchor(`at 9,000 items ` + "`mixed-360d`" + ` goes from (\d+\.\d+) to (\d+\.\d+) under ` + "`hashing`" +
		` and from (\d+\.\d+) to (\d+\.\d+) under ` + "`openai`" + `, and ` + "`mixed-30d`" +
		` from (\d+\.\d+) to (\d+\.\d+) and from (\d+\.\d+) to (\d+\.\d+)`)
	hashingStretched := figuresFor(t, run, armMixed360d, 9_000)
	openaiStretched := figuresFor(t, openai, armMixed360d, 9_000)
	r.number("hashing mixed-360d recall@10 at 9,000", a[0], hashingStretched.RecallAtLimit.Mean)
	r.number("hashing mixed-360d recall@budget at 9,000", a[1], hashingStretched.RecallAtBudget.Mean)
	r.number("openai mixed-360d recall@10 at 9,000", a[2], openaiStretched.RecallAtLimit.Mean)
	r.number("openai mixed-360d recall@budget at 9,000", a[3], openaiStretched.RecallAtBudget.Mean)
	r.number("hashing mixed-30d recall@10 at 9,000", a[4], mixed.RecallAtLimit.Mean)
	r.number("hashing mixed-30d recall@budget at 9,000", a[5], mixed.RecallAtBudget.Mean)
	r.number("openai mixed-30d recall@10 at 9,000", a[6], openaiMixed.RecallAtLimit.Mean)
	r.number("openai mixed-30d recall@budget at 9,000", a[7], openaiMixed.RecallAtBudget.Mean)

	a = r.anchor(`` + "`flat`" + ` at 9,000 falls from (\d+\.\d+) to (\d+\.\d+) under ` + "`hashing`" +
		` and from (\d+\.\d+) to (\d+\.\d+) under ` + "`openai`" +
		` — because the same answers arrive among nearly (\w+) times as many memories`)
	openaiFlat := figuresFor(t, openai, armFlat, 9_000)
	r.number("hashing flat precision@10 at 9,000", a[0], flat.PrecisionAtLimit.Mean)
	r.number("hashing flat precision@budget at 9,000", a[1], flat.PrecisionAtBudget.Mean)
	r.number("openai flat precision@10 at 9,000", a[2], openaiFlat.PrecisionAtLimit.Mean)
	r.number("openai flat precision@budget at 9,000", a[3], openaiFlat.PrecisionAtBudget.Mean)
	r.number("returned@budget over the search limit", numberWord(t, a[4]),
		halfUp(flat.ReturnedAtBudget.Mean/float64(run.Config.DefaultSearchLimit), 0))

	a = r.anchor(`Facts are short single-clause statements averaging (\d+\.\d+) characters`)
	r.number("content length in Limits", a[0], sizeFor(t, run, armMixed30d, 9_000).StoreContentChars)

	// §What the embedder costs.
	a = r.anchor(`Its best answer sits at rank (\d+\.\d+) to (\d+\.\d+) of the whole store against ` +
		"`hashing`" + `'s (\d+\.\d+) to (\d+\.\d+), and at 1,000 items every probe's first result is a planted answer — precision@1 of (\d+\.\d+)`)
	r.number("openai flat best rank at 1,000", a[0], figuresFor(t, openai, armFlat, 1_000).BestRank.Mean)
	r.number("openai flat best rank at 9,000", a[1], figuresFor(t, openai, armFlat, 9_000).BestRank.Mean)
	r.number("hashing flat best rank at 1,000", a[2], figuresFor(t, run, armFlat, 1_000).BestRank.Mean)
	r.number("hashing flat best rank at 9,000", a[3], figuresFor(t, run, armFlat, 9_000).BestRank.Mean)
	r.number("openai flat precision@1 at 1,000", a[4], figuresFor(t, openai, armFlat, 1_000).PrecisionAt1.Mean)

	a = r.anchor(`recall@10 is (\d+\.\d+) against (\d+\.\d+) at 1,000 items, (\d+\.\d+) against (\d+\.\d+) at 4,000, and (\d+\.\d+) against (\d+\.\d+) at 9,000`)
	for i, size := range storeSizes {
		r.number(fmt.Sprintf("openai mixed-30d recall@10 at %d", size), a[2*i],
			figuresFor(t, openai, armMixed30d, size).RecallAtLimit.Mean)
		r.number(fmt.Sprintf("hashing mixed-30d recall@10 at %d", size), a[2*i+1],
			figuresFor(t, run, armMixed30d, size).RecallAtLimit.Mean)
		// The record's two central claims about the embedders are claims
		// about intervals: apart everywhere under flat, overlapping
		// everywhere under the planting the read path ships with.
		assertSeparated(t, fmt.Sprintf("the embedders under flat at %d", size),
			figuresFor(t, run, armFlat, size).RecallAtLimit,
			figuresFor(t, openai, armFlat, size).RecallAtLimit, true)
		assertSeparated(t, fmt.Sprintf("the embedders under mixed-30d at %d", size),
			figuresFor(t, run, armMixed30d, size).RecallAtLimit,
			figuresFor(t, openai, armMixed30d, size).RecallAtLimit, false)
	}

	a = r.anchor(`returns at 9,000 items average (\d+\.\d+) hours old with salience (\d+\.\d+), against ` +
		"`hashing`" + `'s (\d+\.\d+) hours and (\d+\.\d+)`)
	r.number("openai returned age", a[0], openaiMixed.ReturnedAge.Mean)
	r.number("openai returned salience", a[1], openaiMixed.ReturnedSalience.Mean)
	r.number("hashing returned age", a[2], mixed.ReturnedAge.Mean)
	r.number("hashing returned salience", a[3], mixed.ReturnedSalience.Mean)

	a = r.anchor(`embedded ([\d,]+) distinct texts, at a published price of \$(\d+\.\d+) per million tokens`)
	r.number("distinct texts embedded", a[0], float64(distinctTextsEmbedded(t, corpus)))

	// §Which conclusions may be quoted across embedders.
	a = r.anchor(`That is (\d+) on the embedder that ships, a factor of (\d+)`)
	hashingRetired := familyNamed(t, loadSeparability(t, embedderHashing), "correction").Superseded
	openaiRetired := familyNamed(t, loadSeparability(t, embedderOpenAI512), "correction").Superseded
	r.number("corrections the shipped embedder retires", a[0], float64(hashingRetired))
	r.number("the factor between them", a[1], float64(openaiRetired)/float64(hashingRetired))

	a = r.anchor(`(\d+\.\d+) against (\d+\.\d+) recall@10 at 9,000 items, intervals apart at every size`)
	r.number("hashing flat recall@10 quoted", a[0], flat.RecallAtLimit.Mean)
	r.number("openai flat recall@10 quoted", a[1], figuresFor(t, openai, armFlat, 9_000).RecallAtLimit.Mean)

	a = r.anchor(`ten memories averaging under (\w+) hours old from a store averaging (\d+)`)
	hours, err := strconv.ParseFloat(numberWord(t, a[0]), 64)
	require.NoError(t, err)
	assert.Less(t, mixed.ReturnedAge.Mean, hours, "hashing returns older memories than the record says")
	assert.Less(t, openaiMixed.ReturnedAge.Mean, hours, "openai returns older memories than the record says")
	r.number("store age quoted", a[1], sizeFor(t, run, armMixed30d, 9_000).StoreAgeHours)

	r.report()

	// Figures in the record no committed file carries.
	r.anchor(`A full run is minutes of CPU`)
	t.Log("unsourced: [Limits] 'a full run is minutes of CPU' — no committed file records the runtime; " +
		"the harness prints it per size and writes nothing")
	r.anchor(`They are (\d+) MB for this corpus`)
	t.Log("unsourced: [Limits] 'they are 202 MB for this corpus' — the vector cache is written outside " +
		"the repository and nothing committed records its size")
	r.anchor(`a published price of \$0.02 per million tokens`)
	t.Log("unsourced: [What the embedder costs] the price per million tokens is the provider's, " +
		"not a measurement; no committed file carries a token count or a bill")
}

// assertSeparated checks a claim the record makes about two figures: that
// their 95% intervals do or do not overlap. A record that says "which the
// intervals separate" is stating a fact about the numbers, so the register
// checks it like any other.
// distinctTextsEmbedded is every text the openai run paid for: the
// corpus and its probes, plus the separability pair set.
func distinctTextsEmbedded(t *testing.T, corpus Corpus) int {
	t.Helper()
	set, err := LoadPairSet(filepath.Join(resultsDir, "separability_pairs_seed7.json"))
	require.NoError(t, err)
	texts := map[string]bool{}
	for _, text := range append(corpusTexts(corpus), pairTexts(set)...) {
		texts[text] = true
	}
	return len(texts)
}

func assertSeparated(t *testing.T, claim string, a, b Figure, want bool) {
	t.Helper()
	overlap := a.Mean-a.Half <= b.Mean+b.Half && b.Mean-b.Half <= a.Mean+a.Half
	assert.Equal(t, want, !overlap,
		"%s: [%.3f, %.3f] and [%.3f, %.3f]", claim,
		a.Mean-a.Half, a.Mean+a.Half, b.Mean-b.Half, b.Mean+b.Half)
}

// budgetSpread returns the smallest and largest returned@budget across
// every row of the runs, the range the record states as what a recall
// injects.
func budgetSpread(runs ...Run) (low, high float64) {
	low = math.Inf(1)
	for _, run := range runs {
		for _, arm := range run.Arms {
			for _, size := range arm.Sizes {
				low = math.Min(low, size.Figures.ReturnedAtBudget.Mean)
				high = math.Max(high, size.Figures.ReturnedAtBudget.Mean)
			}
		}
	}
	return low, high
}

func pairsIn(corpus Corpus) map[string]bool {
	out := map[string]bool{}
	for _, it := range corpus.Items {
		out[it.Subject+"/"+it.Topic] = true
	}
	return out
}

// valueSlots counts the values the topics offer, counting a value used by
// two topics twice.
func valueSlots() int {
	var n int
	for _, t := range topics {
		n += len(t.values)
	}
	return n
}

// substringPairs returns the planted values that contain another planted
// value, which is where a content match and the planted pair disagree.
func substringPairs() [][2]string {
	var values []string
	for _, t := range topics {
		values = append(values, t.values...)
	}
	var out [][2]string
	for _, a := range values {
		for _, b := range values {
			if a != b && strings.Contains(b, a) {
				out = append(out, [2]string{a, b})
			}
		}
	}
	return out
}

// trapFacts counts the planted facts stating a value that a substring
// rule confuses with another value.
func trapFacts(corpus Corpus) int {
	trapped := map[string]bool{}
	for _, pair := range substringPairs() {
		trapped[pair[0]], trapped[pair[1]] = true, true
	}
	var n int
	for _, it := range corpus.Items {
		for _, word := range strings.FieldsFunc(strings.ToLower(it.Content), func(r rune) bool {
			return r < 'a' || r > 'z'
		}) {
			if trapped[word] {
				n++
				break
			}
		}
	}
	return n
}

// randomBestRank is where the first planted answer would sit if the store
// were ordered at random: (size + 1) / (answers per probe + 1).
func randomBestRank(corpus Corpus, size int) float64 {
	answers := map[string]int{}
	for _, it := range corpus.Items[:size] {
		answers[it.Subject+"/"+it.Topic]++
	}
	var total float64
	for _, probe := range corpus.Probes {
		total += float64(answers[probe.Subject+"/"+probe.Topic])
	}
	return (float64(size) + 1) / (total/float64(len(corpus.Probes)) + 1)
}

func sizeFor(t *testing.T, run Run, arm string, size int) SizeResult {
	t.Helper()
	for _, a := range run.Arms {
		if a.Name != arm {
			continue
		}
		for _, s := range a.Sizes {
			if s.Size == size {
				return s
			}
		}
	}
	t.Fatalf("no committed result for %s at %d", arm, size)
	return SizeResult{}
}

func figuresFor(t *testing.T, run Run, arm string, size int) Figures {
	t.Helper()
	return Summarize(sizeFor(t, run, arm, size))
}

func numberWord(t *testing.T, word string) string {
	t.Helper()
	words := map[string]string{"three": "3", "four": "4", "five": "5", "six": "6",
		"seven": "7", "eight": "8", "nine": "9", "ten": "10", "seventeen": "17"}
	value, ok := words[strings.ToLower(word)]
	require.True(t, ok, "unspelled number %q", word)
	return value
}
