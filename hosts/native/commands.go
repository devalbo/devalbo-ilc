package main

// The CLI front-end: argv in, a proto request out (Decision 28).
//
// This is the piece that makes the ILC inversion real on this tier. The engine
// exports ONE operation — `execute(method_id, request)` — and a host's job is to
// turn native input into that request. argv here, a React form on the web tier,
// a serial line on embedded. Same handlers, three front-ends.
//
// Parsing lives here and not in the engine, which is why it may use anything the
// host likes (stdlib `flag` today; cobra or `huh` menus would be equally fine).
// The engine stays reflection-free for TinyGo; this does not have to.
//
// Nothing here decides what a command MEANS. If a rule about `new` or
// `import-fs` shows up in this file, it is in the wrong place — it belongs in
// engine/ or engine/platform/, where every tier shares it.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/devalbo-ilc/engine/platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
)

// message is what both ends of the boundary satisfy.
type request interface{ MarshalVT() ([]byte, error) }
type response interface{ UnmarshalVT([]byte) error }

// call is the entire host↔engine boundary: encode, dispatch on the method id,
// decode. Errors arrive in the result envelope rather than as exceptions.
func call(method uint32, req request, resp response) error {
	body, err := req.MarshalVT()
	if err != nil {
		return err
	}
	r := engine.ExecuteMethod(method, body)
	if !r.Success {
		return errors.New(r.Err)
	}
	if resp == nil {
		return nil
	}
	return resp.UnmarshalVT(r.Output)
}

// runCommand parses argv for an in-engine verb and dispatches it.
func runCommand(args []string) error {
	if len(args) == 0 {
		return errors.New(usage())
	}
	verb, rest := args[0], args[1:]

	switch verb {
	case "version":
		var resp ilcv1.VersionResponse
		if err := call(platform.MethodVersion, &ilcv1.VersionRequest{}, &resp); err != nil {
			return err
		}
		fmt.Println(resp.Version)
		return nil

	case "echo":
		var resp dlcv1.EchoResponse
		if err := call(engine.MethodEcho, &dlcv1.EchoRequest{Args: rest}, &resp); err != nil {
			return err
		}
		fmt.Println(resp.Text)
		return nil

	case "new":
		return runNew(rest)
	case "export-fs":
		return runExportFS(rest)
	case "import-fs":
		return runImportFS(rest)
	case "reset-fs":
		return runResetFS(rest)

	case "help", "-h", "--help":
		fmt.Println(usage())
		return nil
	}
	return errors.New("unknown command " + verb + "\n\n" + usage())
}

func runNew(args []string) error {
	fs := newFlagSet("new")
	module := fs.String("module", "", "Go module path (default github.com/you/<app>)")
	platformPath := fs.String("platform-path", "", "local devalbo-ilc checkout (bootstrap: until ilc-platform is published)")
	tiers := fs.String("tiers", "", "comma-separated tiers to scaffold (default: native,web)")
	rest, err := parse(fs, args, "new [--module path] [--platform-path dir] [--tiers a,b] <project>")
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("new: expected one <app> name, flags before it (e.g. dlc new --module X myapp)")
	}

	var resp dlcv1.NewResponse
	if err := call(engine.MethodNew, &dlcv1.NewRequest{
		Name:         rest[0],
		Module:       *module,
		PlatformPath: *platformPath,
		Tiers:        splitList(*tiers),
	}, &resp); err != nil {
		return err
	}

	// Presentation is the host's job — the engine returned structured data, and
	// the browser renders the same response as a file list.
	fmt.Println("scaffold " + resp.Path)
	for _, f := range resp.Files {
		fmt.Println("  + " + filepath.Join(resp.Path, f))
	}
	fmt.Println("\nnext:")
	fmt.Println("  cd " + resp.Path + " && devbox shell")
	fmt.Println("  make gen && go mod tidy && make verify")
	return nil
}

func runExportFS(args []string) error {
	fs := newFlagSet("export-fs")
	prefix := fs.String("prefix", "", "subtree to export (default: whole root)")
	rest, err := parse(fs, args, "export-fs [--prefix path]")
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return errors.New("export-fs: unexpected argument " + rest[0] + " (use --prefix)")
	}

	var resp ilcv1.ExportFsResponse
	if err := call(platform.MethodExportFs, &ilcv1.ExportFsRequest{Prefix: *prefix}, &resp); err != nil {
		return err
	}
	// The bundle is the program's output, so it goes to stdout and pipes:
	//   dlc export-fs --prefix myapp > myapp.bft.json
	os.Stdout.Write(resp.Bundle)
	return nil
}

func runImportFS(args []string) error {
	fs := newFlagSet("import-fs")
	prefix := fs.String("prefix", "", "destination subtree")
	replace := fs.Bool("replace", false, "clear the destination first (default: merge)")
	rest, err := parse(fs, args, "import-fs [--prefix path] [--replace] <bundle.json>")
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("import-fs: expected one <bundle.json>, flags before it")
	}
	bundle, err := os.ReadFile(rest[0])
	if err != nil {
		return errors.New("import-fs: " + err.Error())
	}

	mode := ilcv1.ImportMode_IMPORT_MODE_MERGE
	if *replace {
		mode = ilcv1.ImportMode_IMPORT_MODE_REPLACE
	}
	var resp ilcv1.ImportFsResponse
	if err := call(platform.MethodImportFs, &ilcv1.ImportFsRequest{
		Bundle: bundle,
		Prefix: *prefix,
		Mode:   mode,
	}, &resp); err != nil {
		return err
	}
	for _, f := range resp.Files {
		fmt.Println("  + " + f)
	}
	return nil
}

func runResetFS(args []string) error {
	fs := newFlagSet("reset-fs")
	prefix := fs.String("prefix", "", "subtree to delete (default: whole root)")
	rest, err := parse(fs, args, "reset-fs [--prefix path]")
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return errors.New("reset-fs: unexpected argument " + rest[0] + " (use --prefix)")
	}

	var resp ilcv1.ResetFsResponse
	if err := call(platform.MethodResetFs, &ilcv1.ResetFsRequest{Prefix: *prefix}, &resp); err != nil {
		return err
	}
	for _, r := range resp.Removed {
		fmt.Println("  - " + r)
	}
	return nil
}

// splitList parses a comma-separated flag value. Empty means "unset", which the
// engine reads as "the default set" — not as "no tiers".
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render errors ourselves, with usage attached
	return fs
}

// parse applies flags and returns the positionals.
//
// stdlib `flag` stops at the first non-flag argument, so `dlc new myapp --module X`
// would silently drop the flag. Callers check the positional count, and the
// usage string always shows flags before the operand — a silent drop is the one
// outcome not allowed.
func parse(fs *flag.FlagSet, args []string, use string) ([]string, error) {
	if err := fs.Parse(args); err != nil {
		return nil, errors.New(fs.Name() + ": " + err.Error() + "\nusage: dlc " + use)
	}
	return fs.Args(), nil
}

func usage() string {
	return strings.Join([]string{
		"usage: dlc <command> [flags]",
		"",
		"  version                                    print the engine version",
		"  echo [words...]                            echo back",
		"  new [--module m] [--platform-path p] <app> scaffold a project",
		"  export-fs [--prefix p]                     bundle a tree as BFT on stdout",
		"  import-fs [--prefix p] [--replace] <file>  unpack a BFT bundle",
		"  reset-fs [--prefix p]                      delete a subtree",
		"  build <tier> [--out d] [--web-out d]       build a tier (toolchain; host-side)",
	}, "\n")
}
