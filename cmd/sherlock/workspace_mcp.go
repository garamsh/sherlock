package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/garamsh/sherlock/internal/config"
)

// codeWorkspaceUnreachable is the JSON-RPC error code for a call the
// CLI answered with exit 3, in the server-defined range (ADR 0028 §5).
const codeWorkspaceUnreachable = -32001

// newWorkspaceMCPCommand returns `sherlock workspace mcp`: a stdio MCP
// server whose tools each run one operation subcommand of this binary
// as a child process (ADR 0028). It is not an operation itself.
func newWorkspaceMCPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the workspace operations to an MCP client over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.ResolveWorkspaceClient(cmd.Flags())
			if err != nil {
				return err
			}
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate this binary to run the workspace operations: %w", err)
			}
			server := newWorkspaceMCPServer(cliRunner{binary: self, addr: cfg.Addr})
			return server.Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	config.AddWorkspaceClientFlags(cmd.Flags())
	return cmd
}

// cliRunner runs one workspace operation subcommand of binary against
// the listener at addr.
type cliRunner struct {
	binary string
	addr   string
}

// cliRun is what one operation subcommand left behind.
type cliRun struct {
	stdout []byte
	stderr []byte
	status int
}

// run execs `workspace <operation> --workspace-addr <addr> <flags> --
// <positionals>` with stdin as the child's whole standard input, so the
// child never reads the MCP channel this process is served over.
func (r cliRunner) run(ctx context.Context, operation string, flags, positionals []string, stdin []byte) (cliRun, error) {
	argv := append([]string{"workspace", operation, "--workspace-addr", r.addr}, flags...)
	argv = append(append(argv, "--"), positionals...)
	child := exec.CommandContext(ctx, r.binary, argv...)
	child.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	err := child.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		// A child that could not start is the server's own failure, not a
		// tool error for the model (ADR 0028 §5).
		return cliRun{}, &jsonrpc.Error{Code: jsonrpc.CodeInternalError,
			Message: fmt.Sprintf("run workspace %s: %v", operation, err)}
	}
	return cliRun{stdout: stdout.Bytes(), stderr: stderr.Bytes(), status: child.ProcessState.ExitCode()}, nil
}

// answer maps a finished operation onto the MCP answer ADR 0028 §5
// fixes: exit 0 to the tool result success builds, exit 2 to a tool
// error the model sees, exit 3 to the unreachable JSON-RPC error, and
// any other exit to an internal error.
func answer(run cliRun, success func() (*mcp.CallToolResult, any, error)) (*mcp.CallToolResult, any, error) {
	failure := strings.TrimSpace(string(run.stderr))
	switch run.status {
	case 0:
		return success()
	case 2:
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: failure}}}, nil, nil
	case 3:
		return nil, nil, &jsonrpc.Error{Code: codeWorkspaceUnreachable,
			Message: "workspace unreachable: " + cliMessage(run.stderr), Data: cliObject(run.stderr)}
	default:
		return nil, nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError,
			Message: fmt.Sprintf("workspace CLI exited %d: %s", run.status, cliMessage(run.stderr)), Data: cliObject(run.stderr)}
	}
}

// cliObject is the CLI's stderr when it is the one JSON failure object,
// and nil otherwise.
func cliObject(stderr []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(stderr)
	if !json.Valid(trimmed) {
		return nil
	}
	return json.RawMessage(trimmed)
}

// cliMessage is the message of the CLI's failure object, or its stderr
// as text when it printed none.
func cliMessage(stderr []byte) string {
	var failure struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stderr), &failure); err == nil && failure.Message != "" {
		return failure.Message
	}
	return strings.TrimSpace(string(stderr))
}

// structured returns a result whose structured content, and its text
// fallback, is the CLI's JSON stdout.
func structured(stdout []byte) func() (*mcp.CallToolResult, any, error) {
	return func() (*mcp.CallToolResult, any, error) {
		return nil, json.RawMessage(bytes.TrimSpace(stdout)), nil
	}
}

type pathInput struct {
	Path string `json:"path" jsonschema:"a path relative to the workspace directory"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"a path relative to the workspace directory"`
	Content string `json:"content" jsonschema:"the file's whole new content"`
}

type execInput struct {
	Command string   `json:"command" jsonschema:"the command; run by sh -c when args is absent, otherwise the executable"`
	Args    []string `json:"args,omitempty" jsonschema:"the executable's arguments, run without a shell"`
	Stdin   *string  `json:"stdin,omitempty" jsonschema:"the command's standard input; none is sent when absent"`
	Timeout string   `json:"timeout,omitempty" jsonschema:"the command's timeout as a Go duration such as 30s; the workspace's exec-timeout applies when absent"`
}

// newWorkspaceMCPServer builds the MCP server and its four tools, one
// per workspace operation and none beyond them (ADR 0028 §4).
func newWorkspaceMCPServer(cli cliRunner) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "sherlock-workspace", Version: "1"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "read", Description: "Read a file in the Sherlock workspace."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
			run, err := cli.run(ctx, "read", nil, []string{in.Path}, nil)
			if err != nil {
				return nil, nil, err
			}
			return answer(run, func() (*mcp.CallToolResult, any, error) {
				if utf8.Valid(run.stdout) {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(run.stdout)}}}, nil, nil
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
					URI: "workspace:///" + strings.TrimPrefix(in.Path, "/"), MIMEType: "application/octet-stream", Blob: run.stdout,
				}}}}, nil, nil
			})
		})

	mcp.AddTool(server, &mcp.Tool{Name: "write", Description: "Create or replace a file in the Sherlock workspace with the given content."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in writeInput) (*mcp.CallToolResult, any, error) {
			run, err := cli.run(ctx, "write", nil, []string{in.Path}, []byte(in.Content))
			if err != nil {
				return nil, nil, err
			}
			return answer(run, structured(run.stdout))
		})

	mcp.AddTool(server, &mcp.Tool{Name: "list", Description: "List a directory in the Sherlock workspace."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
			run, err := cli.run(ctx, "list", nil, []string{in.Path}, nil)
			if err != nil {
				return nil, nil, err
			}
			return answer(run, structured(run.stdout))
		})

	mcp.AddTool(server, &mcp.Tool{Name: "exec", Description: "Run a command in the Sherlock workspace, without network, under its resource caps; the command's exit code is part of the result."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in execInput) (*mcp.CallToolResult, any, error) {
			var flags []string
			if in.Timeout != "" {
				flags = append(flags, "--timeout", in.Timeout)
			}
			var stdin []byte
			if in.Stdin != nil {
				flags = append(flags, "--stdin")
				stdin = []byte(*in.Stdin)
			}
			run, err := cli.run(ctx, "exec", flags, append([]string{in.Command}, in.Args...), stdin)
			if err != nil {
				return nil, nil, err
			}
			return answer(run, structured(run.stdout))
		})

	return server
}
