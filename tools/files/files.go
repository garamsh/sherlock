// files is the platform's files tool. It implements the script-tool
// runner's stdio protocol (ADR 0009 §Decision): one JSON object on
// stdin (the validated input), one JSON object on stdout (the
// validated output), non-zero exit on failure. The tool dials the
// workspace gRPC client at GAGENT_WORKSPACE_ADDR and routes the
// operation (read, write, list) to the matching RPC.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gagent/internal/workspace"
)

type callInput struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

type entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

type callOutput struct {
	Content      string  `json:"content"`
	BytesWritten int     `json:"bytes_written"`
	Entries      []entry `json:"entries"`
}

func main() {
	if err := run(); err != nil {
		writeError(err)
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
	out, err := dispatch(context.Background(), ws, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func dispatch(ctx context.Context, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	switch in.Operation {
	case "read":
		resp, err := ws.Read(ctx, &workspace.ReadRequest{Path: in.Path})
		if err != nil {
			return callOutput{}, grpcError("read", err)
		}
		// BytesWritten and Entries are not produced by read, and the
		// struct carries no omitempty, so every key is emitted
		// regardless. Entries must be an empty slice rather than nil:
		// nil marshals to null, which fails the manifest's
		// "entries": {"type": "array"} at the registry's output gate
		// (issue #319).
		return callOutput{
			Content: string(resp.GetContent()),
			Entries: []entry{},
		}, nil
	case "write":
		// The input schema cannot carry this conditional under the
		// strict-mode subset (oneOf is rejected at the top level,
		// issue #313); the runtime enforces it instead.
		if in.Content == "" {
			return callOutput{}, errors.New("content is required for write")
		}
		if _, err := ws.Write(ctx, &workspace.WriteRequest{Path: in.Path, Content: []byte(in.Content)}); err != nil {
			return callOutput{}, grpcError("write", err)
		}
		return callOutput{
			BytesWritten: len(in.Content),
			Entries:      []entry{},
		}, nil
	case "list":
		resp, err := ws.List(ctx, &workspace.ListRequest{Path: in.Path})
		if err != nil {
			return callOutput{}, grpcError("list", err)
		}
		entries := make([]entry, 0, len(resp.GetEntries()))
		for _, e := range resp.GetEntries() {
			entries = append(entries, entry{Name: e.GetName(), Dir: e.GetDir()})
		}
		return callOutput{Entries: entries}, nil
	default:
		return callOutput{}, fmt.Errorf("unknown operation: %s", in.Operation)
	}
}

func grpcError(op string, err error) error {
	st, _ := status.FromError(err)
	if st != nil && st.Message() != "" {
		return fmt.Errorf("%s: %s", op, st.Message())
	}
	return fmt.Errorf("%s: %w", op, err)
}

func writeError(err error) {
	fmt.Fprintln(os.Stderr, "files:", err)
}
