package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

type episode struct {
	Summary string   `json:"summary"`
	Sources []string `json:"sources"`
}

type request struct {
	CheckTerms bool      `json:"check_terms"`
	Corpus     []episode `json:"corpus"`
	Terms      []string  `json:"terms"`
	IDs        []int64   `json:"ids"`
}

type fixedSummary string

func (s fixedSummary) Summarize(context.Context, []string) (string, error) { return string(s), nil }

func run() error {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "recall-842-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	for _, ep := range req.Corpus {
		if len(ep.Sources) != 8 {
			return fmt.Errorf("fixture episode needs eight sources")
		}
		var sources []agent.SourceMessage
		for _, content := range ep.Sources {
			sources = append(sources, agent.SourceMessage{Role: agent.RoleUser, Content: content})
		}
		if err := store.RecordTurn(ctx, sources, fixedSummary(ep.Summary)); err != nil {
			return err
		}
	}
	var out any
	if req.CheckTerms {
		return json.NewEncoder(os.Stdout).Encode(agent.SearchTokens(req.Terms))
	}
	if req.IDs != nil {
		sources := []agent.SourceMessage{}
		for _, id := range req.IDs {
			source, err := store.Source(ctx, id)
			if err != nil {
				return err
			}
			sources = append(sources, source)
		}
		out = sources
	} else {
		recall := memory.ConversationRecall{Store: store}
		result, err := recall.Recall(ctx, joinTerms(req.Terms))
		if err != nil {
			return err
		}
		out = result
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func joinTerms(terms []string) string {
	result := ""
	for _, term := range terms {
		result += " " + term
	}
	return result
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
