package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/openai"
)

// The embedders measured. Both are the ones the tree already has: hashing
// is what `cmd/gagent` defaults to, openai is the provider
// `internal/memory/openai` ships, at whatever dimension the model returns
// — it sends no dimensions parameter, so text-embedding-3-small answers at
// its native 1536. openai512-cache is not a third embedder: it replays the
// 512-dimension vectors committed to the memory-creation round-4 cache, so
// this experiment can reproduce that round's figures rather than quote
// them.
const (
	embedderHashing    = "hashing"
	embedderOpenAI     = "openai"
	embedderOpenAI512  = "openai512-cache"
	openAIModel        = "text-embedding-3-small"
	openAIBaseURL      = "https://api.openai.com/v1"
	openAIKeyEnv       = "OPENAI_API_KEY"
	openAIRequestLimit = 8
)

// measuredEmbedders are the embedders the record scores retrieval under.
// openai512-cache is not among them: the corpus is not in the round-4
// cache, so it can only answer the separability question.
var measuredEmbedders = []string{embedderHashing, embedderOpenAI}

// Embedder pairs a memory.Embedder with the name every figure it produces
// is labelled by. A figure that does not say which embedder made it is the
// defect #410 exists to stop, so the name travels with the vectors.
type Embedder struct {
	Name     string
	Dims     int
	Embedder memory.Embedder
}

// cachingEmbedder memoizes vectors by text. The OpenAI provider charges per
// text and answers in about a tenth of a second, and one corpus is embedded
// nine times over the plantings and store sizes; the cache means each
// distinct text is paid for once, here and across runs. offline refuses to
// call the provider at all, which is how the committed round-4 vectors are
// replayed without an API key.
type cachingEmbedder struct {
	inner   memory.Embedder
	path    string
	offline bool

	mu      sync.RWMutex
	vectors map[string][]float32
	dirty   bool
}

// Embed implements memory.Embedder.
func (c *cachingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	c.mu.RLock()
	vec, ok := c.vectors[text]
	c.mu.RUnlock()
	if ok {
		return vec, nil
	}
	if c.offline {
		return nil, fmt.Errorf("no cached vector for %q and this embedder makes no requests", text)
	}
	vec, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.vectors[text] = vec
	c.dirty = true
	c.mu.Unlock()
	return vec, nil
}

// prefetch fills the cache for every distinct text before the measurement
// starts, so the run pays each text once and the timings that follow
// measure the store rather than the network.
func (c *cachingEmbedder) prefetch(ctx context.Context, texts []string) error {
	missing := make([]string, 0, len(texts))
	seen := make(map[string]bool, len(texts))
	c.mu.RLock()
	for _, text := range texts {
		if _, ok := c.vectors[text]; !ok && !seen[text] {
			missing = append(missing, text)
			seen[text] = true
		}
	}
	c.mu.RUnlock()
	if len(missing) == 0 {
		return nil
	}
	if c.offline {
		return fmt.Errorf("%d of %d texts are not in %s and this embedder makes no requests",
			len(missing), len(texts), c.path)
	}
	fmt.Printf("embedding %d texts not already cached\n", len(missing))

	// Workers drain the queue even after one of them fails, so a provider
	// error surfaces as itself rather than as a deadlock on the send.
	var (
		failed  error
		failure sync.Mutex
		wg      sync.WaitGroup
	)
	work := make(chan string)
	for range openAIRequestLimit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for text := range work {
				failure.Lock()
				stop := failed != nil
				failure.Unlock()
				if stop {
					continue
				}
				if _, err := c.Embed(ctx, text); err != nil {
					failure.Lock()
					if failed == nil {
						failed = err
					}
					failure.Unlock()
				}
			}
		}()
	}
	for _, text := range missing {
		work <- text
	}
	close(work)
	wg.Wait()
	if err := c.save(); err != nil {
		return err
	}
	if failed != nil {
		return fmt.Errorf("prefetch: %w", failed)
	}
	return nil
}

// save writes the cache back when it grew. The file holds vectors keyed by
// text and no credential: the key reaches the provider from the
// environment and is never written anywhere.
func (c *cachingEmbedder) save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.dirty || c.path == "" {
		return nil
	}
	body, err := json.Marshal(c.vectors)
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	if err := os.WriteFile(c.path, body, 0o600); err != nil {
		return fmt.Errorf("write cache %s: %w", c.path, err)
	}
	return nil
}

// newCache reads a vector cache, tolerating a missing file.
func newCache(inner memory.Embedder, path string, offline bool) (*cachingEmbedder, error) {
	cache := &cachingEmbedder{inner: inner, path: path, offline: offline,
		vectors: map[string][]float32{}}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &cache.vectors); err != nil {
		return nil, fmt.Errorf("decode cache %s: %w", path, err)
	}
	return cache, nil
}

// NewEmbedder builds one of the measured embedders. The OpenAI key is read
// from the environment named by cmd/gagent's own default and is never
// accepted from a flag or a file the harness writes.
func NewEmbedder(name, cachePath string) (Embedder, error) {
	switch name {
	case embedderHashing:
		return Embedder{Name: name, Dims: memory.EmbeddingDim,
			Embedder: memory.NewHashingEmbedder()}, nil
	case embedderOpenAI:
		key := os.Getenv(openAIKeyEnv)
		if key == "" {
			return Embedder{}, fmt.Errorf("embedder %q: %s is not set", name, openAIKeyEnv)
		}
		client, err := openai.New(openai.Config{
			BaseURL: openAIBaseURL,
			Model:   openAIModel,
			APIKey:  key,
			Timeout: 60 * time.Second,
		})
		if err != nil {
			return Embedder{}, fmt.Errorf("embedder %q: %w", name, err)
		}
		cache, err := newCache(client, cachePath, false)
		if err != nil {
			return Embedder{}, err
		}
		return Embedder{Name: name, Embedder: cache}, nil
	case embedderOpenAI512:
		cache, err := newCache(nil, cachePath, true)
		if err != nil {
			return Embedder{}, err
		}
		if len(cache.vectors) == 0 {
			return Embedder{}, fmt.Errorf("embedder %q: %s holds no vectors", name, cachePath)
		}
		return Embedder{Name: name, Embedder: cache}, nil
	default:
		return Embedder{}, fmt.Errorf("unknown embedder %q (want %q, %q or %q)",
			name, embedderHashing, embedderOpenAI, embedderOpenAI512)
	}
}

// prefetchAll fills a cache-backed embedder before measuring; embedders
// that hold no cache do nothing.
func (e Embedder) prefetchAll(ctx context.Context, texts []string) error {
	cache, ok := e.Embedder.(*cachingEmbedder)
	if !ok {
		return nil
	}
	return cache.prefetch(ctx, texts)
}

// dimensions returns the vector width the embedder answers with, which the
// run header records beside every figure. A cache-backed embedder reads it
// off a vector it already holds rather than spending a request on it.
func (e Embedder) dimensions(ctx context.Context) (int, error) {
	if e.Dims > 0 {
		return e.Dims, nil
	}
	if cache, ok := e.Embedder.(*cachingEmbedder); ok {
		cache.mu.RLock()
		defer cache.mu.RUnlock()
		for _, vec := range cache.vectors {
			return len(vec), nil
		}
	}
	vec, err := e.Embedder.Embed(ctx, "dimension probe")
	if err != nil {
		return 0, fmt.Errorf("dimension probe: %w", err)
	}
	return len(vec), nil
}
