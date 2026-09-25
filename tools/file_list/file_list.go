// file_list lists workspace directories over the stdio tool contract.
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

	"github.com/garamsh/sherlock/internal/workspace"
)

// callTimeout bounds one workspace RPC. Read, Write and List are
// pod-local filesystem calls with nothing to wait on, so a workspace
// still answering returns far inside it.
const callTimeout = 10 * time.Second

type callInput struct {
	Path string `json:"path"`
}

type entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

type callOutput struct {
	Entries []entry `json:"entries"`
}

func main() {
	if err := run(); err != nil {
		writeError(err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("SHERLOCK_WORKSPACE_ADDR")
	if addr == "" {
		return errors.New("SHERLOCK_WORKSPACE_ADDR is required")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial workspace: %v", err)
	}
	defer conn.Close()
	ws := workspace.NewWorkspaceClient(conn)

	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	dec.DisallowUnknownFields()
	var input struct {
		Path string `json:"path"`
	}
	if err := dec.Decode(&input); err != nil {
		return fmt.Errorf("decode input: %v", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("input must be one JSON object")
	}
	if input.Path == "" {
		return errors.New("path is required")
	}
	in := callInput{Path: input.Path}
	out, err := dispatch(context.Background(), ws, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func dispatch(ctx context.Context, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := ws.List(ctx, &workspace.ListRequest{Path: in.Path})
	if err != nil {
		return callOutput{}, grpcError("list", err)
	}
	entries := make([]entry, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		entries = append(entries, entry{Name: e.GetName(), Dir: e.GetDir()})
	}
	return callOutput{Entries: entries}, nil
}

func grpcError(op string, err error) error {
	st, _ := status.FromError(err)
	if st != nil && st.Message() != "" {
		return fmt.Errorf("%s: %s", op, st.Message())
	}
	return fmt.Errorf("%s: %v", op, err)
}

func writeError(err error) {
	fmt.Fprintln(os.Stderr, "file_list:", err)
}
