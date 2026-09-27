// Command harness runs the shared phases of the #874 chained-recall study
// (research/chained-recall/). Arms are separate commands under arms/.
//
//	harness proxy -ceiling <USD> -dir <ledger dir> [-probe]  # MINIMAX_KEY in its environment only
//	harness screen -work <dir>                               # the chain check before synthesis; free
//	harness synthesize -batch 4 -proxy http://127.0.0.1:8091 # the stream's summaries; paid
//	harness build -batch 4 -work <dir>                       # a shared store from recorded summaries; free
//	harness check -batch 4 -work <dir>                       # the chain check on a shared store; free
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: harness proxy|screen|synthesize|build|check [flags]")
	}
	var err error
	switch os.Args[1] {
	case "proxy":
		err = runProxy(os.Args[2:])
	case "screen", "synthesize", "build", "check":
		err = runStudy(os.Args[1], os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runProxy(args []string) error {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8091", "listen address")
	ceiling := fs.Float64("ceiling", 0, "USD-equivalent ceiling at list prices, counting every call already in the ledger; a guard, since the key is on a subscription")
	dir := fs.String("dir", "", "directory holding calls.jsonl, synthesis-cache.jsonl and probes.log")
	upstream := fs.String("upstream", "https://api.minimax.io/v1/chat/completions", "provider endpoint")
	probeFirst := fs.Bool("probe", false, "send one minimal ledgered request before serving, and stay paused if it meets the usage limit")
	_ = fs.Parse(args)
	key := os.Getenv("MINIMAX_KEY")
	if key == "" || *dir == "" || *ceiling <= 0 {
		return errors.New("proxy needs MINIMAX_KEY in the environment, -dir and a positive -ceiling")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	callsPath := filepath.Join(*dir, "calls.jsonl")
	spent, err := loadSpent(callsPath)
	if err != nil {
		return err
	}
	cache, err := loadCache(filepath.Join(*dir, "synthesis-cache.jsonl"))
	if err != nil {
		return err
	}
	calls, err := os.OpenFile(callsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer calls.Close()
	cacheLog, err := os.OpenFile(filepath.Join(*dir, "synthesis-cache.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer cacheLog.Close()
	probes, err := os.OpenFile(filepath.Join(*dir, "probes.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer probes.Close()
	p := &proxy{
		upstream: *upstream, key: key,
		ledger: &ledger{ceiling: *ceiling, spent: spent},
		client: &http.Client{Timeout: 5 * time.Minute},
		log:    calls, cache: cache, inflight: map[string]chan struct{}{}, cacheLog: cacheLog,
	}
	p.quota = &quotaGate{probe: p.probeKey, log: probes, wait: probeInterval}
	if *probeFirst {
		status, body := p.probeKey(context.Background())
		p.quota.note(fmt.Sprintf("preflight probe: HTTP %d", status))
		if usageLimit(status, body) {
			p.quota.close()
		} else if status != http.StatusOK {
			return fmt.Errorf("preflight probe answered HTTP %d", status)
		}
	}
	log.Printf("proxy on %s: ceiling %.2f, already spent %.4f, %d cached syntheses", *addr, *ceiling, spent, len(cache))
	return http.ListenAndServe(*addr, p)
}

func runStudy(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	dir := fs.String("dir", "research/chained-recall", "study directory")
	work := fs.String("work", "", "scratch root outside the repository")
	proxyURL := fs.String("proxy", "http://127.0.0.1:8091", "study proxy base URL")
	embedURL := fs.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL (local TEI)")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model")
	batch := fs.Int("batch", 4, "batch size")
	workers := fs.Int("workers", 2, "synthesis requests at once")
	_ = fs.Parse(args)
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: *embedURL, Model: *embedModel})
	if err != nil {
		return err
	}
	ctx := context.Background()
	if command == "screen" {
		if *work == "" {
			return errors.New("screen needs -work for its embedding cache")
		}
		return screen(ctx, *dir, *work, embedder)
	}
	corpus, err := study.LoadCorpus(*dir)
	if err != nil {
		return err
	}
	env := &study.Env{Corpus: corpus, Embedder: embedder, Dir: *dir, Work: *work, Proxy: *proxyURL, Arm: "shared"}
	switch command {
	case "synthesize":
		return synthesize(ctx, env, *batch, *workers)
	case "build":
		if *work == "" {
			return errors.New("build needs -work")
		}
		return study.BuildShared(ctx, env, *batch, filepath.Join(*work, "shared", fmt.Sprintf("b%d", *batch)))
	default:
		if *work == "" {
			return errors.New("check needs -work")
		}
		return check(ctx, env, *batch)
	}
}
