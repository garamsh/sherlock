package main

import (
	"fmt"
	"math/rand"

	"github.com/garamsh/gagent/internal/memory"
)

// subjects are the entities the planted facts are about. Each name is one
// token under the store's tokenizer, so a fact carries exactly one subject
// token and a task statement naming a subject selects that subject's facts
// and nothing else.
var subjects = []string{
	"Alma", "Otto", "Maria", "Kofi", "Lisa", "Nadia", "Tomas", "Ingrid",
	"Rafael", "Yuki", "Hassan", "Petra", "Emeka", "Sofia", "Anders", "Leila",
	"Bruno", "Chiara", "Dmitri", "Esther", "Farid", "Greta", "Hugo", "Iris",
	"Jonas", "Karin", "Luca", "Mateo", "Noor", "Olga", "Pablo", "Quinn",
	"Rosa", "Stefan", "Tariq", "Ulla", "Viktor", "Wanda", "Xavier", "Yara",
	"Zoltan", "Amara", "Bjorn", "Celia", "Dario", "Elif", "Fabio", "Gunnar",
	"Helena", "Idris", "Jana", "Kasper", "Linnea", "Milos", "Nuria", "Oskar",
	"Priya", "Rania", "Sanjay", "Tereza", "Ursula", "Vasco", "Wilma", "Yannis",
	"Zara", "Andrei", "Beatriz", "Cato", "Dora", "Erol", "Freya", "Goran",
	"Halima", "Ivo", "Josefa", "Kwame", "Lorna", "Magnus", "Nikos", "Orla",
	"Pilar", "Radu", "Selma", "Tomoko", "Ulrik", "Verena", "Willem", "Ximena",
	"Yusuf", "Zeynep",
}

// topic is one thing the store holds facts about. task is the statement
// the read path is queried with; fact is what the store holds. The two
// share the topic's head word and differ elsewhere, because the shipped
// recall step queries with the frame's task statement rather than with a
// memory's own wording.
type topic struct {
	name   string
	task   string
	fact   string
	values []string
}

// topics carry deliberate substring pairs among their values — fish and
// shellfish, nuts and walnuts, rowing and growing — so a credit rule that
// matched content by substring would score them wrong.
var topics = []topic{
	{
		name:   "allergy",
		task:   "Plan a dinner for %s that leaves out everything they are allergic to.",
		fact:   "%s is allergic to %s.",
		values: []string{"fish", "shellfish", "nuts", "walnuts", "dairy", "sesame", "soy", "mustard", "gluten", "celery"},
	},
	{
		name:   "language",
		task:   "Pick which language the reply to %s should be written in, of the ones they speak.",
		fact:   "%s speaks %s.",
		values: []string{"korean", "german", "portuguese", "japanese", "italian", "dutch", "swedish", "hindi", "polish", "arabic"},
	},
	{
		name:   "instrument",
		task:   "Book a rehearsal room for %s with space for every instrument they play.",
		fact:   "%s plays the %s.",
		values: []string{"cello", "drums", "trumpet", "piano", "harp", "flute", "bass", "violin", "oboe", "banjo"},
	},
	{
		name:   "commute",
		task:   "Work out when %s should set off, from how they travel to the office.",
		fact:   "%s travels to the office by %s.",
		values: []string{"tram", "bicycle", "ferry", "metro", "coach", "scooter", "taxi", "train", "car", "boat"},
	},
	{
		name:   "software",
		task:   "List the licences %s needs for the software they use at work.",
		fact:   "%s uses %s at work.",
		values: []string{"blender", "figma", "matlab", "photoshop", "premiere", "tableau", "autocad", "logic", "ableton", "sketch"},
	},
	{
		name:   "pet",
		task:   "Arrange care at home for the animals %s keeps.",
		fact:   "%s keeps a %s at home.",
		values: []string{"turtle", "parrot", "gecko", "terrier", "rabbit", "hamster", "goldfish", "chinchilla", "canary", "ferret"},
	},
	{
		name:   "city",
		task:   "Fill in the relocation form with every city %s has lived in.",
		fact:   "%s lived in %s.",
		values: []string{"lisbon", "osaka", "helsinki", "nairobi", "bogota", "krakow", "adelaide", "tbilisi", "quebec", "palermo"},
	},
	{
		name:   "cooking",
		task:   "Draw up a shopping list for what %s cooks at the weekend.",
		fact:   "%s cooks %s at the weekend.",
		values: []string{"paella", "ramen", "goulash", "risotto", "tagine", "dumplings", "chowder", "pierogi", "biryani", "moussaka"},
	},
	{
		name:   "training",
		task:   "Fit what %s trains twice a week into next week's calendar.",
		fact:   "%s trains %s twice a week.",
		values: []string{"judo", "rowing", "climbing", "fencing", "boxing", "pilates", "karate", "sprinting", "archery", "diving"},
	},
	{
		name:   "subscription",
		task:   "Total up what %s pays for every month.",
		fact:   "%s pays for %s every month.",
		values: []string{"newspaper", "gym", "storage", "podcast", "streaming", "coworking", "insurance", "parking", "cloud", "delivery"},
	},
	{
		name:   "project",
		task:   "Check where the projects %s owns have got to.",
		fact:   "%s owns the %s project.",
		values: []string{"atlas", "kestrel", "monsoon", "lantern", "quarry", "beacon", "harbour", "meridian", "thistle", "compass"},
	},
	{
		name:   "certificate",
		task:   "Update the staff profile with the certificates %s holds.",
		fact:   "%s holds a certificate in %s.",
		values: []string{"welding", "auditing", "surveying", "rigging", "brewing", "grafting", "diving", "piloting", "translating", "tuning"},
	},
	{
		name:   "drink",
		task:   "Stock the kitchen with what %s drinks in the morning.",
		fact:   "%s drinks %s in the morning.",
		values: []string{"espresso", "matcha", "kefir", "chicory", "horchata", "chai", "cider", "smoothie", "cocoa", "juice"},
	},
	{
		name:   "reading",
		task:   "Suggest a next title from what %s is reading.",
		fact:   "%s is reading %s.",
		values: []string{"biographies", "thrillers", "essays", "poetry", "manuals", "atlases", "comics", "letters", "sagas", "almanacs"},
	},
	{
		name:   "team",
		task:   "Route the approval to whichever team %s reports to.",
		fact:   "%s reports to the %s team.",
		values: []string{"platform", "billing", "design", "safety", "logistics", "research", "support", "legal", "tooling", "payroll"},
	},
	{
		name:   "hardware",
		task:   "Order the accessories that fit the machine %s works on.",
		fact:   "%s works on a %s.",
		values: []string{"thinkpad", "macbook", "chromebook", "workstation", "tablet", "handheld", "laptop", "desktop", "netbook", "terminal"},
	},
	{
		name:   "leave",
		task:   "Block out the calendar for the months %s takes leave.",
		fact:   "%s takes leave in %s.",
		values: []string{"march", "august", "november", "january", "june", "september", "february", "december", "april", "october"},
	},
	{
		name:   "degree",
		task:   "Fill in what %s studied at university.",
		fact:   "%s studied %s at university.",
		values: []string{"geology", "philosophy", "acoustics", "botany", "cartography", "hydrology", "linguistics", "metallurgy", "zoology", "astronomy"},
	},
	{
		name:   "desk",
		task:   "Assign a locker near where %s sits.",
		fact:   "%s sits on the %s floor.",
		values: []string{"ground", "third", "seventh", "mezzanine", "top", "fourth", "second", "fifth", "sixth", "basement"},
	},
	{
		name:   "groceries",
		task:   "Note which shops %s buys groceries from.",
		fact:   "%s buys groceries from %s.",
		values: []string{"marketstall", "cooperative", "delicatessen", "supermarket", "greengrocer", "bakery", "wholesaler", "farmshop", "butcher", "kiosk"},
	},
}

// Salience values are the ones the platform derives from a writeback op
// (internal/agent/memory/writeback.go); the corpus plants no others.
// They are read from the memory package rather than re-typed: the
// committed corpus carries the salience it planted, and the generator is
// checked against that file, so a shipped value that moves fails there
// instead of leaving this corpus quietly planting a salience the
// platform no longer assigns.
const (
	salienceAddEpisodic = memory.SalienceAddEpisodic
	salienceAddSemantic = memory.SalienceAddSemantic
	salienceUpdate      = memory.SalienceUpdate
)

// Corpus parameters. minFacts and maxFacts bound how many facts one
// subject-topic pair holds, so recall@k and precision@k differ at every k.
// ageWindowHours is the span the planted timestamps cover, counted back
// from the run's fixed now.
const (
	minFacts       = 3
	maxFacts       = 7
	ageWindowHours = 30 * 24
)

// Item is one memory to plant.
type Item struct {
	Content  string  `json:"content"`
	Subject  string  `json:"subject"`
	Topic    string  `json:"topic"`
	Type     string  `json:"type"`
	Salience float64 `json:"salience"`
	AgeHours int     `json:"age_hours"`
}

// Probe is one task statement the read path is queried with, together
// with the subject-topic pair whose planted facts answer it.
type Probe struct {
	Task    string `json:"task"`
	Subject string `json:"subject"`
	Topic   string `json:"topic"`
}

// Corpus is the planted store contents and the probes over them. Items is
// ordered so that any prefix at or above the smallest store size holds
// every probed fact: a store of size n is Items[:n].
type Corpus struct {
	Seed   int64   `json:"seed"`
	Probes []Probe `json:"probes"`
	Items  []Item  `json:"items"`
}

// Answers reports whether item is a planted answer to probe. Ground truth
// is the subject-topic pair the generator planted, never a match on
// content: two facts can share every word but the subject.
func (p Probe) Answers(it Item) bool {
	return it.Subject == p.Subject && it.Topic == p.Topic
}

// WithAgeScale returns the corpus with every planted age multiplied by
// factor, which stretches the window the timestamps cover without
// touching the facts, the probes, or the order.
func (c Corpus) WithAgeScale(factor int) Corpus {
	out := c
	out.Items = make([]Item, len(c.Items))
	copy(out.Items, c.Items)
	for i := range out.Items {
		out.Items[i].AgeHours *= factor
	}
	return out
}

// Flat returns the corpus planted at one salience and one timestamp. The
// ranking function then adds the same salience and the same recency to
// every memory, so the order it produces is the order of its relevance
// term alone.
func (c Corpus) Flat() Corpus {
	out := c
	out.Items = make([]Item, len(c.Items))
	copy(out.Items, c.Items)
	for i := range out.Items {
		out.Items[i].AgeHours = 0
		out.Items[i].Salience = salienceAddSemantic
		out.Items[i].Type = "semantic"
	}
	return out
}

// GenerateCorpus builds the corpus for one seed: probes probed pairs, and
// items items in total. It is deterministic in seed and in nothing else.
func GenerateCorpus(seed int64, probes, items, headSize int) (Corpus, error) {
	if probes <= 0 || items <= 0 || headSize <= 0 {
		return Corpus{}, fmt.Errorf("probes, items and headSize must be positive")
	}
	if probes > len(subjects)*len(topics) {
		return Corpus{}, fmt.Errorf("%d probes exceeds the %d subject-topic pairs available",
			probes, len(subjects)*len(topics))
	}
	rng := rand.New(rand.NewSource(seed))

	type pair struct {
		subject string
		topic   topic
	}
	pairs := make([]pair, 0, len(subjects)*len(topics))
	for _, s := range subjects {
		for _, t := range topics {
			pairs = append(pairs, pair{subject: s, topic: t})
		}
	}
	rng.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })

	corpus := Corpus{Seed: seed}
	var probed, filler []Item
	for i, p := range pairs {
		n := minFacts + rng.Intn(maxFacts-minFacts+1)
		offset := rng.Intn(len(p.topic.values))
		facts := make([]Item, 0, n)
		for j := 0; j < n; j++ {
			value := p.topic.values[(offset+j)%len(p.topic.values)]
			facts = append(facts, newItem(rng, p.subject, p.topic, value))
		}
		if i < probes {
			corpus.Probes = append(corpus.Probes, Probe{
				Task:    fmt.Sprintf(p.topic.task, p.subject),
				Subject: p.subject,
				Topic:   p.topic.name,
			})
			probed = append(probed, facts...)
			continue
		}
		if len(probed)+len(filler) >= items {
			break
		}
		filler = append(filler, facts...)
	}
	if len(probed)+len(filler) < items {
		return Corpus{}, fmt.Errorf("generated %d items, short of the %d requested",
			len(probed)+len(filler), items)
	}
	if len(probed) > headSize {
		return Corpus{}, fmt.Errorf("%d probed facts exceed the %d-item head", len(probed), headSize)
	}

	// The head is the smallest store measured; every probed fact sits in
	// it, shuffled among filler, so a larger store is the same store with
	// more distractors and not a different one.
	head := append(probed, filler[:headSize-len(probed)]...)
	rng.Shuffle(len(head), func(i, j int) { head[i], head[j] = head[j], head[i] })
	tail := filler[headSize-len(probed) : items-len(probed)]
	rng.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	corpus.Items = append(head, tail...)
	return corpus, nil
}

// newItem draws the fields the ranking function reads — type, salience,
// age — independently of whether the fact answers a probe, so the
// salience and recency terms carry no information about the answer.
func newItem(rng *rand.Rand, subject string, t topic, value string) Item {
	it := Item{
		Content:  fmt.Sprintf(t.fact, subject, value),
		Subject:  subject,
		Topic:    t.name,
		AgeHours: rng.Intn(ageWindowHours),
	}
	switch draw := rng.Float64(); {
	case draw < 0.45:
		it.Type, it.Salience = "episodic", salienceAddEpisodic
	case draw < 0.85:
		it.Type, it.Salience = "semantic", salienceAddSemantic
	case draw < 0.925:
		it.Type, it.Salience = "episodic", salienceUpdate
	default:
		it.Type, it.Salience = "semantic", salienceUpdate
	}
	return it
}
