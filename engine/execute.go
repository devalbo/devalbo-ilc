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
	"os"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
	"github.com/peterbourgon/ff/v3/ffcli"
)

// Result is the dispatch envelope. Aliased from the platform so hosts keep
// importing one package (this one) while the type is owned where the registry
// lives.
type Result = platform.Result

// ExecuteMethod dispatches one command through the platform registry. Importing
// this package is what registers dlc's commands (see commands.go's init), so a
// host that links the app gets the app's verbs plus the platform's.
func ExecuteMethod(method uint32, request []byte) Result {
	return platform.Execute(method, request)
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
			root, module, files, err := scaffold(args[0], *newModule)
			if err != nil {
				return err
			}
			out.Write(renderManifest(root, module, files))
			return nil
		},
	}

	// export-fs [--prefix p] — the bundle goes to stdout, so it pipes:
	//   dlc export-fs --prefix myapp > myapp.bft.json
	exportFS := flag.NewFlagSet("export-fs", flag.ContinueOnError)
	exportFS.SetOutput(io.Discard)
	exportPrefix := exportFS.String("prefix", "", "subtree to export (default: whole root)")
	exportCmd := &ffcli.Command{
		Name:       "export-fs",
		ShortUsage: "export-fs [--prefix path]",
		FlagSet:    exportFS,
		Exec: func(_ context.Context, args []string) error {
			if len(args) > 0 {
				return errors.New("export-fs: unexpected argument " + args[0] + " (use --prefix)")
			}
			resp, err := platformExport(*exportPrefix)
			if err != nil {
				return err
			}
			out.Write(resp.Bundle)
			return nil
		},
	}

	// import-fs <bundle.json> [--prefix p] — the inverse; `dlc new` is the same
	// operation with a template bundle (§7.3).
	importFS := flag.NewFlagSet("import-fs", flag.ContinueOnError)
	importFS.SetOutput(io.Discard)
	importPrefix := importFS.String("prefix", "", "destination subtree")
	importReplace := importFS.Bool("replace", false, "clear the destination first (default: merge)")
	importCmd := &ffcli.Command{
		Name:       "import-fs",
		ShortUsage: "import-fs [--prefix path] <bundle.json>",
		FlagSet:    importFS,
		Exec: func(_ context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("import-fs: expected one <bundle.json>, flags before it")
			}
			bundle, err := os.ReadFile(args[0])
			if err != nil {
				return errors.New("import-fs: " + err.Error())
			}
			resp, err := platformImport(bundle, *importPrefix, *importReplace)
			if err != nil {
				return err
			}
			for _, f := range resp.Files {
				out.WriteString("  + " + f + "\n")
			}
			return nil
		},
	}

	root := &ffcli.Command{
		Subcommands: []*ffcli.Command{versionCmd, echoCmd, newCmd, exportCmd, importCmd},
		Exec: func(context.Context, []string) error {
			return errors.New("no command (try: dlc version | dlc new <app>)")
		},
	}

	if err := root.ParseAndRun(context.Background(), args); err != nil {
		return Result{Err: err.Error()}
	}
	return Result{Success: true, Output: out.Bytes()}
}

// The argv shim reaches platform verbs the same way any caller does — through
// the registry, by method_id — rather than by importing their handlers. That
// keeps one dispatch path, and it is the shape a scaffolded app's CLI will copy.

func platformExport(prefix string) (*ilcv1.ExportFsResponse, error) {
	req, err := (&ilcv1.ExportFsRequest{Prefix: prefix}).MarshalVT()
	if err != nil {
		return nil, err
	}
	r := platform.Execute(platform.MethodExportFs, req)
	if !r.Success {
		return nil, errors.New(r.Err)
	}
	var resp ilcv1.ExportFsResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		return nil, err
	}
	return &resp, nil
}

func platformImport(bundle []byte, prefix string, replace bool) (*ilcv1.ImportFsResponse, error) {
	mode := ilcv1.ImportMode_IMPORT_MODE_MERGE
	if replace {
		mode = ilcv1.ImportMode_IMPORT_MODE_REPLACE
	}
	req, err := (&ilcv1.ImportFsRequest{Bundle: bundle, Prefix: prefix, Mode: mode}).MarshalVT()
	if err != nil {
		return nil, err
	}
	r := platform.Execute(platform.MethodImportFs, req)
	if !r.Success {
		return nil, errors.New(r.Err)
	}
	var resp ilcv1.ImportFsResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		return nil, err
	}
	return &resp, nil
}
