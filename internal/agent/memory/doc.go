// Package memory implements the agent session's three memory hooks —
// agent.Recaller, agent.WritebackSink and agent.Compactor — against the
// memory service (ADR 0006). It sits under the package declaring those
// interfaces, so the session's dependencies are constructed from one
// place and internal/agent itself keeps no dependency on the store.
package memory
