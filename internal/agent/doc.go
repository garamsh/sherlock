// Package agent runs the reasoning loop for one rolling session per
// agent: model call, tool-call decision, execution, result
// incorporation, repeat — led by the agent's memory files and ended by
// the writeback. All model access goes through the narrow
// Model interface (ADR 0004): the deterministic ScriptedModel for offline
// runs and tests, the OpenAI-compatible provider in internal/agent/openai/
// for real chat models.
package agent
