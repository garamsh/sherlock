package tool

import (
	"errors"
	"fmt"
)

// Policy is the per-tool permission policy the platform applies at the
// call boundary. It is operator-supplied: nothing about it is fixed
// in code (ADR 0018 §Tool contract). An empty policy allows every
// installed tool; a populated policy restricts to the listed names.
type Policy struct {
	// allowed is the set of tool names the platform invokes. An empty
	// set means every installed tool is allowed.
	allowed map[string]struct{}
}

// ErrPolicyDenied is returned when a call would route to a tool the
// policy forbids. The descriptor for the tool is still fetched at
// startup so the policy decision is the only gate at call time.
var ErrPolicyDenied = errors.New("tool denied by policy")

// AllowAll returns a policy that permits every installed tool name.
func AllowAll() Policy {
	return Policy{allowed: nil}
}

// AllowList returns a policy that permits only the listed tool names.
// An empty list means every call is denied; this is the fail-closed
// default for an operator who has not yet declared a tool list.
func AllowList(names ...string) Policy {
	if len(names) == 0 {
		return Policy{allowed: map[string]struct{}{}}
	}
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return Policy{allowed: out}
}

// Permitted reports whether the policy permits calling name.
func (p Policy) Permitted(name string) bool {
	if p.allowed == nil {
		return true
	}
	_, ok := p.allowed[name]
	return ok
}

// Enforce returns a typed denial when the policy forbids name.
func (p Policy) Enforce(name string) error {
	if p.Permitted(name) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPolicyDenied, name)
}
