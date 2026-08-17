// Package anthropic implements agent.Model against Anthropic's Messages
// API. It is a second wire protocol rather than a second vendor: the
// OpenAI-compatible client cannot reach it, because the request shape,
// the system prompt's position and the tool-call encoding all differ.
package anthropic
