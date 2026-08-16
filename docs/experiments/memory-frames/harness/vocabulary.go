package main

import (
	"fmt"
	"math/rand"
)

// attribute is one thing a subject can have a value for, with the sentences
// each kind of assertion is said in and the question the checkpoint asks.
//
// assert, also, narrow and replace all take the subject and the value.
// They differ only in what they say about the values already on the thread,
// and that difference lives in the wording alone: the extracted fact a
// frame receives in the oracle arm is the same shape in all four, so a
// frame that wants to know whether a value replaces or joins the ones
// before it has to decide, not look it up.
type attribute struct {
	name string
	// assert opens a thread.
	assert string
	// also adds a second value that stands beside the first.
	also string
	// narrow adds a value under a qualifier, which is true at the same time
	// as the value it narrows and reads almost identically to it.
	narrow string
	// replace ends the values before it without naming them. It names no
	// old value on purpose: a correction that repeats what it corrects
	// would plant the stale value in the text and the stale-return figure
	// would count the workload's own words.
	replace string
	// question is what the checkpoint asks. It names the subject and the
	// attribute and never the value.
	question string
}

// attributes are the twelve fact kinds the workload plants.
var attributes = []attribute{
	{
		name:     "project",
		assert:   "%s leads the %s project.",
		also:     "%s leads the %s project as well.",
		narrow:   "On Thursdays %s leads the %s project.",
		replace:  "%s leads the %s project from now on.",
		question: "Which project does %s lead?",
	},
	{
		name:     "clinic",
		assert:   "%s goes to the %s clinic.",
		also:     "%s goes to the %s clinic too.",
		narrow:   "For check-ups %s goes to the %s clinic.",
		replace:  "%s has moved to the %s clinic.",
		question: "Which clinic does %s go to?",
	},
	{
		name:     "line",
		assert:   "%s commutes on the %s line.",
		also:     "%s commutes on the %s line as well.",
		narrow:   "In winter %s commutes on the %s line.",
		replace:  "%s commutes on the %s line now.",
		question: "Which line does %s commute on?",
	},
	{
		name:     "accounts",
		assert:   "%s keeps the accounts in %s.",
		also:     "%s keeps the accounts in %s too.",
		narrow:   "For payroll %s keeps the accounts in %s.",
		replace:  "%s has switched the accounts to %s.",
		question: "What does %s keep the accounts in?",
	},
	{
		name:     "hall",
		assert:   "%s rehearses at the %s hall.",
		also:     "%s rehearses at the %s hall as well.",
		narrow:   "In summer %s rehearses at the %s hall.",
		replace:  "%s rehearses at the %s hall from now on.",
		question: "Where does %s rehearse?",
	},
	{
		name:     "supplier",
		assert:   "%s orders paper from %s.",
		also:     "%s orders paper from %s as well.",
		narrow:   "For headed paper %s orders from %s.",
		replace:  "%s orders paper from %s now.",
		question: "Who does %s order paper from?",
	},
	{
		name:     "cottage",
		assert:   "%s rents the %s cottage.",
		also:     "%s rents the %s cottage as well.",
		narrow:   "At new year %s rents the %s cottage.",
		replace:  "%s rents the %s cottage from now on.",
		question: "Which cottage does %s rent?",
	},
	{
		name:     "bank",
		assert:   "%s banks with %s.",
		also:     "%s banks with %s as well.",
		narrow:   "For the business account %s banks with %s.",
		replace:  "%s banks with %s now.",
		question: "Who does %s bank with?",
	},
	{
		name:     "course",
		assert:   "%s is taking the %s course.",
		also:     "%s is taking the %s course as well.",
		narrow:   "On alternate weeks %s takes the %s course.",
		replace:  "%s is taking the %s course instead.",
		question: "Which course is %s taking?",
	},
	{
		name:     "team",
		assert:   "%s sits with the %s team.",
		also:     "%s sits with the %s team as well.",
		narrow:   "On review days %s sits with the %s team.",
		replace:  "%s sits with the %s team from now on.",
		question: "Which team does %s sit with?",
	},
	{
		name:     "bakery",
		assert:   "%s buys bread at the %s bakery.",
		also:     "%s buys bread at the %s bakery as well.",
		narrow:   "At weekends %s buys bread at the %s bakery.",
		replace:  "%s buys bread at the %s bakery now.",
		question: "Which bakery does %s buy bread at?",
	},
	{
		name:     "backups",
		assert:   "%s keeps the backups on %s.",
		also:     "%s keeps the backups on %s as well.",
		narrow:   "For the archive %s keeps the backups on %s.",
		replace:  "%s keeps the backups on %s from now on.",
		question: "Where does %s keep the backups?",
	},
}

// subjects are the people the facts are about. One subject holds threads
// under several attributes, so naming a subject never selects a fact on its
// own and naming an attribute never does either.
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
}

// Every planted value is one of these two words joined by a hyphen, and no
// pair is planted twice. A value is therefore a token that identifies the
// assertion that carries it, which is what lets a returned context be
// credited by reading it: the trap #371 found — crediting "fish" to
// "shellfish" — cannot arise when the thing looked for is a pair no other
// fact uses. The cost of that choice is stated in the record's limits.
var (
	valueFirst = []string{
		"amber", "copper", "ironwood", "silverfin", "cobalt", "saffron", "marlow", "quill",
		"thistle", "harrow", "vellum", "garnet", "oakhill", "brindle", "cinder", "larkin",
		"mirefield", "nutmeg", "opal", "pewter", "ravel", "sable", "tamarind", "umber",
		"verdigris", "wicklow", "yarrow", "zephyr", "bramble", "dovetail", "ember", "flint",
	}
	valueSecond = []string{
		"quarry", "lantern", "annex", "ridge", "harbour", "meadow", "foundry", "spire",
		"hollow", "brook", "mill", "gable", "wharf", "coppice", "beacon", "chandler",
		"drift", "fenn", "glade", "hearth", "isle", "kiln", "ledge", "moor",
		"orchard", "pike", "reach", "sedge", "tarn", "vale", "weir", "yard",
	}
)

// allValues returns every value the vocabulary can plant, in a fixed order.
func allValues() []string {
	out := make([]string, 0, len(valueFirst)*len(valueSecond))
	for _, first := range valueFirst {
		for _, second := range valueSecond {
			out = append(out, first+"-"+second)
		}
	}
	return out
}

// noiseTemplates are the turns that carry no fact. They name subjects and
// everyday things, so the noise a probe has to be found among mentions the
// person the probe asks about — noise that never named anyone would be a
// distractor nothing distracts from.
var noiseTemplates = []string{
	"%s asked about the %s again.",
	"%s said nothing came of the %s.",
	"%s is running late; the %s can wait.",
	"Remind me to chase %s about the %s.",
	"%s says the %s is sorted.",
	"%s got the %s by mistake.",
	"%s wants a word about the %s at some point.",
	"Tell %s the %s is on the list.",
	"%s had a look at the %s and shrugged.",
	"%s has no news on the %s.",
	"%s mentioned the %s in passing.",
	"%s has the %s until the end of the week.",
	"%s is not worried about the %s.",
	"Someone should tell %s where the %s ended up.",
	"%s brought the %s up and then dropped it.",
	"%s sent the %s back unopened.",
	"%s thinks the %s can wait until next month.",
	"Ask %s whether the %s ever turned up.",
	"%s left a note about the %s.",
	"%s says the %s is fine.",
}

// noiseThings are the everyday things noise turns are about. None of them
// is a planted value, which the workload's tests check rather than assume.
var noiseThings = []string{
	"invoice", "rota", "printer", "kettle", "parking permit", "fire drill",
	"lift", "post", "recycling", "badge", "window blind", "spare key",
	"coffee machine", "notice board", "stationery order", "visitor log",
	"first aid box", "photocopier", "umbrella stand", "wall clock",
}

// noiseText draws one turn that carries no fact. The two slots make the
// noise turns mostly distinct: identical turns would supersede one another
// in a store that supersedes on similarity, which would flatter it.
func noiseText(rng *rand.Rand) string {
	template := noiseTemplates[rng.Intn(len(noiseTemplates))]
	subject := subjects[rng.Intn(len(subjects))]
	thing := noiseThings[rng.Intn(len(noiseThings))]
	return fmt.Sprintf(template, subject, thing)
}
