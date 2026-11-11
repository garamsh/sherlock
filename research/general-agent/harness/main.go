// Command harness runs the #1164 study (research/general-agent/).
//
// It is built separately inside each arm's source tree, so each arm runs
// its own production session, store and tools wiring. It uses only the API
// the shipped structure S and the file-and-skill loop H share.
//
//	harness proxy -ceiling 3 -dir <ledger dir> -model <id> -provider <slug> -input-usd 0.02 -output-usd 0.04
//	harness run -arm S -corpus research/general-agent/corpus.json -out <dir> -proxy http://127.0.0.1:8091 -model <id>
//
// The proxy reads OPENROUTER_API_KEY from its own environment; nothing
// else needs it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: harness proxy|run [flags]")
	}
	var err error
	switch os.Args[1] {
	case "proxy":
		err = runProxy(os.Args[2:])
	case "run":
		err = runArm(os.Args[2:])
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
	ceiling := fs.Float64("ceiling", 0, "USD ceiling, counting every call already in the ledger")
	dir := fs.String("dir", "", "directory holding calls.jsonl and synthesis-cache.jsonl")
	upstream := fs.String("upstream", "https://openrouter.ai/api/v1/chat/completions", "provider endpoint")
	model := fs.String("model", "", "the one model every request is sent to")
	provider := fs.String("provider", "", "the one OpenRouter provider every request is routed to")
	inputUSD := fs.Float64("input-usd", 0, "USD per million input tokens at the pinned provider, for reservations")
	outputUSD := fs.Float64("output-usd", 0, "USD per million output tokens at the pinned provider, for reservations")
	bodies := fs.Bool("bodies", false, "record every request and answer in bodies.jsonl, for diagnosis")
	_ = fs.Parse(args)
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" || *dir == "" || *ceiling <= 0 || *model == "" || *provider == "" || *inputUSD <= 0 || *outputUSD <= 0 {
		return errors.New("proxy needs OPENROUTER_API_KEY in the environment, -dir, a positive -ceiling, -model, -provider and both prices")
	}
	p, closeLogs, err := newProxy(proxyConfig{
		upstream: *upstream, key: key, dir: *dir, ceiling: *ceiling, model: *model, provider: *provider,
		price: price{input: *inputUSD / 1e6, output: *outputUSD / 1e6}, bodies: *bodies,
	})
	if err != nil {
		return err
	}
	defer closeLogs()
	spent, _ := p.ledger.state()
	log.Printf("proxy on %s: model %s at %s, ceiling %.2f, already spent %.4f, %d cached syntheses",
		*addr, *model, *provider, *ceiling, spent, len(p.cache))
	server := &http.Server{Addr: *addr, Handler: p, ReadHeaderTimeout: 10 * time.Second}
	return server.ListenAndServe()
}

func runArm(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	arm := fs.String("arm", "", "arm name: S or H")
	corpusPath := fs.String("corpus", "research/general-agent/corpus.json", "corpus file")
	out := fs.String("out", "", "output directory for results.jsonl and transcripts")
	proxyURL := fs.String("proxy", "http://127.0.0.1:8091", "study proxy")
	model := fs.String("model", "", "model ID the arms request; the proxy sends every request to its own -model")
	embedURL := fs.String("embed-url", "http://127.0.0.1:8090/v1", "OpenAI-compatible embeddings base URL")
	embedModel := fs.String("embed-model", "BAAI/bge-base-en-v1.5", "embedding model name")
	toolsDir := fs.String("tools", "tools", "directory holding the shipped tool manifests")
	only := fs.String("only", "", "comma-separated family IDs to run; empty runs all")
	workers := fs.Int("workers", 2, "families run at once")
	window := fs.Int("window", 0, "rolling window in messages; zero takes the corpus's")
	_ = fs.Parse(args)
	if *arm == "" || *out == "" || *model == "" {
		return errors.New("run needs -arm, -out and -model")
	}
	r, err := newRunner(runnerConfig{
		arm: *arm, corpusPath: *corpusPath, proxy: *proxyURL, model: *model,
		embedURL: *embedURL, embedModel: *embedModel, toolsDir: *toolsDir, window: *window,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	return r.runAll(filepath.Clean(*out), selection(*only), *workers)
}
