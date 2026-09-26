package study

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Message is one source message: a replication message in the base, or
// a chain exchange's or the tail's message in the stream. IDs are the
// source IDs a store assigns when the messages are recorded in order.
type Message struct {
	ID      int64  `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
	// Chain and Hop name the chain exchange a stream message belongs to;
	// "tail" and 0 for the tail, empty for the base.
	Chain string `json:"chain,omitempty"`
	Hop   int    `json:"hop,omitempty"`
}

// Hop is one link of a chain: the statement that carries it, the two
// entities it joins (the last hop's second is the answer), and Source, the
// ID of the statement's message in the stream.
type Hop struct {
	Statement string   `json:"statement"`
	Ends      []string `json:"ends"`
	Key       string   `json:"key"`
	Source    int64    `json:"source"`
}

// Chain is one primary probe: a query answered only through its hops.
type Chain struct {
	ID     string          `json:"id"`
	Family string          `json:"family"`
	K      int             `json:"k"`
	Domain string          `json:"domain"`
	Start  string          `json:"start"`
	Query  string          `json:"query"`
	Hops   []Hop           `json:"hops"`
	Answer string          `json:"answer"`
	Check  json.RawMessage `json:"check"`
}

// Guard is one of the replication's exact, correction or absent queries,
// whose evidence is in the base.
type Guard struct {
	ID       string          `json:"id"`
	Family   string          `json:"family"`
	Query    string          `json:"query"`
	Evidence []int64         `json:"evidence"`
	Key      string          `json:"key,omitempty"`
	Check    json.RawMessage `json:"check"`
}

// Corpus is everything every arm is built from and probed with.
type Corpus struct {
	// Base is the #856 replication's 4,800 messages, IDs 1 to 4,800.
	Base []Message `json:"-"`
	// Stream follows the base: the chain exchanges, then the tail.
	Stream []Message `json:"stream"`
	Chains []Chain   `json:"chains"`
	Guards []Guard   `json:"guards"`
	// Window is the live session's message window.
	Window int `json:"window"`
}

// Messages is the base followed by the stream, in source order.
func (c *Corpus) Messages() []Message {
	return append(append([]Message(nil), c.Base...), c.Stream...)
}

// SourceChars is the number of characters in every source message.
func (c *Corpus) SourceChars() int {
	n := 0
	for _, m := range c.Messages() {
		n += len([]rune(m.Content))
	}
	return n
}

// LoadCorpus reads corpus.json from the study directory and the base from
// the replication evidence beside it.
func LoadCorpus(studyDir string) (*Corpus, error) {
	var c Corpus
	if err := readJSON(filepath.Join(studyDir, "corpus.json"), &c); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(ResearchDir(studyDir), "mid-memory-rag", "evidence", "replication", "corpus.json"), &c.Base); err != nil {
		return nil, err
	}
	for i, m := range c.Messages() {
		if m.ID != int64(i+1) {
			return nil, fmt.Errorf("message %d has ID %d; the corpus must number sources from 1 without gaps", i+1, m.ID)
		}
	}
	return &c, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ReadJSONL decodes every line of a JSON Lines file.
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var v T
		if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, scanner.Err()
}
