package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
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
	resultPath = "../results/retrieval_seed7.json"
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
	body, err := os.ReadFile(resultPath)
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

	// | `arm` | 1,000 | 0.030 ±0.013 | ... | 29.6 ±5.8 |
	row := regexp.MustCompile("(?m)^\\| `([a-z0-9-]+)` \\| ([\\d,]+) \\|(.*)\\|$")
	rows := row.FindAllStringSubmatch(r.readme, -1)
	require.Len(t, rows, len(run.Arms)*len(run.StoreSizes), "the score table lost or gained a row")

	for _, match := range rows {
		size, err := strconv.Atoi(strings.ReplaceAll(match[2], ",", ""))
		require.NoError(t, err)
		figures := figuresFor(t, run, match[1], size)

		var cells []string
		for _, cell := range strings.Split(match[3], "|") {
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(cell, "*", "")))
		}
		named := []struct {
			name string
			fig  Figure
		}{
			{"recall@1", figures.RecallAt1}, {"precision@1", figures.PrecisionAt1},
			{"recall@5", figures.RecallAt5}, {"precision@5", figures.PrecisionAt5},
			{"recall@10", figures.RecallAtLimit}, {"precision@10", figures.PrecisionAtLimit},
			{"best rank", figures.BestRank},
		}
		require.Len(t, cells, len(named), "%s at %d: the row lost or gained a column", match[1], size)
		for i, want := range named {
			stated := strings.SplitN(cells[i], " ±", 2)
			require.Len(t, stated, 2, "%s at %d: %s states no interval", match[1], size, want.name)
			claim := fmt.Sprintf("%s at %d, %s", match[1], size, want.name)
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

	a := r.anchor(`where one returned memory in (\d+) is an answer`)
	r.number("returned memories per answer", a[0], 1/mixed.PrecisionAtLimit.Mean)

	a = r.anchor(`the best answer sits at rank (\d+) on average — the top (\d+\.\d+)% of the store, against about ([\d,]+) if the order were random — while ` +
		"`DefaultSearchLimit` shows the top (\\d+\\.\\d+)%")
	r.number("best rank at the largest store", a[0], mixed.BestRank.Mean)
	r.number("best rank as a share of the store", a[1], 100*mixed.BestRank.Mean/9_000)
	r.number("best rank under a random order", a[2], randomBestRank(corpus, 9_000))
	r.number("the search limit as a share of the store", a[3], 100*float64(run.Config.DefaultSearchLimit)/9_000)

	a = r.anchor(`average (\d+\.\d+) hours old against a store averaging (\d+), and carry salience (\d+\.\d+) against the store's (\d+\.\d+)`)
	r.number("age of the returned memories", a[0], mixed.ReturnedAge.Mean)
	r.number("age of the store", a[1], sizeFor(t, run, armMixed30d, 9_000).StoreAgeHours)
	r.number("salience of the returned memories", a[2], mixed.ReturnedSalience.Mean)
	r.number("salience of the store", a[3], sizeFor(t, run, armMixed30d, 9_000).StoreSalience)

	a = r.anchor(`takes recall@10 at 1,000 items from (\d+\.\d+) to (\d+\.\d+)`)
	r.number("recall@10 at 1,000, as generated", a[0], small.RecallAtLimit.Mean)
	r.number("recall@10 at 1,000, stretched", a[1], figuresFor(t, run, armMixed360d, 1_000).RecallAtLimit.Mean)

	a = r.anchor(`at 9,000 items enough is, and the same stretch takes it from (\d+\.\d+) to (\d+\.\d+)`)
	r.number("recall@10 at 9,000, as generated", a[0], mixed.RecallAtLimit.Mean)
	r.number("recall@10 at 9,000, stretched", a[1], figuresFor(t, run, armMixed360d, 9_000).RecallAtLimit.Mean)

	a = r.anchor(`` + "`flat`" + ` returns (\d+\.\d+) of the planted answers in the top ten at 1,000 items and (\d+\.\d+) at 9,000`)
	r.number("flat recall@10 at 1,000", a[0], figuresFor(t, run, armFlat, 1_000).RecallAtLimit.Mean)
	r.number("flat recall@10 at 9,000", a[1], flat.RecallAtLimit.Mean)

	a = r.anchor(`the relevance term is bounded by (\d+\.\d+) in total`)
	r.number("bound on the relevance term", a[0], run.Config.WeightVector+run.Config.WeightFTS)

	a = r.anchor(`only when the ten average more than (\d+\.\d+) characters. The corpus averages (\d+\.\d+)`)
	r.number("content length the budget would bind at", a[0],
		float64(run.Config.RecallTokenBudget*run.Config.CharsPerToken)/float64(run.Config.DefaultSearchLimit))
	r.number("content length of the corpus", a[1], sizeFor(t, run, armMixed30d, 9_000).StoreContentChars)

	a = r.anchor(`` + "`returned@budget`" + ` is exactly (\d+\.\d+) in all nine rows`)
	for _, arm := range run.Arms {
		for _, size := range arm.Sizes {
			r.number(fmt.Sprintf("returned@budget, %s at %d", arm.Name, size.Size), a[0],
				size.Figures.ReturnedAtBudget.Mean)
			assert.Equal(t, size.Figures.RecallAtLimit, size.Figures.RecallAtBudget,
				"%s at %d: recall@budget is not recall@10, so the record's claim has moved", arm.Name, size.Size)
		}
	}

	a = r.anchor(`Facts are short single-clause statements averaging (\d+\.\d+) characters`)
	r.number("content length in Limits", a[0], sizeFor(t, run, armMixed30d, 9_000).StoreContentChars)

	r.report()

	// One figure in the record no committed file carries.
	r.anchor(`A full run is minutes of CPU`)
	t.Log("unsourced: [Limits] 'a full run is minutes of CPU' — no committed file records the runtime; " +
		"the harness prints it per size and writes nothing")
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
		"seven": "7", "eight": "8", "nine": "9", "ten": "10"}
	value, ok := words[word]
	require.True(t, ok, "unspelled number %q", word)
	return value
}
