package playwright_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/tooltest"
	"github.com/garamsh/gagent/tools/playwright"
)

// schemaFile mirrors the embed in tools/playwright/playwright.go so
// the test reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "playwright", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(playwright.InputSchema()) == 0 {
		t.Fatal("playwright.InputSchema returned empty bytes")
	}
	if len(playwright.OutputSchema()) == 0 {
		t.Fatal("playwright.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, _ := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(playwright.CallInput{}))
}

func TestPlaywright_Accessors(t *testing.T) {
	pt := playwright.New()
	assert.Equal(t, "playwright", pt.Name())
	assert.Contains(t, pt.Description(), "browser")
	assert.NotEmpty(t, pt.InputSchema())
	assert.NotEmpty(t, pt.OutputSchema())
}

func TestPlaywright_NavigateRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>hi from playwright</h1></body></html>"))
	}))
	defer srv.Close()

	pt := playwright.New()
	out, err := pt.Call(context.Background(), json.RawMessage(
		`{"url":"`+srv.URL+`","action":"navigate"}`,
	))
	require.NoError(t, err)
	assert.Contains(t, string(out), "hi from playwright")
}

func TestPlaywright_ExtractTextStripsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><p>hello</p><p>world</p></body></html>"))
	}))
	defer srv.Close()

	pt := playwright.New()
	out, err := pt.Call(context.Background(), json.RawMessage(
		`{"url":"`+srv.URL+`","action":"extract_text"}`,
	))
	require.NoError(t, err)
	assert.Contains(t, string(out), "hello")
	assert.Contains(t, string(out), "world")
	assert.False(t, strings.Contains(string(out), "<p>"))
}

func TestPlaywright_ClickRequiresSDK(t *testing.T) {
	pt := playwright.New()
	out, err := pt.Call(context.Background(), json.RawMessage(
		`{"url":"https://example.com","action":"click","selector":"a.button"}`,
	))
	require.NoError(t, err, "the call returns a structured result, not a Go error")
	assert.Contains(t, string(out), `"ok":false`,
		"click returns a structured not-ok result until the browser SDK lands")
	assert.Contains(t, string(out), "playwright")
}

func TestPlaywright_UnknownActionIsStructuredFailure(t *testing.T) {
	pt := playwright.New()
	out, err := pt.Call(context.Background(), json.RawMessage(
		`{"url":"https://example.com","action":"frobnicate"}`,
	))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"ok":false`)
	assert.Contains(t, string(out), "unknown action")
}

func TestPlaywright_InvalidInputRejectedAtBoundary(t *testing.T) {
	pt := playwright.New()
	tests := []struct {
		name  string
		input string
	}{
		{name: "missing url", input: `{"action":"navigate"}`},
		{name: "missing action", input: `{"url":"https://example.com"}`},
		{name: "additional field", input: `{"url":"https://example.com","action":"navigate","mode":"fast"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), pt, json.RawMessage(tt.input))
			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}
