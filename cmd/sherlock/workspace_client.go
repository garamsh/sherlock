package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/workspace"
)

const (
	// fileCallTimeout bounds one Read, Write or List call, as the file
	// tools bound theirs.
	fileCallTimeout = 10 * time.Second

	// execDispatchMargin is what the workspace needs beyond a command's
	// own bound: dispatch, reaping a child past its deadline, replying.
	execDispatchMargin = 5 * time.Second

	// execStallTimeout bounds an Exec that names no timeout, as
	// shell_exec's does. The workspace's exec-timeout bounds the command;
	// this catches a workspace that stopped answering, and ends a command
	// an exec-timeout above it allows in exit 3.
	execStallTimeout = time.Hour
)

// failureClass is the party a failed operation stopped at (ADR 0027).
type failureClass string

const (
	classUsage       failureClass = "usage"
	classWorkspace   failureClass = "workspace"
	classUnreachable failureClass = "unreachable"
)

// operationError is a failed workspace operation. It is printed as one
// JSON object on stderr, and its class decides the exit status.
type operationError struct {
	Class   failureClass `json:"error"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
}

func (e *operationError) Error() string { return e.Message }

// exitStatus is the binary's exit status for what the command tree
// returned: 0 for no error, an operation's class, or 1 for any other
// command's error.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var op *operationError
	if !errors.As(err, &op) {
		return 1
	}
	switch op.Class {
	case classWorkspace:
		return 2
	case classUnreachable:
		return 3
	default:
		return 1
	}
}

func usageError(err error) *operationError {
	return &operationError{Class: classUsage, Message: err.Error()}
}

// callError classifies a failed call. Unavailable and DeadlineExceeded
// are the client's report that no answer came; the workspace sends
// neither, so every other status is the workspace's own answer.
func callError(err error) *operationError {
	st := status.Convert(err)
	class := classWorkspace
	if st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
		class = classUnreachable
	}
	return &operationError{Class: class, Code: st.Code().String(), Message: st.Message()}
}

// newWorkspaceOperationCommands returns the `sherlock workspace`
// subcommands that each make one call on the workspace contract and
// nothing else on this host (ADR 0027).
func newWorkspaceOperationCommands() []*cobra.Command {
	read := operationCommand("read <path>", "Print a workspace file's bytes", 1,
		func(ctx context.Context, cmd *cobra.Command, ws workspace.WorkspaceClient, args []string) error {
			ctx, cancel := context.WithTimeout(ctx, fileCallTimeout)
			defer cancel()
			resp, err := ws.Read(ctx, &workspace.ReadRequest{Path: args[0]})
			if err != nil {
				return callError(err)
			}
			_, err = cmd.OutOrStdout().Write(resp.GetContent())
			return err
		})

	write := operationCommand("write <path>", "Replace a workspace file with standard input", 1,
		func(ctx context.Context, cmd *cobra.Command, ws workspace.WorkspaceClient, args []string) error {
			content, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return usageError(fmt.Errorf("read stdin: %w", err))
			}
			ctx, cancel := context.WithTimeout(ctx, fileCallTimeout)
			defer cancel()
			if _, err := ws.Write(ctx, &workspace.WriteRequest{Path: args[0], Content: content}); err != nil {
				return callError(err)
			}
			return printJSON(cmd, struct {
				BytesWritten int `json:"bytes_written"`
			}{len(content)})
		})

	list := operationCommand("list <path>", "List a workspace directory", 1,
		func(ctx context.Context, cmd *cobra.Command, ws workspace.WorkspaceClient, args []string) error {
			ctx, cancel := context.WithTimeout(ctx, fileCallTimeout)
			defer cancel()
			resp, err := ws.List(ctx, &workspace.ListRequest{Path: args[0]})
			if err != nil {
				return callError(err)
			}
			type entry struct {
				Name string `json:"name"`
				Dir  bool   `json:"dir"`
			}
			entries := make([]entry, 0, len(resp.GetEntries()))
			for _, e := range resp.GetEntries() {
				entries = append(entries, entry{Name: e.GetName(), Dir: e.GetDir()})
			}
			return printJSON(cmd, struct {
				Entries []entry `json:"entries"`
			}{entries})
		})

	var timeout time.Duration
	var forwardStdin bool
	exec := operationCommand("exec [--timeout <duration>] [--stdin] [--] <command> [<arg>...]",
		"Run a command in the workspace, isolated as the agent's own exec is", -1,
		func(ctx context.Context, cmd *cobra.Command, ws workspace.WorkspaceClient, args []string) error {
			if len(args) == 0 {
				return usageError(errors.New("exec needs a command"))
			}
			if timeout < 0 {
				return usageError(fmt.Errorf("--timeout %s is negative", timeout))
			}
			// The isolation shell_exec gets: an empty network namespace,
			// the untrusted class, and no caller environment or workdir.
			req := &workspace.ExecRequest{Command: args[0], NoNetwork: true}
			if len(args) > 1 {
				req.Args = args[1:]
			}
			if forwardStdin {
				stdin, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return usageError(fmt.Errorf("read stdin: %w", err))
				}
				req.Stdin = stdin
			}
			bound := execStallTimeout
			if timeout > 0 {
				req.TimeoutNanos = timeout.Nanoseconds()
				bound = timeout + execDispatchMargin
			}
			ctx, cancel := context.WithTimeout(ctx, bound)
			defer cancel()
			resp, err := ws.Exec(ctx, req)
			if err != nil {
				return callError(err)
			}
			return printJSON(cmd, struct {
				Stdout   string `json:"stdout"`
				Stderr   string `json:"stderr"`
				ExitCode int    `json:"exit_code"`
				TimedOut bool   `json:"timed_out"`
			}{resp.GetStdout(), resp.GetStderr(), int(resp.GetExitCode()), resp.GetTimedOut()})
		})
	exec.Flags().DurationVar(&timeout, "timeout", 0, "the command's timeout (default: the workspace's exec-timeout)")
	exec.Flags().BoolVar(&forwardStdin, "stdin", false, "send standard input to the command")
	// Flags end at the command, so its own arguments reach it unparsed.
	exec.Flags().SetInterspersed(false)

	return []*cobra.Command{read, write, list, exec}
}

// operationCommand builds one operation subcommand: it takes nargs
// positional arguments (any number when negative), dials the resolved
// address, runs call, and prints any failure as one JSON object on
// stderr in place of cobra's error and usage text.
func operationCommand(use, short string, nargs int,
	call func(context.Context, *cobra.Command, workspace.WorkspaceClient, []string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return reportFailure(cmd, runOperation(cmd, args, nargs, call))
		},
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return reportFailure(cmd, usageError(err))
	})
	config.AddWorkspaceClientFlags(cmd.Flags())
	return cmd
}

func runOperation(cmd *cobra.Command, args []string, nargs int,
	call func(context.Context, *cobra.Command, workspace.WorkspaceClient, []string) error) error {
	if nargs >= 0 && len(args) != nargs {
		return usageError(fmt.Errorf("%s takes %d argument(s), got %d", cmd.Name(), nargs, len(args)))
	}
	cfg, err := config.ResolveWorkspaceClient(cmd.Flags())
	if err != nil {
		return usageError(err)
	}
	// The listener is loopback-only and unauthenticated (ADR 0012), so
	// the connection is plaintext, as the tools' is.
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return usageError(fmt.Errorf("workspace address %q: %w", cfg.Addr, err))
	}
	defer func() { _ = conn.Close() }()
	return call(cmd.Context(), cmd, workspace.NewWorkspaceClient(conn), args)
}

// reportFailure prints err as the operation's JSON failure on stderr.
// An error that is not an operation's is the CLI's own and takes the
// usage class: a failed write of the result to stdout, after the
// workspace performed the operation, is one (ADR 0027 §4).
func reportFailure(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	var op *operationError
	if !errors.As(err, &op) {
		op = usageError(err)
	}
	if encodeErr := json.NewEncoder(cmd.ErrOrStderr()).Encode(op); encodeErr != nil {
		return errors.Join(op, encodeErr)
	}
	return op
}

func printJSON(cmd *cobra.Command, v any) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
}
