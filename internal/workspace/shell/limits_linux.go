//go:build linux

package shell

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// shimStatusFD is the descriptor the shim reports on. The parent passes
// the write end as the child's first extra file, so a message means the
// caps were not applied and the command never ran; silence means the
// shim reached exec, which closes the descriptor. The exit status
// cannot carry this: after exec it belongs to the child's command,
// which could forge any value.
const shimStatusFD = 3

// shimResources maps the names on the shim's command line to the kernel
// resources they cap.
var shimResources = map[string]int{
	"cpu":    unix.RLIMIT_CPU,
	"as":     unix.RLIMIT_AS,
	"fsize":  unix.RLIMIT_FSIZE,
	"nofile": unix.RLIMIT_NOFILE,
	"nproc":  unix.RLIMIT_NPROC,
}

// RunShim applies the caps named in args to this process and execs the
// program after the separator, replacing itself with the child's real
// command. Lowering one's own limits needs no capability, while
// prlimit(2) against a child that has already dropped to the exec UID
// needs CAP_SYS_RESOURCE — which is why the caps are applied here and
// not by the parent (ADR 0008). A cap that cannot be applied fails
// before exec, so the command never runs uncapped.
func RunShim(args []string) error {
	err := runShim(args)
	reportShimFailure(err)
	return err
}

func runShim(args []string) error {
	specs, path, argv, err := parseShimArgv(args)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		resource, ok := shimResources[spec.name]
		if !ok {
			return fmt.Errorf("shim: unknown limit %q", spec.name)
		}
		limit := syscall.Rlimit{Cur: spec.value, Max: spec.value}
		// syscall.Setrlimit, not unix.Setrlimit: the standard library
		// raises RLIMIT_NOFILE at startup and restores the original
		// across exec unless its own setter cleared that bookkeeping,
		// which would silently undo the open-file cap.
		if err := syscall.Setrlimit(resource, &limit); err != nil {
			return fmt.Errorf("shim: setrlimit %s=%d: %w", spec.name, spec.value, err)
		}
	}
	// Silence on the status descriptor is the parent's success signal,
	// so the descriptor must close exactly when the command starts.
	unix.CloseOnExec(shimStatusFD)
	return syscall.Exec(path, argv, os.Environ())
}

// reportShimFailure writes the reason to the parent's status descriptor
// and to stderr. A nil error writes nothing: the parent reads an empty
// descriptor as "the caps are in force".
func reportShimFailure(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	status := os.NewFile(shimStatusFD, "shim-status")
	if status == nil {
		return
	}
	defer status.Close()
	fmt.Fprint(status, err.Error())
}
