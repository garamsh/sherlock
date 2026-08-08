// Package agent runs the reasoning loop for one rolling session per
// environment: model call, tool-call decision, execution, result
// incorporation, repeat — framed by the memory lifecycle (frame, recall,
// act, writeback) of ADR 0006. All model access goes through the narrow
// Model interface (ADR 0004): the deterministic ScriptedModel for offline
// runs and tests, the OpenAI-compatible provider in internal/agent/openai/
// for real chat models.
package agent
