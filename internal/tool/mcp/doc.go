// Package mcp adapts external MCP servers (stdio transport) to the tool.Tool
// contract: it starts server subprocesses, discovers their tools, and
// forwards calls. See docs/architecture/security.md for the trust model.
package mcp
