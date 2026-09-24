package agent

import (
	"encoding/json"
	"strings"

	"github.com/garamsh/gagent/internal/tool"
)

// MemoryReadTool is the reserved tool the read step answers with. It is
// the platform's like the writeback's, never an operator's, and it is
// never routed to the tool client: the read step makes its own model
// call and reads the answer out of it.
//
// It is a tool call rather than a response format for the reason #615
// found: a provider can accept `response_format` and ignore it, and a
// read step whose answer arrives as prose has placed nothing. A response
// carrying no call is exactly that outcome, and the step reports it
// rather than guessing (ADR 0019).
const MemoryReadTool = "memory_read"

// ReadAddressSchema is what the read step answers with: the two halves
// of an address a question reaches. The restriction is absent on
// purpose — see ReadInstruction.
const ReadAddressSchema = `{
  "type": "object",
  "properties": {
    "subject": {
      "type": "string",
      "description": "Who or what the question is about."
    },
    "question": {
      "type": "string",
      "description": "The question a fact answering this one is held under, worded as the rules word it. Empty where the question asks about the subject in general."
    }
  },
  "required": ["subject", "question"],
  "additionalProperties": false
}`

// readToolDescription states the call's job. It names no rule of its
// own: every rule the read step follows is addressContract's, which the
// writeback follows too.
const readToolDescription = "Give the address this question reaches, so the memory held there can be read."

// ReadInstruction is the read step's system prompt. It is the write's
// own account of an address — addressContract, the same constant the
// writeback section embeds — read as a question rather than as a
// statement, and it carries nothing the write does not carry (ADR 0019).
//
// The framing is what makes one text serve both. The write forms an
// address from a statement; this asks for the address a statement
// answering the question would have been held at, so the rules read
// forward rather than being translated, and the step's target is
// agreement with the writeback rather than correctness of its own.
//
// It asks for no restriction. The store's read of an address with none
// answers that address and every restriction under it, which is the
// wider read this design errs toward; and asking a model to supply one
// is what collapsed whole-address accuracy in the record, where a model
// supplied a restriction on nearly every assertion that placed none
// (`docs/research/memory-frames/` §Whether the address carries the
// arity).
const ReadInstruction = `A memory holds a fact at the address of the question it answers, and that address is formed from the statement that states the fact:

` + addressContract + `
You are given a question. Call ` + "`" + MemoryReadTool + "`" + ` with the address a statement answering it would be held at: the subject it is about, and the question worded the way the rules above word it. Do not give a restriction — you are looking for the fact, not narrowing it.

Where the question asks about a subject in general rather than about one question a fact answers, give the subject and leave the question empty.`

// MemoryReadDescriptor is the reserved read tool as the model sees it.
func MemoryReadDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name:        MemoryReadTool,
		Description: readToolDescription,
		InputSchema: json.RawMessage(ReadAddressSchema),
	}
}

// ReadAddress is the address a question reached, or the absence of one.
// Subject empty means the step placed nothing; Question empty with a
// subject means it placed a subject alone (ADR 0019's three outcomes).
type ReadAddress struct {
	Subject  string
	Question string
}

// Placed reports whether the step reached an address at all. A question
// with no subject reaches nothing: the store is keyed on the subject
// first, and there is no read that does not name one.
func (a ReadAddress) Placed() bool { return a.Subject != "" }

// ParseReadAddress reads the address out of the read step's response.
// Anything that is not one of the reserved call's arguments places
// nothing, which is a stated outcome rather than a failure: a provider
// that ignores the tool and answers in prose has told the step it could
// not place the question, and the step says so rather than inventing an
// address to read.
func ParseReadAddress(resp Response) ReadAddress {
	for _, call := range resp.ToolCalls {
		if call.Name != MemoryReadTool {
			continue
		}
		var doc struct {
			Subject  string `json:"subject"`
			Question string `json:"question"`
		}
		if err := json.Unmarshal(call.Input, &doc); err != nil {
			return ReadAddress{}
		}
		return ReadAddress{
			Subject:  strings.TrimSpace(doc.Subject),
			Question: strings.TrimSpace(doc.Question),
		}
	}
	return ReadAddress{}
}
