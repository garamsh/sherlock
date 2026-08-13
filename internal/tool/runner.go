package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner is the platform's stdio runner (ADR 0009 §Decision). It spawns
// the tool's executable, writes one JSON line to stdin, reads one JSON
// line from stdout, and returns the validated output or the failure
// details. The runner is stateless: each Call spawns a new process.
type Runner struct {
	manifest Manifest
	workdir  string
}

// NewRunner returns a stdio runner for the given manifest. The runner
// resolves command paths relative to workdir; the manifest stores the
// command's path relative to the manifest file (ADR 0009 §Decision).
func NewRunner(manifest Manifest, workdir string) *Runner {
	return &Runner{manifest: manifest, workdir: workdir}
}

// Run dispatches one call to the tool: it spawns the executable,
// writes the validated input as one JSON line to stdin, reads one JSON
// line from stdout, and validates the response against the output
// schema. On a non-zero exit or a missing error object the captured
// stderr is returned as the failure detail. The output schema is
// re-validated by the registry after Run returns — Run only enforces
// the structural "one JSON object per line" contract.
func (r *Runner) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, string, error) {
	if r.manifest.Command == "" {
		return nil, "", errors.New("runner: command is empty")
	}
	cmd := exec.CommandContext(ctx, resolveCommand(r.workdir, r.manifest.Command))
	cmd.Env = append(os.Environ(), r.manifest.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("open stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", fmt.Errorf("open stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("spawn: %w", err)
	}

	// Write the input as one JSON line, then close stdin so the
	// tool sees EOF and knows the request is complete. A broken-pipe
	// error here means the tool exited before reading the input —
	// the exit status is the real signal, so the write error is
	// surfaced only when the tool also exited successfully.
	if err := writeJSONLine(stdin, input); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		_ = killAndWait(cmd)
		return nil, "", fmt.Errorf("write input: %w", err)
	}
	if err := stdin.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		_ = killAndWait(cmd)
		return nil, "", fmt.Errorf("close stdin: %w", err)
	}

	// Capture stderr in the background so a tool that prints
	// diagnostics before exiting does not block on a full pipe.
	stderrBuf := &bytes.Buffer{}
	stderrDone := readAll(stderr, stderrBuf)

	// Read one JSON line from stdout. The contract is one line; any
	// trailing data is ignored — the tool is not a stream protocol.
	var line []byte
	var readErr error
	line, readErr = readJSONLine(stdout)
	stderrDone()

	// waitErr captures the exit status. A non-zero exit is the
	// dominant signal: a tool that exits with an error before
	// writing its JSON line is reporting failure via the exit
	// status, not the protocol. The platform surfaces the exit
	// status to the caller.
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, stderrBuf.String(), waitErr
	}
	if readErr != nil {
		return nil, stderrBuf.String(), readErr
	}

	var response json.RawMessage
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, stderrBuf.String(), fmt.Errorf("decode output: %w", err)
	}
	return response, stderrBuf.String(), nil
}

// ReadErrorObject checks whether the response object carries an "error"
// field (ADR 0009 §Decision). When the tool communicates a failure as a
// JSON object on stdout, the failure is surfaced through the same
// execution-error path the platform uses for non-zero exits.
func ReadErrorObject(response json.RawMessage) (string, bool) {
	if len(response) == 0 || !bytes.HasPrefix(bytes.TrimSpace(response), []byte("{")) {
		return "", false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(response, &doc); err != nil {
		return "", false
	}
	raw, ok := doc["error"]
	if !ok {
		return "", false
	}
	var msg string
	if err := json.Unmarshal(raw, &msg); err == nil {
		return msg, true
	}
	return string(raw), true
}

// writeJSONLine writes one JSON object as a single line followed by a
// newline. json.Encoder appends a newline automatically; the tool
// reads the next line off stdin.
func writeJSONLine(w io.Writer, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	enc := json.NewEncoder(w)
	return enc.Encode(payload)
}

// readJSONLine reads one line from r and returns the trimmed bytes
// without the trailing newline. io.EOF before any byte is reported as
// an error; a blank line is reported as an empty payload.
func readJSONLine(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read stdout: %w", err)
	}
	if len(line) == 0 {
		return nil, io.EOF
	}
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, errors.New("empty output line")
	}
	return line, nil
}

// readAll drains r into buf and returns a function that blocks until
// the drain finishes. The caller waits on the returned function before
// using buf.
func readAll(r io.Reader, buf *bytes.Buffer) func() {
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return func() { <-done }
}

// killAndWait terminates the process and reaps it. Used on the error
// paths where the child is still running and the platform must not
// leak it.
func killAndWait(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	return cmd.Wait()
}

// resolveCommand resolves the manifest's command path against the
// manifest's directory. Absolute paths are passed through; relative
// paths are joined with workdir.
func resolveCommand(workdir, command string) string {
	if filepath.IsAbs(command) {
		return command
	}
	return filepath.Join(workdir, command)
}
