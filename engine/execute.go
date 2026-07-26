// Package engine holds dlc's portable business logic. It compiles two ways
// (Decision 26): linked natively in-process by hosts/native, and as a wasip2
// component via cmd/engine-component. Execute is the single entry point both
// paths call — keep it free of WIT / cm types and build tags so it builds under
// plain `go` and TinyGo alike.
//
// Command parsing is the in-engine ff/v3/ffcli tree (Decision 22, Spike 4): one
// parser across every tier; the host only forwards argv. Everything here stays
// reflection-free — ffcli over stdlib flag, and `new` renders templates with
// plain string substitution rather than text/template (reflection-heavy under
// TinyGo — Spike 4's cobra wall).
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

// Execute builds the in-engine ffcli command tree and dispatches. Capability
// access (writing a scaffold to disk, console) lands behind the caps seam
// (caps_native.go / caps_wasip2.go, §5.3) when a command needs it; for now `new`
// renders in memory so Execute stays tag-free and parity-safe.
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
				module = "github.com/you/" + app
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

// templateFile is one file in the scaffold. A plain slice (not a map) keeps the
// output order deterministic — important for the native↔wasm parity check.
type templateFile struct {
	path    string
	content string
}

// scaffoldTemplates is the minimal self-shaped skeleton dlc emits. Tokens
// {{.AppName}} / {{.Module}} are substituted by renderScaffold. This is the
// bootstrap in-tree template; it grows into templates/component-model/ (§16.6).
var scaffoldTemplates = []templateFile{
	{"go.mod", "module {{.Module}}\n\ngo 1.23.0\n"},
	{"engine/execute.go", "package engine\n\n// {{.AppName}} engine, scaffolded by dlc.\nfunc Execute(args []string) string {\n\treturn \"hello from {{.AppName}}\"\n}\n"},
	{"README.md", "# {{.AppName}}\n\nScaffolded by `dlc new`. Module `{{.Module}}`.\n"},
}

// renderScaffold token-substitutes each template file and returns a manifest of
// what would be written. Disk writing lands with the filesystem capability seam
// (import-fs -> write tree, §7.3); rendering is the engine-logic half.
func renderScaffold(app, module string) []byte {
	var b bytes.Buffer
	b.WriteString("scaffold " + app + " (module " + module + ")\n")
	for _, t := range scaffoldTemplates {
		content := substitute(t.content, app, module)
		b.WriteString("  + " + t.path + " (" + strconv.Itoa(len(content)) + " bytes)\n")
	}
	b.WriteString("note: rendered in-memory; file writing lands with the filesystem capability\n")
	return b.Bytes()
}

// substitute is the reflection-free stand-in for text/template — safe under
// TinyGo, and enough for the two scaffold tokens.
func substitute(s, app, module string) string {
	s = strings.ReplaceAll(s, "{{.AppName}}", app)
	s = strings.ReplaceAll(s, "{{.Module}}", module)
	return s
}
