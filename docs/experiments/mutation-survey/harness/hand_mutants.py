#!/usr/bin/env python3
"""The thirteen hand deletions: whole guards removed, not expressions perturbed.

gremlins' operators rewrite an expression — a comparison, an arithmetic sign — and
cannot express "delete this guard", which is the shape a security boundary fails
in and the shape the garam PM's OAuth CSRF case had. These are that shape: each
mutant removes the substance of one guard and leaves the code compiling.

M2 does not compile once its comparison is gone (the bound variable goes unused),
so M2b is the same deletion in a form the compiler accepts. M11 has no safe
textual anchor and is recorded as attempted rather than quietly dropped, because
the denominator is part of the result.

Running this rewrites tracked files under `internal/` and restores each with
`git checkout --` before the next; it refuses to start on a dirty tree.
"""
from __future__ import annotations
from mutate import survey

REGISTRY = "internal/tool/registry.go"
PROCESS = "internal/workspace/shell/process_linux.go"
STORE = "internal/memory/store.go"
SERVICE = "internal/memory/service.go"
TOOL = "./internal/tool/..."
SHELL = "./internal/workspace/shell/..."
MEMORY = "./internal/memory/..."

# id, file, package under test, what the deletion removes, anchor, replacement.
MUTANTS = [
    ("M1", REGISTRY, TOOL, "drift pin never enforced (call site deleted)",
     """		if err := r.checkPin(loaded.manifest.Name, loaded.hash); err != nil {
			return err
		}
""", ""),
    ("M2", REGISTRY, TOOL, "a drifted manifest hash is accepted",
     """	if pinned != hash {
		return fmt.Errorf("descriptor drift for %q: pinned=%s fetched=%s", name, pinned, hash)
	}
""", ""),
    ("M2b", REGISTRY, TOOL, "a drifted manifest hash is accepted (viable form)",
     """	if pinned != hash {
		return fmt.Errorf("descriptor drift for %q: pinned=%s fetched=%s", name, pinned, hash)
	}
""", "	_ = pinned\n"),
    ("M3", REGISTRY, TOOL, "a tool the pin set does not name is accepted",
     """	pinned, ok := r.pins[name]
	if !ok {
		return fmt.Errorf("unpinned tool %q: not named by the pin set; pin it as %s or remove the tool", name, hash)
	}
""", "	pinned := r.pins[name]\n"),
    ("M10", REGISTRY, TOOL, "duplicate tool names are accepted",
     """		if _, exists := r.tools[loaded.manifest.Name]; exists {
			return fmt.Errorf("duplicate tool name %q", loaded.manifest.Name)
		}
""", ""),
    ("M11", "internal/tool/manifest.go", TOOL,
     "the manifest hash is constant (drift undetectable at source)", None, None),
    ("M4", PROCESS, SHELL, "the privilege drop is removed (child keeps root)",
     """	if euid == 0 {
		// Setuid clears effective + permitted capabilities in the
		// child per capabilities(7); the bounding/inheritable sets are
		// not modified here (Go 1.26 has no SysProcAttr hook to clear
		// them before exec), but the child is no longer root so its
		// caps are inert. Single-account mode (euid != 0) needs no
		// Credential: the child inherits the process's own account.
		attr.Credential = &syscall.Credential{Uid: uint32(e.AgentUID), Gid: uint32(e.AgentUID)}
	}
""", ""),
    ("M5", PROCESS, SHELL, "the wrong-account refusal is removed",
     """	if euid != 0 && euid != e.AgentUID {
		return Result{}, ErrAgentUIDUnavailable
	}
""", ""),
    ("M6", PROCESS, SHELL, "netns isolation dropped for the privileged path",
     """	if req.NoNetwork && euid == 0 {
		// unshare runs in the child before the credential drop, while
		// CAP_SYS_ADMIN is still held: the child lands in a fresh,
		// empty network namespace (ADR 0010).
		attr.Unshareflags = syscall.CLONE_NEWNET
	}
""", ""),
    ("M12", PROCESS, SHELL,
     "netns isolation dropped for the unprivileged path (single-account)",
     """	if req.NoNetwork && euid != 0 {
		// Unprivileged network isolation (ADR 0010): a user namespace
		// with the current UID/GID mapped to themselves is creatable
		// without privileges and owns the fresh, empty network
		// namespace cloned with it.
		attr.Cloneflags = syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET
		attr.UidMappings = []syscall.SysProcIDMap{{ContainerID: euid, HostID: euid, Size: 1}}
		attr.GidMappings = []syscall.SysProcIDMap{{ContainerID: os.Getegid(), HostID: os.Getegid(), Size: 1}}
		attr.GidMappingsEnableSetgroups = false
	}
""", ""),
    ("M7", STORE, MEMORY, "a replacement supersedes nothing (behaves as a join)",
     """	var supersede []Memory
	if in.Operation == OpReplace {
		held, err := s.store.At(ctx, in.Address)
		if err != nil {
			return StoreResult{}, fmt.Errorf("read address: %w", err)
		}
		supersede = held
	}
""", "	var supersede []Memory\n"),
    ("M8", SERVICE, MEMORY, "an unknown operation is accepted",
     """	if in.Operation != OpReplace && in.Operation != OpJoin {
		return fmt.Errorf("%w: unknown operation %q", ErrInvalidInput, in.Operation)
	}
""", ""),
    ("M9", SERVICE, MEMORY, "an empty value is accepted",
     """	if in.Value == "" {
		return fmt.Errorf("%w: empty value", ErrInvalidInput)
	}
""", ""),
]

if __name__ == "__main__":
    survey(MUTANTS)
