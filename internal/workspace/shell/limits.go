package shell

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ShimCommand is the argv verb that runs the platform binary as the
// resource-limit shim: it applies the caps to itself and execs the
// command, so the caps are in force before the child's first
// instruction (ADR 0008).
const ShimCommand = "exec-limits"

// shimSeparator ends the cap list; the program path and its argv
// follow.
const shimSeparator = "--"

// ShimIfRequested runs this process as the resource-limit shim when its
// arguments carry the verb, and does not return in that case: the shim
// execs the child's command, or exits non-zero after reporting the cap
// it could not apply. The platform binary calls it before its command
// tree, and so does any test binary that starts a real executor — the
// executor execs whichever binary it is running as.
func ShimIfRequested() {
	if len(os.Args) < 2 || os.Args[1] != ShimCommand {
		return
	}
	if err := RunShim(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// limitSpec is one cap on the shim's command line: the kernel resource
// by name, and the value both its soft and hard limit take.
type limitSpec struct {
	name  string
	value uint64
}

// shimArgv renders the shim's command line: the verb, one name=value
// pair per cap, the separator, the resolved program path, and the argv
// the program sees.
func shimArgv(shimPath string, specs []limitSpec, path string, argv []string) []string {
	out := make([]string, 0, len(specs)+len(argv)+4)
	out = append(out, shimPath, ShimCommand)
	for _, spec := range specs {
		out = append(out, spec.name+"="+strconv.FormatUint(spec.value, 10))
	}
	out = append(out, shimSeparator, path)
	return append(out, argv...)
}

// parseShimArgv splits the shim's arguments into the caps to apply, the
// program to exec, and the argv it receives. Every malformed shape is
// an error rather than a partial cap set: a cap the shim cannot read is
// a cap the child would run without.
func parseShimArgv(args []string) (specs []limitSpec, path string, argv []string, err error) {
	for i, arg := range args {
		if arg == shimSeparator {
			rest := args[i+1:]
			if len(rest) < 2 {
				return nil, "", nil, errors.New("shim: program path and argv must follow " + shimSeparator)
			}
			return specs, rest[0], rest[1:], nil
		}
		name, value, ok := strings.Cut(arg, "=")
		if !ok || name == "" {
			return nil, "", nil, fmt.Errorf("shim: malformed limit %q", arg)
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, "", nil, fmt.Errorf("shim: limit %s: %w", name, err)
		}
		specs = append(specs, limitSpec{name: name, value: parsed})
	}
	return nil, "", nil, errors.New("shim: missing " + shimSeparator + " separator")
}
