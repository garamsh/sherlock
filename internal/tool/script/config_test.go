package script_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/script"
)

// writeConfigSchemas writes a minimal input/output JSON Schema pair into
// dir and returns their paths. The input schema satisfies the OpenAI
// function-calling subset (top-level type object, no forbidden keywords).
func writeConfigSchemas(t *testing.T, dir string) (input, output string) {
	t.Helper()
	input = filepath.Join(dir, "input.json")
	output = filepath.Join(dir, "output.json")
	require.NoError(t, os.WriteFile(input, []byte(
		`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`), 0o644))
	require.NoError(t, os.WriteFile(output, []byte(
		`{"type":"object","properties":{"count":{"type":"integer"}}}`), 0o644))
	return input, output
}

func TestValidateConfigs(t *testing.T) {
	input, output := writeConfigSchemas(t, t.TempDir())
	valid := script.Config{
		Name:         "wordcount",
		Description:  "Count words in a text.",
		InputSchema:  input,
		OutputSchema: output,
		Exec:         script.ExecConfig{Command: []string{"true"}},
	}
	colliding := valid
	colliding.Name = "files"
	missingSchema := valid
	missingSchema.Name = "noschema"
	missingSchema.InputSchema = filepath.Join(t.TempDir(), "missing.json")
	emptyCommand := valid
	emptyCommand.Name = "nocommand"
	emptyCommand.Exec = script.ExecConfig{}

	tests := []struct {
		name    string
		tools   []script.Config
		wantErr string
		wantDup bool
	}{
		{name: "no tools is valid", tools: nil},
		{name: "valid script tool passes", tools: []script.Config{valid}},
		{
			name:    "collision with a built-in fails",
			tools:   []script.Config{colliding},
			wantErr: `"files"`,
			wantDup: true,
		},
		{
			name:    "duplicate script-tool names fail",
			tools:   []script.Config{valid, valid},
			wantErr: `"wordcount"`,
			wantDup: true,
		},
		{
			name:    "missing schema file fails",
			tools:   []script.Config{missingSchema},
			wantErr: `"noschema"`,
		},
		{
			name:    "empty exec command fails",
			tools:   []script.Config{emptyCommand},
			wantErr: `"nocommand"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := script.ValidateConfigs(tt.tools)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr,
				"the error must name the offending tool")
			if tt.wantDup {
				assert.ErrorIs(t, err, tool.ErrDuplicateTool)
			}
		})
	}
}

// TestValidateConfigs_EnvPassthrough pins the startup boundary for the
// ADR 0003 custody channel: a bare KEY entry whose variable is unset
// fails validation naming the tool and the key; a set variable passes,
// and KEY=VALUE literals are unchanged.
func TestValidateConfigs_EnvPassthrough(t *testing.T) {
	input, output := writeConfigSchemas(t, t.TempDir())
	cfg := script.Config{
		Name:         "gh-tool",
		Description:  "GitHub tooling.",
		InputSchema:  input,
		OutputSchema: output,
		Exec: script.ExecConfig{
			Command: []string{"true"},
			Env:     []string{"GH_TOKEN", "PLAIN=literal"},
		},
	}

	err := script.ValidateConfigs([]script.Config{cfg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"gh-tool"`, "the error names the tool")
	assert.Contains(t, err.Error(), "GH_TOKEN", "the error names the unset key")

	t.Setenv("GH_TOKEN", "s3cret")
	assert.NoError(t, script.ValidateConfigs([]script.Config{cfg}),
		"a set passthrough variable and a literal both pass")
}

func TestConfig_SpecSchemaDocument(t *testing.T) {
	dir := t.TempDir()
	combined := filepath.Join(dir, "combined.json")
	require.NoError(t, os.WriteFile(combined, []byte(
		`{"input":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},`+
			`"output":{"type":"object","properties":{"title":{"type":"string"}}}}`), 0o644))
	plain := filepath.Join(dir, "plain.json")
	require.NoError(t, os.WriteFile(plain, []byte(
		`{"type":"object","properties":{"text":{"type":"string"}}}`), 0o644))

	tests := []struct {
		name       string
		schemaPath string
		wantInput  string
		wantOutput string
	}{
		{
			name:       "combined document returns the named half",
			schemaPath: combined,
			wantInput:  `{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`,
			wantOutput: `{"type":"object","properties":{"title":{"type":"string"}}}`,
		},
		{
			name:       "plain document passes through as-is",
			schemaPath: plain,
			wantInput:  `{"type":"object","properties":{"text":{"type":"string"}}}`,
			wantOutput: `{"type":"object","properties":{"text":{"type":"string"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := script.Config{
				Name:         "fetch",
				Description:  "Fetch a page.",
				InputSchema:  tt.schemaPath,
				OutputSchema: tt.schemaPath,
				Exec:         script.ExecConfig{Command: []string{"true"}},
			}.Spec()
			require.NoError(t, err)

			assert.JSONEq(t, tt.wantInput, string(spec.InputSchema))
			assert.JSONEq(t, tt.wantOutput, string(spec.OutputSchema))
		})
	}
}
