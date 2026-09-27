package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

// ScreenTop is the raw-query depth a hop beyond the first must stay out of
// for its chain to be kept: deeper than any arm's delivered budget, which
// is at most 20 items.
const ScreenTop = 20

type poolChain struct {
	ID     string `json:"id"`
	Family string `json:"family"`
	K      int    `json:"k"`
	Query  string `json:"query"`
	Hops   []struct {
		Exchange []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"exchange"`
	} `json:"hops"`
}

// exchangeText is an exchange as a batch-4 synthesis request carries it,
// the stand-in for its mid's summary before any summary exists.
func exchangeText(c poolChain, hop int) string {
	lines := make([]string, 0, 4)
	for _, m := range c.Hops[hop].Exchange {
		lines = append(lines, m.Role+": "+m.Content)
	}
	return strings.Join(lines, "\n")
}

// vectorCache embeds each distinct text once across runs.
type vectorCache struct {
	embedder agent.Embedder
	path     string
	vectors  map[string][]float32
	file     *os.File
}

func openVectorCache(path string, embedder agent.Embedder) (*vectorCache, error) {
	c := &vectorCache{embedder: embedder, path: path, vectors: map[string][]float32{}}
	type entry struct {
		Key    string    `json:"key"`
		Vector []float32 `json:"vector"`
	}
	if _, err := os.Stat(path); err == nil {
		entries, err := study.ReadJSONL[entry](path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			c.vectors[e.Key] = e.Vector
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	c.file = f
	return c, nil
}

func (c *vectorCache) embed(ctx context.Context, text string) ([]float32, error) {
	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:])
	if v, ok := c.vectors[key]; ok {
		return v, nil
	}
	v, err := c.embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	c.vectors[key] = v
	line, _ := json.Marshal(map[string]any{"key": key, "vector": v})
	_, err = c.file.Write(append(line, '\n'))
	return v, err
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot, na, nb = dot+float64(a[i])*float64(b[i]), na+float64(a[i])*float64(a[i]), nb+float64(b[i])*float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

type screened struct {
	ID    string `json:"id"`
	Pass  bool   `json:"pass"`
	Ranks []int  `json:"ranks"`
}

// screen is the chain check before synthesis. Every candidate exchange and
// every base mid summary (the replication's batch-4 syntheses) is a
// candidate item; a chain passes when its raw query ranks none of its hop
// 2 and hop 3 exchanges within ScreenTop. Ranks are 1-based over the whole
// pool, so they are recorded for every hop, the first included.
func screen(ctx context.Context, dir, work string, embedder agent.Embedder) error {
	var pool []poolChain
	data, err := os.ReadFile(filepath.Join(dir, "pool.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &pool); err != nil {
		return err
	}
	type recorded struct {
		Summary string `json:"summary"`
	}
	base, err := study.ReadJSONL[recorded](filepath.Join(study.ResearchDir(dir), "mid-memory-rag", "evidence", "replication", "synthesis-n4.jsonl"))
	if err != nil {
		return err
	}
	cache, err := openVectorCache(filepath.Join(work, "screen-vectors.jsonl"), embedder)
	if err != nil {
		return err
	}
	defer cache.file.Close()
	type candidate struct {
		chain  string
		hop    int
		vector []float32
	}
	var items []candidate
	for _, b := range base {
		v, err := cache.embed(ctx, strings.TrimSpace(b.Summary))
		if err != nil {
			return err
		}
		items = append(items, candidate{vector: v})
	}
	for _, c := range pool {
		for h := range c.Hops {
			v, err := cache.embed(ctx, exchangeText(c, h))
			if err != nil {
				return err
			}
			items = append(items, candidate{chain: c.ID, hop: h, vector: v})
		}
	}
	var out []screened
	counts := map[string][2]int{}
	for _, c := range pool {
		q, err := cache.embed(ctx, c.Query)
		if err != nil {
			return err
		}
		order := make([]int, len(items))
		scores := make([]float64, len(items))
		for i, it := range items {
			order[i], scores[i] = i, cosine(q, it.vector)
		}
		sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
		ranks := make([]int, len(c.Hops))
		for r, i := range order {
			if items[i].chain == c.ID {
				ranks[items[i].hop] = r + 1
			}
		}
		pass := true
		for h := 1; h < len(ranks); h++ {
			pass = pass && ranks[h] > ScreenTop
		}
		out = append(out, screened{ID: c.ID, Pass: pass, Ranks: ranks})
		n := counts[c.Family]
		n[1]++
		if pass {
			n[0]++
		}
		counts[c.Family] = n
	}
	summary := map[string]any{}
	for f, n := range counts {
		summary[f] = map[string]int{"passed": n[0], "candidates": n[1]}
	}
	fmt.Println(summary)
	data, err = json.MarshalIndent(map[string]any{"top": ScreenTop, "base_mids": len(base), "families": summary, "chains": out}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "evidence", "screen.json"), append(data, '\n'), 0o644)
}
