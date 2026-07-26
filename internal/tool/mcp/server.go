package mcp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync/atomic"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/garamsh/gagent/internal/tool"
)

// Server owns one MCP server subprocess and the session to it. Its
// discovered tools stay registered after the process dies; calls against
// them then fail with ErrServerUnavailable.
type Server struct {
	name    string
	session *mcpsdk.ClientSession
	tools   []tool.Tool
	dead    atomic.Bool
}

// Start spawns the configured server subprocess with a scrubbed environment,
// performs the initialize handshake, and discovers the server's tools.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Name == "" || cfg.Command == "" {
		return nil, fmt.Errorf("start mcp server: name and command are required: %w", ErrInvalidConfig)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = scrubbedEnv(cfg.Env)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "gagent", Version: "dev"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("start mcp server %q: %w", cfg.Name, err)
	}
	tools, err := listTools(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("list tools of mcp server %q: %w", cfg.Name, errors.Join(err, session.Close()))
	}
	s := &Server{name: cfg.Name, session: session}
	for _, t := range tools {
		s.tools = append(s.tools, newTool(s, t, timeout))
	}
	go func() {
		_ = session.Wait()
		s.dead.Store(true)
	}()
	return s, nil
}

// listTools returns all tools of the connected server, following pagination.
func listTools(ctx context.Context, session *mcpsdk.ClientSession) ([]*mcpsdk.Tool, error) {
	var tools []*mcpsdk.Tool
	params := &mcpsdk.ListToolsParams{}
	for {
		res, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		tools = append(tools, res.Tools...)
		if res.NextCursor == "" {
			return tools, nil
		}
		params.Cursor = res.NextCursor
	}
}

// Name returns the configured server name.
func (s *Server) Name() string { return s.name }

// Tools returns the server's discovered tools, one tool.Tool adapter each.
func (s *Server) Tools() []tool.Tool { return s.tools }

// Close shuts the session down, terminating the server subprocess.
func (s *Server) Close() error { return s.session.Close() }
