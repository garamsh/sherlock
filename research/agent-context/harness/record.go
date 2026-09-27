package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// exchange is one chat call as the session made it: what the model was
// shown and what it answered.
type exchange struct {
	Tools    []string
	Messages []agent.Message
	Response agent.Response
	Err      error
}

// recorder stands between the session and the production client and keeps
// every exchange, so the study reads the context each call actually had.
type recorder struct {
	inner agent.Model

	mu        sync.Mutex
	exchanges []exchange
}

func (r *recorder) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	resp, err := r.inner.Chat(ctx, messages, tools)
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	r.mu.Lock()
	r.exchanges = append(r.exchanges, exchange{Tools: names, Messages: append([]agent.Message(nil), messages...), Response: resp, Err: err})
	r.mu.Unlock()
	return resp, err
}

func (r *recorder) take() []exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.exchanges
	r.exchanges = nil
	return out
}

// kind names an exchange by the one reserved tool it offers, as the proxy
// does.
func (e exchange) kind() string {
	if len(e.Tools) == 1 {
		switch e.Tools[0] {
		case "memory_summary":
			return "synthesis"
		case "memory_understand":
			return "understand"
		}
	}
	return "act"
}

// actContext is what an act call had in view that the turn's own model
// output did not put there: everything before the turn's input message,
// the input itself, and the tool results that followed it.
func actContext(e exchange, input string) []string {
	start := len(e.Messages)
	for i := len(e.Messages) - 1; i >= 0; i-- {
		if e.Messages[i].Role == agent.RoleUser && e.Messages[i].Content == input {
			start = i
			break
		}
	}
	var out []string
	for i, m := range e.Messages {
		if i <= start || m.Role == agent.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// reached reports whether any act call of a turn had a key in view.
func reached(exchanges []exchange, input string, keys []string) bool {
	for _, e := range exchanges {
		if e.kind() != "act" {
			continue
		}
		for _, text := range actContext(e, input) {
			if containsAny(text, keys) {
				return true
			}
		}
	}
	return false
}

// containsAny reports whether text states any key, matched the way the
// scorer matches a value (score.py value_tokens and states): every token
// with a digit, every capitalized word and every month must appear as a
// whole word, or, for a key with none of those, every word but connectives.
// A number may carry an ordinal suffix and may drop its thousands commas;
// a 24-hour time may appear on the 12-hour clock.
func containsAny(text string, keys []string) bool {
	norm := normalizeText(text)
	for _, k := range keys {
		tokens := keyTokens(k)
		if len(tokens) == 0 {
			continue
		}
		all := true
		for _, t := range tokens {
			if !tokenPattern(t).MatchString(norm) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

var (
	thousands = regexp.MustCompile(`(\d),(\d{3})`)
	rawToken  = regexp.MustCompile(`[A-Za-z0-9]+(?::[0-9]+)?`)
	hasDigit  = regexp.MustCompile(`\d`)
	clock     = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	months    = map[string]bool{"january": true, "february": true, "march": true, "april": true, "may": true, "june": true, "july": true, "august": true, "september": true, "october": true, "november": true, "december": true}
	stopWords = map[string]bool{"with": true, "the": true, "a": true, "an": true, "of": true, "and": true}
)

func dropThousands(s string) string {
	for {
		next := thousands.ReplaceAllString(s, "$1$2")
		if next == s {
			return s
		}
		s = next
	}
}

func normalizeText(s string) string {
	s = dropThousands(strings.ToLower(s))
	return strings.NewReplacer("€", " ", "$", " ").Replace(s)
}

func keyTokens(key string) []string {
	raw := rawToken.FindAllString(dropThousands(key), -1)
	var marked, rest []string
	for _, t := range raw {
		if hasDigit.MatchString(t) || unicode.IsUpper([]rune(t)[0]) || months[strings.ToLower(t)] {
			marked = append(marked, strings.ToLower(t))
		} else if !stopWords[strings.ToLower(t)] {
			rest = append(rest, strings.ToLower(t))
		}
	}
	if len(marked) > 0 {
		return marked
	}
	return rest
}

func tokenPattern(t string) *regexp.Regexp {
	p := regexp.QuoteMeta(t)
	if _, err := strconv.Atoi(t); err == nil {
		p += `(?:st|nd|rd|th)?`
	}
	if m := clock.FindStringSubmatch(t); m != nil {
		if h, _ := strconv.Atoi(m[1]); h > 12 {
			p = fmt.Sprintf(`(?:%s|%d:%s\s*(?:pm|p\.m\.))`, p, h-12, m[2])
		}
	}
	return regexp.MustCompile(`(?:^|[^a-z0-9])` + p + `(?:$|[^a-z0-9])`)
}
