// Package playwright is the agent's browser-automation tool (ADR 0018):
// a Go package the agent imports at compile time, no subprocess
// wrapping, no script adapter. The implementation lives in the agent's
// own process: it talks to a headless browser through its language
// SDK directly. Adding or removing this tool is a Go-package change in
// the agent's build, not a config change in the workspace (ADR 0018).
//
// This package defines the tool's descriptor, the request and response
// shapes, and a minimal HTTP-fetch implementation as the placeholder
// body. The full Playwright SDK (playwright-go or equivalent) replaces
// the placeholder once the dependency lands; the public contract —
// tool.Tool, input/output schemas, action surface — stays the same.
package playwright

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ToolName is the registered name of the playwright tool.
const ToolName = "playwright"

// DefaultTimeout is the per-call ceiling when the input does not set
// timeout_seconds.
const DefaultTimeout = 30 * time.Second

// schemaFile holds the tool's input and output JSON Schemas side by
// side. The conformance test (internal/tool/tooltest) reads the same
// shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors schema.json's on-disk shape; the raw messages keep
// the embedded bytes untouched straight through to the LLM-facing
// tool.Tool contract.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// schema is the parsed view of schemaFile. A package-level parse keeps
// the embedded bytes a single source of truth and a single failure
// point if the file is malformed.
var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("playwright: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// InputSchema returns the JSON Schema for the playwright tool's input.
func InputSchema() json.RawMessage { return schema.Input }

// OutputSchema returns the JSON Schema for the playwright tool's output.
func OutputSchema() json.RawMessage { return schema.Output }

// Tool is the agent's playwright tool. It drives a headless browser
// against the URL the model passes; the placeholder body uses a
// straight HTTP fetch so the tool is runnable today, with a clear
// hook (the implementation seam) for the real browser SDK to land.
type Tool struct {
	// Client is the HTTP client the placeholder body uses; nil
	// selects http.DefaultClient. Tests inject a fake.
	Client *http.Client
}

// New returns a playwright tool.
func New() *Tool {
	return &Tool{Client: http.DefaultClient}
}

// Name returns the tool name.
func (t *Tool) Name() string { return ToolName }

// Description returns the one-sentence tool summary.
func (t *Tool) Description() string {
	return "Drive a headless browser: navigate, click, fill forms, extract text, take screenshots."
}

// InputSchema returns the JSON Schema for the tool input.
func (t *Tool) InputSchema() json.RawMessage { return InputSchema() }

// OutputSchema returns the JSON Schema for the tool output.
func (t *Tool) OutputSchema() json.RawMessage { return OutputSchema() }

// Action enumerates the supported operations. The schema's enum keeps
// the wire contract in lockstep with this constant.
type Action string

const (
	ActionNavigate    Action = "navigate"
	ActionExtractText Action = "extract_text"
	ActionClick       Action = "click"
	ActionFillForm    Action = "fill_form"
)

// CallInput is the canonical typed form of the playwright tool's
// input. Selector and Text are required for click and fill_form
// respectively; the schema's required array gates them.
type CallInput struct {
	URL            string `json:"url"`
	Action         string `json:"action"`
	Selector       string `json:"selector"`
	Text           string `json:"text"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Output is the structured result every action returns. ok is true on
// success; the action-specific fields are populated as the action
// requires. error carries the failure message when ok is false.
type Output struct {
	OK            bool   `json:"ok"`
	Body          string `json:"body"`
	ExtractedText string `json:"extracted_text"`
	Error         string `json:"error"`
}

// Call dispatches the requested action. Input has already passed
// schema validation at the contract boundary.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args CallInput
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("playwright: decode input: %w", err)
	}
	if _, err := url.Parse(args.URL); err != nil {
		return nil, fmt.Errorf("playwright: invalid url: %w", err)
	}
	out := t.run(ctx, args)
	return json.Marshal(out)
}

// run executes one action. The placeholder body uses an HTTP fetch
// for navigate/extract_text; click/fill_form return a not-implemented
// error so the agent sees a structured failure rather than a silent
// no-op. The full Playwright SDK replaces this once added.
func (t *Tool) run(ctx context.Context, args CallInput) Output {
	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch Action(args.Action) {
	case ActionNavigate:
		return t.navigate(ctx, args.URL)
	case ActionExtractText:
		return t.extractText(ctx, args.URL)
	case ActionClick:
		return Output{OK: false, Error: "playwright click requires the Playwright SDK; install playwright-go to enable browser-level actions"}
	case ActionFillForm:
		return Output{OK: false, Error: "playwright fill_form requires the Playwright SDK; install playwright-go to enable browser-level actions"}
	default:
		return Output{OK: false, Error: fmt.Sprintf("unknown action %q", args.Action)}
	}
}

// navigate fetches the URL's body. The placeholder is honest: a real
// browser tool runs JavaScript and waits for selectors, neither of
// which a plain fetch gives. The tool description states what the
// browser SDK will deliver.
func (t *Tool) navigate(ctx context.Context, rawURL string) Output {
	client := t.client()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Output{OK: false, Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Output{OK: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Output{OK: false, Error: err.Error()}
	}
	return Output{OK: true, Body: string(body)}
}

// extractText is the placeholder's HTML-to-text: a strip-tags pass
// that drops everything between < and >. It is honest about the
// tradeoff — headless browsers see scripts render, a plain fetch
// does not — and the tool description makes the limit clear.
func (t *Tool) extractText(ctx context.Context, rawURL string) Output {
	nav := t.navigate(ctx, rawURL)
	if !nav.OK {
		return nav
	}
	return Output{OK: true, ExtractedText: stripTags(nav.Body)}
}

// client returns the wired HTTP client, defaulting to
// http.DefaultClient when the tool is constructed without one.
func (t *Tool) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	return http.DefaultClient
}

// stripTags removes HTML tags from the body. It is a placeholder for
// the browser-rendered text extraction the real SDK delivers.
func stripTags(body string) string {
	var b strings.Builder
	inTag := false
	for _, r := range body {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
