package script

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/garamsh/gagent/internal/tool"
)

// Config is one entry of the sidecar config's tools: section — the single
// shape a script tool has from the config file to the tool server
// (docs/convention/simplicity.md: one concept, one shape). The
// mapstructure tags are how viper decodes the entry; Spec reads the
// schema files and builds the Spec at registration. Adding a script tool
// is a config change on the sidecar, not a code change (ADR 0009,
// ADR 0018).
type Config struct {
	// Name is the tool name; it must not collide with a built-in.
	Name string `mapstructure:"name"`
	// Description is the one-sentence tool summary shown to the model.
	Description string `mapstructure:"description"`
	// InputSchema is the path to the input JSON Schema file.
	InputSchema string `mapstructure:"input_schema"`
	// OutputSchema is the path to the output JSON Schema file.
	OutputSchema string `mapstructure:"output_schema"`
	// Exec describes how to run the tool.
	Exec ExecConfig `mapstructure:"exec"`
}

// ExecConfig describes how to run a script tool with the timeout still in
// seconds; Spec converts it to time.Duration when building the Spec.
// TimeoutSeconds is an explicit integer: unmarshalling a config integer
// straight into time.Duration would yield nanoseconds.
type ExecConfig struct {
	// Command is the argv array (first element is the executable).
	Command []string `mapstructure:"command"`
	// StdinFormat selects how input is fed to the process ("json").
	StdinFormat string `mapstructure:"stdin_format"`
	// StdoutFormat selects how stdout is interpreted ("json" validates
	// and returns a JSON document; anything else is returned as-is).
	StdoutFormat string `mapstructure:"stdout_format"`
	// TimeoutSeconds caps one tool run; <= 0 selects the package's
	// DefaultTimeout.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
	// Env holds extra environment entries: KEY=VALUE literals, or a bare
	// KEY resolved from the sidecar's process environment at spawn (the
	// passthrough form for secrets that must not sit in this file,
	// ADR 0003). A bare KEY whose variable is unset fails validation.
	Env []string `mapstructure:"env"`
	// Workdir is the process working directory; empty inherits the
	// server's.
	Workdir string `mapstructure:"workdir"`
}

// ValidateConfigs checks a tools: section the way registration would,
// without registering: names must not collide with a built-in or with
// each other, both schema files must be readable, and the exec command
// must be non-empty. Tool servers call it at startup — the boundary for
// config (docs/convention/runtime-safety.md §Boundaries) — so an invalid
// tools: entry fails fast with an error naming the offending tool instead
// of surfacing on the first call (ADR 0009).
func ValidateConfigs(cfgs []Config) error {
	builtins := make(map[string]struct{}, len(tool.BuiltinToolNames()))
	for _, name := range tool.BuiltinToolNames() {
		builtins[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(cfgs))
	for _, c := range cfgs {
		if _, ok := builtins[c.Name]; ok {
			return fmt.Errorf("script tool %q collides with a built-in: %w", c.Name, tool.ErrDuplicateTool)
		}
		if _, ok := seen[c.Name]; ok {
			return fmt.Errorf("script tool %q is declared twice: %w", c.Name, tool.ErrDuplicateTool)
		}
		seen[c.Name] = struct{}{}
		if len(c.Exec.Command) == 0 {
			return fmt.Errorf("script tool %q: exec.command is required", c.Name)
		}
		for _, e := range c.Exec.Env {
			if strings.Contains(e, "=") {
				continue
			}
			if _, ok := os.LookupEnv(e); !ok {
				return fmt.Errorf("script tool %q: env passthrough %q is not set in the environment", c.Name, e)
			}
		}
		if _, err := c.Spec(); err != nil {
			return fmt.Errorf("script tool %q: %w", c.Name, err)
		}
	}
	return nil
}

// Spec builds the Spec for one configured script tool, reading its JSON
// Schema documents from disk.
func (c Config) Spec() (Spec, error) {
	inputSchema, err := readSchema(c.InputSchema, "input")
	if err != nil {
		return Spec{}, err
	}
	outputSchema, err := readSchema(c.OutputSchema, "output")
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		Name:         c.Name,
		Description:  c.Description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Exec: ExecSpec{
			Command:      c.Exec.Command,
			StdinFormat:  c.Exec.StdinFormat,
			StdoutFormat: c.Exec.StdoutFormat,
			Timeout:      time.Duration(c.Exec.TimeoutSeconds) * time.Second,
			Env:          c.Exec.Env,
			Workdir:      c.Exec.Workdir,
		},
	}, nil
}

// readSchema reads a JSON Schema document from path. Some script tools
// ship one combined document of the form {"input": ..., "output": ...};
// when the document has that shape, readSchema returns the named half.
// Any other document is returned as-is.
func readSchema(path, half string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, nil
	}
	if _, ok := doc["input"]; !ok {
		return raw, nil
	}
	if _, ok := doc["output"]; !ok {
		return raw, nil
	}
	if schema, ok := doc[half]; ok {
		return schema, nil
	}
	return raw, nil
}
