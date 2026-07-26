// Package engine holds dlc's portable business logic. It compiles two ways
// (Decision 26): linked natively in-process by hosts/native, and as a wasip2
// component via cmd/engine-component. Keep it free of WIT / cm types and build
// tags so it builds under plain `go` and TinyGo alike.
//
// There are two entry points, and only one of them is the destination:
//
//   - ExecuteMethod(method, request) — the real boundary (Decisions 28/29/31).
//     A scalar method_id plus proto-encoded request bytes; dispatch is a
//     registry map lookup. Command *parsing* is host-side, so the engine never
//     sees argv.
//   - Execute(args) — the bootstrap argv shim. The parity harness and the
//     native host still ride on it; it retires once the hosts build requests.
//
// Everything here stays reflection-free: ffcli over stdlib flag in the shim, and
// `new` renders templates with plain string substitution rather than
// text/template (reflection-heavy under TinyGo — Spike 4's cobra wall).
package engine

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"strconv"
	"strings"

	"github.com/peterbourgon/ff/v3/ffcli"
)

const version = "dlc 0.0.0-bootstrap"

// Result is the host-neutral outcome of a command. The wasip2 entrypoint maps it
// to the WIT command-result; the native host consumes it directly. An empty Err
// means success-with-no-error (maps to option<string> = none).
type Result struct {
	Success bool
	Output  []byte
	Err     string
}

// ExecuteMethod dispatches one command: method_id → registry → the handler
// decodes `request` as its own message type and returns response bytes in the
// command-result envelope (Decision 28). An unknown id is an error result, not a
// panic — hosts and engines version independently.
func ExecuteMethod(method uint32, request []byte) Result {
	h, ok := lookup(method)
	if !ok {
		return Result{Err: "unknown method_id " + strconv.FormatUint(uint64(method), 10)}
	}
	return h(request)
}

// Execute is the bootstrap argv shim (see the package doc): it builds the
// in-engine ffcli command tree and dispatches. Its commands call the same
// scaffold/version logic the registry handlers do, so the two paths cannot
// drift while both exist.
func Execute(args []string) Result {
	var out bytes.Buffer

	versionCmd := &ffcli.Command{
		Name:       "version",
		ShortUsage: "version",
		Exec: func(context.Context, []string) error {
			out.WriteString(version + "\n")
			return nil
		},
	}

	echoCmd := &ffcli.Command{
		Name:       "echo",
		ShortUsage: "echo [args...]",
		Exec: func(_ context.Context, args []string) error {
			out.WriteString(strings.Join(args, " ") + "\n")
			return nil
		},
	}

	// dlc new [--module path] <app>
	// Flags must precede <app>: stdlib flag (which ffcli uses) stops parsing at
	// the first non-flag arg, so `new myapp --module X` would drop the flag. We
	// require exactly one positional and error otherwise — never a silent drop.
	newFS := flag.NewFlagSet("new", flag.ContinueOnError)
	newFS.SetOutput(io.Discard)
	newModule := newFS.String("module", "", "Go module path (default github.com/you/<app>)")
	newCmd := &ffcli.Command{
		Name:       "new",
		ShortUsage: "new [--module path] <app>",
		FlagSet:    newFS,
		Exec: func(_ context.Context, args []string) error {
			if len(args) == 0 {
				return errors.New("new: missing <app> name")
			}
			if len(args) > 1 {
				return errors.New("new: expected one <app> name, flags before it (e.g. dlc new --module X myapp)")
			}
			app := args[0]
			module := *newModule
			if module == "" {
				module = defaultModule(app)
			}
			out.Write(renderScaffold(app, module))
			return nil
		},
	}

	root := &ffcli.Command{
		Subcommands: []*ffcli.Command{versionCmd, echoCmd, newCmd},
		Exec: func(context.Context, []string) error {
			return errors.New("no command (try: dlc version | dlc new <app>)")
		},
	}

	if err := root.ParseAndRun(context.Background(), args); err != nil {
		return Result{Err: err.Error()}
	}
	return Result{Success: true, Output: out.Bytes()}
}
