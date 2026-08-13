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
	req := &workspace.ExecRequest{
		Command:   in.Command,
		NoNetwork: true,
	}
	if in.TimeoutNanos > 0 {
		req.TimeoutNanos = time.Duration(in.TimeoutNanos).Nanoseconds()
	}
	resp, err := ws.Exec(context.Background(), req)
	if err != nil {
		return grpcError("exec", err)
	}
	out := callOutput{
		Stdout:   resp.GetStdout(),
		Stderr:   resp.GetStderr(),
		ExitCode: int(resp.GetExitCode()),
		TimedOut: resp.GetTimedOut(),
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func grpcError(op string, err error) error {
	st, _ := status.FromError(err)
	if st != nil && st.Message() != "" {
		return fmt.Errorf("%s: %s", op, st.Message())
	}
	return fmt.Errorf("%s: %w", op, err)
}
