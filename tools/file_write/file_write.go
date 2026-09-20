// file_write creates or replaces workspace files over the stdio tool contract.
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

// callTimeout bounds one workspace RPC. Read, Write and List are
// pod-local filesystem calls with nothing to wait on, so a workspace
// still answering returns far inside it.
const callTimeout = 10 * time.Second

type callInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type callOutput struct {
	BytesWritten int `json:"bytes_written"`
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
		return fmt.Errorf("dial workspace: %v", err)
	}
	defer conn.Close()
	ws := workspace.NewWorkspaceClient(conn)

	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	dec.DisallowUnknownFields()
	var input struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
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
	if input.Content == nil {
		return errors.New("content is required")
	}
	in := callInput{Path: input.Path, Content: *input.Content}
	out, err := dispatch(context.Background(), ws, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func dispatch(ctx context.Context, ws workspace.WorkspaceClient, in callInput) (callOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if _, err := ws.Write(ctx, &workspace.WriteRequest{Path: in.Path, Content: []byte(in.Content)}); err != nil {
		return callOutput{}, grpcError("write", err)
	}
	return callOutput{BytesWritten: len(in.Content)}, nil
}

func grpcError(op string, err error) error {
	st, _ := status.FromError(err)
	if st != nil && st.Message() != "" {
		return fmt.Errorf("%s: %s", op, st.Message())
	}
	return fmt.Errorf("%s: %v", op, err)
}

func writeError(err error) {
	fmt.Fprintln(os.Stderr, "file_write:", err)
}
