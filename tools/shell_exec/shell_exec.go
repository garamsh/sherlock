// shell_exec is the platform's shell_exec tool. It implements the
// script-tool runner's stdio protocol (ADR 0009 §Decision): one JSON
// object on stdin (the validated input), one JSON object on stdout
// (the validated output), non-zero exit on failure. The tool dials
// the workspace gRPC client at GAGENT_WORKSPACE_ADDR and forwards
// the command to the workspace's Exec RPC.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gagent/internal/workspace"
)

const (
	// stallTimeout bounds a call that names no timeout of its own. It
	// is deliberately not exec-timeout's default: the ceiling on the
	// command is the operator's, the workspace applies it, and a copy
	// enforced from here would cut short every command a longer
	// exec-timeout allows. What is left for this deadline to catch is
	// a workspace that stopped answering, so it clears any ceiling
	// exec-timeout is given.
	stallTimeout = time.Hour

	// dispatchMargin is what the workspace needs beyond the command's
	// own bound: gRPC dispatch, killing and reaping a child whose
	// deadline expired, and writing the response.
	dispatchMargin = 5 * time.Second
)

type callInput struct {
	Command      string `json:"command"`
	TimeoutNanos int64  `json:"timeout_nanos"`
}

type callOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shell_exec:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("GAGENT_WORKSPACE_ADDR")
	if addr == "" {
		return errors.New("GAGENT_WORKSPACE_ADDR is required")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial workspace: %w", err)
	}
	defer conn.Close()
	ws := workspace.NewWorkspaceClient(conn)

	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	var in callInput
	if err := dec.Decode(&in); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode input: %w", err)
	}
	if in.Command == "" {
		return errors.New("command is required")
	}
	out, err := dispatch(context.Background(), ws, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

// dispatch forwards one command to the workspace's Exec RPC. A call
// that names its own timeout is bounded at it plus dispatchMargin,
// because the tool knows how long the command may run; a call that
// names none leaves that ceiling to the workspace and is bounded
// against the stall alone.
func dispatch(ctx context.Context, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	req := &workspace.ExecRequest{
		Command: in.Command,
	}
	bound := stallTimeout
	if in.TimeoutNanos > 0 {
		commandTimeout := time.Duration(in.TimeoutNanos)
		req.TimeoutNanos = commandTimeout.Nanoseconds()
		bound = commandTimeout + dispatchMargin
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()

	resp, err := ws.Exec(ctx, req)
	if err != nil {
		return callOutput{}, grpcError("exec", err)
	}
	return callOutput{
		Stdout:   resp.GetStdout(),
		Stderr:   resp.GetStderr(),
		ExitCode: int(resp.GetExitCode()),
		TimedOut: resp.GetTimedOut(),
	}, nil
}

func grpcError(op string, err error) error {
	st, _ := status.FromError(err)
	if st != nil && st.Message() != "" {
		return fmt.Errorf("%s: %s", op, st.Message())
	}
	return fmt.Errorf("%s: %w", op, err)
}
