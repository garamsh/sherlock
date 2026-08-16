package main

import "strings"

// A question-shaped address, and whether arity can be read off it.
//
// Every address above is `(subject, attribute)` with the attribute a topic
// noun — `project`, `clinic`, `accounts`. `memory-creation/` measured a key
// naming the question a fact answers against exactly that topic key and
// found it the round's best correction result, and filed it as "an
// improvement to the write vocabulary, not to the store shape". This file
// is that vocabulary, formed here rather than assumed, so the two
// addressings can be run side by side with nothing else varied.
//
// The hypothesis is that a question carries its own arity. "Which project
// does Maria lead?" asks for one project; a question asking for languages
// asks for several. If the address names the question, how many values it
// holds may be readable from the address rather than declared beside every
// write.
//
// What would make that worthless is constructing the question out of
// knowledge of the answer. Everything below therefore reads the sentence,
// the subject and the value and nothing else: no class, no wording label,
// and never `Assert.Replaces`. `question_test.go` holds that mechanically
// rather than by reading this comment — it re-derives every address after
// flipping the workload's ground truth and requires the addresses not to
// move.

// prepositions open a fronted phrase that restricts what a statement is
// about, which is what makes it part of the question rather than part of
// the answer. English closes this class: it is a list, and unlike a list of
// addition cues it is not one an author extends, which is the whole of why
// the address rule reads position and word class rather than meaning.
var prepositions = map[string]bool{
	"about": true, "above": true, "across": true, "after": true, "against": true,
	"along": true, "among": true, "around": true, "at": true, "before": true,
	"behind": true, "below": true, "beneath": true, "beside": true, "besides": true,
	"between": true, "beyond": true, "by": true, "despite": true, "down": true,
	"during": true, "except": true, "for": true, "from": true, "in": true,
	"inside": true, "into": true, "near": true, "of": true, "off": true,
	"on": true, "onto": true, "outside": true, "over": true, "past": true,
	"since": true, "through": true, "throughout": true, "to": true, "toward": true,
	"towards": true, "under": true, "underneath": true, "until": true, "up": true,
	"upon": true, "with": true, "within": true, "without": true,
}

// auxiliaries are what follows a bare interrogative. "Which project does" has
// a head noun between the two and "What does" has none, which is the
// difference the number rule reads. This is a function-word class and closed
// for the same reason the prepositions are.
var auxiliaries = map[string]bool{
	"does": true, "do": true, "did": true, "is": true, "are": true, "was": true,
	"were": true, "has": true, "have": true, "had": true, "will": true,
	"would": true, "can": true, "could": true, "should": true, "must": true,
}

// clauseBoundaries end the span a statement occupies inside a longer
// message. A framed or packed message puts other people's words around an
// assertion, and only the assertion's own clause can say what its question
// restricts.
const clauseBoundaries = ",;:—"

// questionArity is what a question's own form says about how many values it
// answers with. Unmarked is a third answer rather than a default: an
// interrogative with no head noun marks no number in English, and reporting
// it as singular would be reporting a guess as a reading.
type questionArity string

const (
	questionSingular questionArity = "singular"
	questionPlural   questionArity = "plural"
	questionUnmarked questionArity = "unmarked"
)

// questionStems maps an attribute to the question the checkpoint asks about
// it, in the form an address holds it. The question templates are the
// workload's own and this round did not write them: they were there to be
// asked, and an address that names one is naming what was already being
// asked rather than something composed for the result.
var questionStems = buildQuestionStems()

func buildQuestionStems() map[string]string {
	out := make(map[string]string, len(attributes))
	for _, a := range attributes {
		out[a.name] = stemOf(a.question)
	}
	return out
}

// stemOf turns a question into the key an address holds it under: the
// question's own words, lower-cased, with the subject slot and the question
// mark removed.
func stemOf(question string) string {
	words := strings.Fields(strings.ToLower(strings.NewReplacer("%s", " ", "?", "").Replace(question)))
	return strings.Join(words, "-")
}

// QuestionAddress is where a fact goes when the address names the question
// it answers rather than the topic it is about.
//
// It reads the message, the subject and the value. It does not read what the
// assertion replaces, which class the thread is on or which template wrote
// the sentence — those are the answers, and an address derived from them
// would be an address that already knew what it is being measured to find
// out.
func QuestionAddress(text, subject, value, attribute string) Address {
	stem, ok := questionStems[attribute]
	if !ok {
		// An extractor may address a fact outside the vocabulary. The fact
		// lands where it was addressed rather than being dropped, so a
		// misaddressing costs what misaddressing costs and not less.
		stem = attribute
	}
	return Address{Subject: subject, Attribute: stem, Qualifier: qualifierIn(text, subject, value)}
}

// qualifierIn is the restriction the sentence puts on the question, or the
// empty string where it puts none.
//
// A statement that opens with a prepositional phrase before naming its
// subject is answering a narrower question than the same statement without
// it: "On Thursdays Maria leads the X project" answers which project Maria
// leads on Thursdays. A statement that adds a phrase after its subject
// answers the same question it would have answered without it, which is why
// "as well" and "from now on" leave the address where it was.
func qualifierIn(text, subject, value string) string {
	clause := clauseAround(text, value)
	words := strings.Fields(strings.Trim(clause, " ."))
	if len(words) == 0 || !prepositions[strings.Trim(words[0], ",.;:—")] {
		return ""
	}
	for i, word := range words {
		if strings.Trim(word, ",.;:—") != strings.ToLower(subject) {
			continue
		}
		if i == 0 {
			return ""
		}
		return strings.Join(words[:i], " ")
	}
	return ""
}

// clauseAround is the span of a message the value sits in, bounded by the
// message's ends and by the punctuation that separates one clause from the
// next. A message carrying three assertions carries three of these, and a
// framed one puts the framing in its own.
func clauseAround(text, value string) string {
	lower := strings.ToLower(text)
	if value == "" {
		return lower
	}
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return strings.ContainsRune(clauseBoundaries, r)
	}) {
		if Mentions(part, value) {
			return part
		}
	}
	return lower
}

// numberOf reads a question's own form for the number it marks. An
// interrogative with a head noun marks one — "which project" singular,
// "which projects" plural — and a bare interrogative marks none.
func numberOf(stem string) questionArity {
	words := strings.Split(stem, "-")
	if len(words) < 2 {
		return questionUnmarked
	}
	if words[0] != "which" && words[0] != "what" {
		return questionUnmarked
	}
	head := words[1]
	if auxiliaries[head] {
		return questionUnmarked
	}
	if strings.HasSuffix(head, "s") && !strings.HasSuffix(head, "ss") {
		return questionPlural
	}
	return questionSingular
}

// readdress is the turn as one frame's addressing forms it. The two
// addressings differ here and nowhere else: the same extraction, the same
// values, the same turns, and one function deciding where each of them goes.
func readdress(t Turn, frameName string) Turn {
	if !addressesByQuestion(frameName) {
		return t
	}
	if len(t.Written) > 0 {
		written := make([]Written, len(t.Written))
		copy(written, t.Written)
		for i := range written {
			written[i].Address = QuestionAddress(t.Text, written[i].Subject,
				written[i].Value, written[i].Attribute)
		}
		t.Written = written
	}
	if t.Ask != nil {
		// A checkpoint asks the question itself, so its address is the
		// question with no restriction on it. What answers a narrower
		// question also answers this one, which is what the frame's read
		// path does with it.
		asked := QuestionAddress(t.Text, t.Ask.Subject, "", t.Ask.Attribute)
		t.Ask = &asked
	}
	return t
}

// addressesByQuestion reports whether a frame holds facts at the question
// they answer.
func addressesByQuestion(frameName string) bool {
	return frameName == frameQuestion || frameName == frameQuestionDeclared
}

// QuestionForm is one question in the address space and what its own form
// says about the addresses under it.
//
// Agreed counts the addresses whose arity the form gets right. Reporting it
// per question is what separates "the question determines arity" from "the
// question determines arity for the questions that mark a number", which
// are different findings and only one of them is this workload's.
type QuestionForm struct {
	Attribute string `json:"attribute"`
	Question  string `json:"question"`
	Number    string `json:"number"`
	Predicted string `json:"predicted_arity"`
	// FromForm is whether the prediction came from the question's own
	// number or from the fallback a structure needs where it marks none.
	FromForm bool `json:"read_from_the_form"`
	// Addresses under this question, and how many of them ever hold more
	// than one value that is true at the same time.
	Addresses   int     `json:"addresses"`
	MultiValued int     `json:"addresses_holding_two_at_once"`
	Agreed      int     `json:"addresses_the_form_gets_right"`
	Share       float64 `json:"share"`
}

// QuestionCut is one cut of the addresses a question's form gets wrong. The
// residual is the finding when most addresses carry their arity, because it
// is what a structure still has to handle.
type QuestionCut struct {
	Cut       string  `json:"cut"`
	Name      string  `json:"name"`
	Addresses int     `json:"addresses"`
	Wrong     int     `json:"form_gets_wrong"`
	Share     float64 `json:"share"`
}

// QuestionAddressing is the evidence about reading arity off an address.
type QuestionAddressing struct {
	Forms []QuestionForm `json:"forms"`
	// Residual cut by the class the address's thread is on, by whether the
	// question's form marks a number at all, and by the wording that put a
	// second value where one was already held.
	Residual []QuestionCut `json:"residual"`
	// The two addressings side by side: how many addresses each produces
	// and how many of them its question's form gets the arity of wrong.
	TopicAddresses    int `json:"topic_addresses"`
	TopicWrong        int `json:"topic_addresses_the_form_gets_wrong"`
	QuestionAddresses int `json:"question_addresses"`
	QuestionWrong     int `json:"question_addresses_the_form_gets_wrong"`
	Qualified         int `json:"question_addresses_carrying_a_restriction"`
	// Questions whose form marks a number, out of the questions asked.
	Questions  int `json:"questions"`
	FormMarked int `json:"questions_whose_form_marks_a_number"`
}

// addressUse is one address's history: how many values are true at it at
// once at the most, the class of the thread that wrote it, and the wording
// of the assertion that first put a second value beside a first.
type addressUse struct {
	live     []string
	most     int
	class    string
	joinedBy string
}

func (u *addressUse) apply(a Assert, class string) {
	u.class = class
	if a.Replaces {
		u.live = u.live[:0]
	}
	if !contains(u.live, a.Value) {
		u.live = append(u.live, a.Value)
	}
	if len(u.live) > u.most {
		u.most = len(u.live)
		if u.most > 1 && u.joinedBy == "" {
			u.joinedBy = a.Wording
		}
	}
}

// MeasureQuestions reads the workload against both addressings and reports
// where a question's own form carries the arity of the addresses under it.
//
// It reads `Replaces` — this is the scoring side, where ground truth
// belongs. Nothing that forms an address reads it, which is the distinction
// the round rests on and the one `question_test.go` checks.
func MeasureQuestions(w Workload) QuestionAddressing {
	out := QuestionAddressing{}
	single := map[string]bool{}
	byQuestion := map[string]*QuestionForm{}
	for _, a := range attributes {
		stem := questionStems[a.name]
		number := numberOf(stem)
		single[stem] = number != questionPlural
		form := &QuestionForm{Attribute: a.name, Question: stem, Number: string(number),
			Predicted: string(aritySingle), FromForm: number != questionUnmarked}
		if number == questionPlural {
			form.Predicted = string(arityMulti)
		}
		byQuestion[stem] = form
		out.Questions++
		if number != questionUnmarked {
			out.FormMarked++
		}
	}

	topic, question := map[Address]*addressUse{}, map[Address]*addressUse{}
	for _, thread := range w.Threads {
		for _, assert := range thread.Asserts {
			use(topic, Address{Subject: thread.Subject, Attribute: thread.Attribute}).apply(assert, thread.Class)
			at := QuestionAddress(assert.Text, thread.Subject, assert.Value, thread.Attribute)
			use(question, at).apply(assert, thread.Class)
		}
	}

	byClass, byMarking, byWording := map[string][2]int{}, map[string][2]int{}, map[string][2]int{}
	for address, held := range question {
		form, ok := byQuestion[address.Attribute]
		if !ok {
			continue
		}
		wrong := (held.most > 1) == single[address.Attribute]
		form.Addresses++
		out.QuestionAddresses++
		if address.Qualifier != "" {
			out.Qualified++
		}
		if held.most > 1 {
			form.MultiValued++
		}
		if !wrong {
			form.Agreed++
		} else {
			out.QuestionWrong++
		}
		count(byClass, held.class, wrong)
		count(byMarking, marking(form.FromForm), wrong)
		if held.joinedBy != "" {
			count(byWording, held.joinedBy, wrong)
		}
	}
	for _, a := range attributes {
		form := byQuestion[questionStems[a.name]]
		form.Share = ratio(form.Agreed, form.Addresses)
		out.Forms = append(out.Forms, *form)
	}
	for address, held := range topic {
		out.TopicAddresses++
		if (held.most > 1) == single[questionStems[address.Attribute]] {
			out.TopicWrong++
		}
	}

	out.Residual = append(cutCounts("class", classNames, byClass),
		cutCounts("question form", markingNames, byMarking)...)
	out.Residual = append(out.Residual, cutCounts("second value added by", wordingNames, byWording)...)
	return out
}

// use returns the address's history, starting one where the address is new.
func use(seen map[Address]*addressUse, at Address) *addressUse {
	held, ok := seen[at]
	if !ok {
		held = &addressUse{}
		seen[at] = held
	}
	return held
}

// count adds one address to a cut, and one to its wrong column where the
// question's form got that address wrong.
func count(counts map[string][2]int, name string, wrong bool) {
	pair := counts[name]
	pair[0]++
	if wrong {
		pair[1]++
	}
	counts[name] = pair
}

// The two markings a question can carry, and the order the residual reports
// them in.
const (
	markingMarked   = "marks a number"
	markingUnmarked = "marks none"
)

var markingNames = []string{markingMarked, markingUnmarked}

func marking(fromForm bool) string {
	if fromForm {
		return markingMarked
	}
	return markingUnmarked
}

// cutCounts turns gathered (addresses, wrong) pairs into the record's rows,
// in the order the names are given.
func cutCounts(cut string, names []string, counts map[string][2]int) []QuestionCut {
	out := make([]QuestionCut, 0, len(names))
	for _, name := range names {
		pair, ok := counts[name]
		if !ok {
			continue
		}
		out = append(out, QuestionCut{Cut: cut, Name: name, Addresses: pair[0],
			Wrong: pair[1], Share: ratio(pair[1], pair[0])})
	}
	return out
}
