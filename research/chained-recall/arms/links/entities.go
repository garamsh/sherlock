package main

import (
	"regexp"
	"strings"
	"unicode"
)

// word is one token: letters and digits, joined inside by hyphens or
// apostrophes ("REF-FEPE732", "don't").
var word = regexp.MustCompile(`[\p{L}\p{N}]+(?:[-'’][\p{L}\p{N}]+)*`)

// stopwords never begin an entity name. A capitalized run that starts a
// sentence ("Every part for Rodend") loses them, and a run made of them
// alone ("The", "User") is no entity.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the this that these those every each all any some both no not
		our their his her its my your we they he she it i you me us them
		user assistant what when where which who whom whose why how
		and or but if then so also as at by for from in into of on onto to under with without
		is are was were be been has have had do does did will would can could should may might must
		please thanks thank yes ok okay got noted understood kept sure great good hi hello here there`) {
		stopwords[w] = true
	}
}

// tokens lowercases text into words, dropping a trailing possessive.
func tokens(text string) []string {
	var out []string
	for _, w := range word.FindAllString(text, -1) {
		out = append(out, stripPossessive(strings.ToLower(w)))
	}
	return out
}

func stripPossessive(w string) string {
	for _, suffix := range []string{"'s", "’s"} {
		if strings.HasSuffix(w, suffix) && len(w) > len(suffix) {
			return strings.TrimSuffix(w, suffix)
		}
	}
	return w
}

// normalize is the key an entity is stored and matched under: its words,
// lowercased, without leading stopwords. "" means the name holds none.
func normalize(name string) string {
	ws := tokens(name)
	for len(ws) > 0 && stopwords[ws[0]] {
		ws = ws[1:]
	}
	return strings.Join(ws, " ")
}

// ruleEntities extracts entity names from a summary by rule, standing in
// for names the synthesis call would write: every maximal run of
// capitalized words that only spaces separate.
func ruleEntities(text string) []string {
	var runs []string
	var run []string
	end := 0
	for _, span := range word.FindAllStringIndex(text, -1) {
		w := text[span[0]:span[1]]
		capitalized := unicode.IsUpper([]rune(w)[0])
		adjacent := strings.TrimSpace(text[end:span[0]]) == ""
		if len(run) > 0 && (!capitalized || !adjacent) {
			runs, run = append(runs, strings.Join(run, " ")), nil
		}
		if capitalized {
			run = append(run, w)
		}
		end = span[1]
	}
	if len(run) > 0 {
		runs = append(runs, strings.Join(run, " "))
	}
	return names(runs)
}

// names normalizes raw entity names, dropping empty and repeated ones.
func names(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range raw {
		name := normalize(n)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
