package main

import (
	"context"
)

// instructionTokens is the allowance the projection below gives the
// instruction a model-calling design sends with every turn, on top of the
// document it carries and the turn itself. It is the one number in the cost
// block that is assumed rather than measured, and it is named so a reader
// can replace it.
const instructionTokens = 200

// Cost is what the run spent and what a design that calls a model per turn
// would spend on the same workload.
//
// It is reported in tokens and calls rather than in money. A price list
// expires; a token count a reader can multiply by whatever a model costs
// when they read it does not. Neither frame measured here calls a model, so
// ModelCalls is zero and the run is free — which is why it could be started
// without asking.
type Cost struct {
	TurnsObserved    int `json:"turns_observed"`
	ContextsProduced int `json:"contexts_produced"`
	ModelCalls       int `json:"model_calls"`
	// EmbedCalls counts every call the frames made to the embedder, and
	// DistinctTexts how many of them were for a text not embedded before.
	// Under the hashing embedder both are free; under a hosted embedder the
	// second is what a run would be billed for.
	EmbedCalls     int `json:"embed_calls"`
	DistinctTexts  int `json:"distinct_texts_embedded"`
	EmbeddedTokens int `json:"embedded_tokens_estimated"`
	// PerTurnModel projects what one decade run of one design that rewrites
	// a bounded document with a model each turn would cost, from this run's
	// own turn count and the shipped context budget.
	PerTurnModel ModelProjection `json:"per_turn_model_projection"`
}

// ModelProjection is the projection's inputs and its result, so a reader
// can redo the arithmetic rather than trust it.
type ModelProjection struct {
	CallsPerRun         int   `json:"calls_per_run"`
	PromptTokensPerCall int   `json:"prompt_tokens_per_call"`
	OutputTokensPerCall int   `json:"output_tokens_per_call"`
	PromptTokensPerRun  int64 `json:"prompt_tokens_per_run"`
	OutputTokensPerRun  int64 `json:"output_tokens_per_run"`
}

// ProjectPerTurnModel works out what a design calling a model once per turn
// would spend over the decade. The prompt carries the document such a
// design holds, the turn it is reading, and an instruction; the output is
// the rewritten document.
func ProjectPerTurnModel(turns, meanTurnChars int) ModelProjection {
	document := documentLimit / charsPerToken
	prompt := document + meanTurnChars/charsPerToken + instructionTokens
	return ModelProjection{
		CallsPerRun:         turns,
		PromptTokensPerCall: prompt,
		OutputTokensPerCall: document,
		PromptTokensPerRun:  int64(turns) * int64(prompt),
		OutputTokensPerRun:  int64(turns) * int64(document),
	}
}

// EmbedCounter counts what the frames asked the embedder for. The harness
// runs one frame at a time, so it takes no lock.
type EmbedCounter struct {
	calls    int
	tokens   int
	distinct map[string]bool
}

// NewEmbedCounter returns a counter with nothing counted yet.
func NewEmbedCounter() *EmbedCounter {
	return &EmbedCounter{distinct: map[string]bool{}}
}

// Wrap returns the embedder with this counter behind it.
func (c *EmbedCounter) Wrap(inner Embedder) Embedder {
	return countingEmbedder{inner: inner, counter: c}
}

// Totals returns what the counter has seen.
func (c *EmbedCounter) Totals() (calls, distinct, tokens int) {
	return c.calls, len(c.distinct), c.tokens
}

type countingEmbedder struct {
	inner   Embedder
	counter *EmbedCounter
}

// Embed implements Embedder.
func (e countingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.counter.calls++
	if !e.counter.distinct[text] {
		e.counter.distinct[text] = true
		e.counter.tokens += max(len(text)/charsPerToken, 1)
	}
	return e.inner.Embed(ctx, text)
}
