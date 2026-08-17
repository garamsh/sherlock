// Package memory implements the agent session's context hooks against
// the memory service (ADR 0006). It sits under the package declaring
// those interfaces, so the session's dependencies are constructed from
// one place and internal/agent itself keeps no dependency on the store.
//
// Two of the three hooks the session declares are here: the writeback,
// which stores what a turn stated, and the compactor, which now only
// drops the oldest messages because an addressed store has nowhere to
// put a summary of them (ADR 0018). The third, agent.Recaller, has no
// implementation: asked in words an addressed store answers nothing, and
// the step that reads a question into an address is #336 step 3.
package memory
