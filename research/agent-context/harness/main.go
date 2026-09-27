// Command harness runs the #862 experiment (docs/research/agent-context/).
//
// It is built separately inside each arm's source tree, so every arm runs
// its own production session, store and recall. It uses only the API the
// shipped structure and both candidates share.
//
//	harness proxy -addr 127.0.0.1:8091 -ceiling 4 -dir <ledger dir>   # MINIMAX_KEY from the environment
//	harness base -out <base.db> -embed-url http://127.0.0.1:8090/v1
//	harness run -arm A -base <base.db> -out <dir> -proxy http://127.0.0.1:8091 [-only F-01,G-02]
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
		log.Fatal("usage: harness proxy|base|run [flags]")
	}
	var err error
	switch os.Args[1] {
	case "proxy":
		err = runProxy(os.Args[2:])
	case "base":
		err = runBase(os.Args[2:])
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
	upstream := fs.String("upstream", "https://api.minimax.io/v1/chat/completions", "provider endpoint")
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
	p := &proxy{
		upstream: *upstream, key: key,
		ledger: &ledger{ceiling: *ceiling, spent: spent},
		client: &http.Client{Timeout: 5 * time.Minute},
		log:    calls, cache: cache, inflight: map[string]chan struct{}{}, cacheLog: cacheLog,
	}
	log.Printf("proxy on %s: ceiling %.2f, already spent %.4f, %d cached syntheses", *addr, *ceiling, spent, len(cache))
	return http.ListenAndServe(*addr, p)
}
