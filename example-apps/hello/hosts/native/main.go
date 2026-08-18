// hello — native CLI host (the native tier slot, Decision 34).
//
// Notice what is NOT here: no `switch args[0]`, no usage string, no flag
// declarations. The command surface — which subcommands exist, what flags they
// take, which are required, what the help says — is GENERATED from
// commands.proto (Decision 29). Add an rpc and you get a subcommand; add a
// request field and you get a flag, with its help from that field's own `help`
// option.
//
// So the only things written here are the two that are genuinely this tier's:
//
//	Render  how a response is printed
//	Fill    values the user should not have to type (a clock, an id)
//
// A SLOT RENDERS; IT NEVER DECIDES. What a command MEANS lives in engine/,
// shared with the browser — which is why the terminal and a tab cannot disagree.
// If you find yourself working something out in this file, it belongs in engine/.
//
// The engine is linked in-process (no wasm runtime in the run path), which is a
// build seam and not a fork: the same engine package compiles to wasm for the
// web tier.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	"github.com/devalbo/devalbo-ilc/dlc-platform/cli"
	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"

	"github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/dlcconfig"

	_ "github.com/devalbo/devalbo-ilc/example-apps/hello/engine" // importing the engine registers its commands
	hellov1 "github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/hello/v1"
)

func main() {
	// GRANT the filesystem before anything touches it — the native equivalent of
	// the WASI preopen a browser host installs before instantiating a component.
	//
	// `./.hello/`: project-local like git, so running in two projects keeps two
	// stores, but CONFINED. That second half matters because `reset-fs` is an
	// INHERITED verb you did not write, and with the bare working directory as
	// root it would clear whatever folder the user happened to be standing in.
	//
	// Change this to "." if your app's output belongs to the USER rather than to
	// the app — that is what `dlc` itself does, because `dlc new` has to scaffold
	// where you are.
	if err := platform.Boot(platform.BootOptions{
		FSRoot:         platform.AppFSRoot(dlcconfig.Name),
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR,
		// WHERE TEXT GOES, declared rather than assumed. Every tier provides
		// `wasi:cli/stdout`, so an app cannot tell from the inside whether
		// printing reaches anyone — a badge with no screen looks identical to
		// this. Saying TERMINAL lets an app print prose confidently instead of
		// relying on CanShowText failing open.
		//
		// Cols and Rows are left UNMEASURED (zero). Go has no portable way to ask
		// a terminal its size, and a guessed 80 would be worse than no answer: an
		// app reads zero as "wrap however you like" and a wrong number as a
		// budget to format against. A host that DOES measure — a TUI, or one
		// willing to take an ioctl dependency — sets them here and re-sends the
		// manifest with a bumped revision on SIGWINCH.
		TextOutlet: ilcv1.TextOutlet_TEXT_OUTLET_TERMINAL,
	}); err != nil {
		os.Stderr.WriteString("hello: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(app(platform.Live, os.Stdout, os.Stderr, os.Stdin).Run(os.Args[1:]))
}

// app builds the command line. Every dependency is an argument, so the whole CLI
// can be tested against a fake engine and a buffer — a slot is the one part of
// an ILC app that parity cannot check, so it needs its own way to fail.
func app(port platform.EnginePort, stdout, stderr io.Writer, stdin io.Reader) cli.App {
	return cli.App{
		Name:  "hello",
		Short: "hello — one engine, a terminal and a browser",

		// This app's commands PLUS the platform's inherited verbs: version,
		// export-fs, import-fs and reset-fs arrive with the platform's schema
		// rather than being written here. Every ILC app gets them, and a bundle
		// exported here imports anywhere else the app runs.
		Commands: append(append([]clispec.Command{}, hellov1.AppServiceCLI...), ilcv1.PlatformServiceCLI...),

		Port:   port,
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,

		Render: map[uint32]cli.Renderer{
			// Prints nothing: the engine streamed this text already, and a host
			// that renders it too shows it twice. Engine prints prose for tiers
			// that cannot decode; hosts render structure for tiers that can.
			hellov1.MethodGreet: render(func(_ io.Writer, _ *hellov1.GreetResponse) error {
				return nil
			}),
			// Same reasoning: the engine already printed every tick and the
			// final word. What a RENDERER adds is the part a terminal can show
			// and a badge cannot — here, the count the app kept for us.
			hellov1.MethodCount: render(func(out io.Writer, r *hellov1.CountResponse) error {
				_, err := fmt.Fprintf(out, "(%d ticks)\n", r.GetCounted())
				return err
			}),
			// THE STRUCTURED ONE. The engine prints a human sentence for tiers
			// that cannot decode a response; this prints the same facts as
			// FIELDS, which is what a terminal is for.
			hellov1.MethodMath: render(func(out io.Writer, r *hellov1.MathResponse) error {
				if p := r.GetProblem(); p != hellov1.Problem_PROBLEM_UNSPECIFIED {
					// A PROBLEM IS NOT AN ERROR HERE EITHER. The command ran; it
					// is reporting what it found, and the exit status stays 0
					// because nothing failed.
					_, err := fmt.Fprintf(out, "%s: %s\n", r.GetExpression(), problem(p))
					return err
				}
				_, err := fmt.Fprintf(out, "%d\n", r.GetResult())
				return err
			}),
			hellov1.MethodLight: render(func(out io.Writer, r *hellov1.LightResponse) error {
				if !r.GetShown() {
					// SAID PLAINLY, because the alternative is a command that
					// prints "ok" having done nothing observable.
					_, err := fmt.Fprintln(out, "this world has no light to set")
					return err
				}
				_, err := fmt.Fprintln(out, "set")
				return err
			}),

			ilcv1.MethodVersion: render(func(out io.Writer, r *ilcv1.VersionResponse) error {
				_, err := fmt.Fprintln(out, r.GetVersion())
				return err
			}),
			ilcv1.MethodExportFs: render(func(out io.Writer, r *ilcv1.ExportFsResponse) error {
				_, err := out.Write(r.GetBundle()) // the bundle IS the output
				return err
			}),
			ilcv1.MethodImportFs: render(func(out io.Writer, r *ilcv1.ImportFsResponse) error {
				for _, f := range r.GetFiles() {
					if _, err := fmt.Fprintln(out, "  + "+f); err != nil {
						return err
					}
				}
				return nil
			}),
			// The derived index (§6.2). Call platform.SetIndexRebuilder in your
			// engine to give this verb something to do.
			ilcv1.MethodRebuildIndex: render(func(out io.Writer, r *ilcv1.RebuildIndexResponse) error {
				_, err := fmt.Fprintf(out, "indexed %d entries\n", r.GetEntries())
				return err
			}),
			ilcv1.MethodResetFs: render(func(out io.Writer, r *ilcv1.ResetFsResponse) error {
				// The engine returns the TOP-LEVEL entries it removed, not a
				// file count — `records/` is one entry holding many notes. This
				// printed "removed 1 file(s)" after deleting two, which reads
				// as though something survived. Name what was actually removed.
				removed := r.GetRemoved()
				if len(removed) == 0 {
					_, err := fmt.Fprintln(out, "nothing to remove")
					return err
				}
				for _, rm := range removed {
					if _, err := fmt.Fprintln(out, "  - "+rm); err != nil {
						return err
					}
				}
				return nil
			}),
		},
	}
}

// problem spells a Problem for a person.
//
// The ENUM is what crosses the wire and what a test asserts on; this is only how
// it reads. A host that printed `PROBLEM_DIVIDE_BY_ZERO` would be showing an
// identifier where a sentence belongs.
func problem(p hellov1.Problem) string {
	switch p {
	case hellov1.Problem_PROBLEM_DIVIDE_BY_ZERO:
		return "cannot divide by zero"
	case hellov1.Problem_PROBLEM_OVERFLOW:
		return "the numbers are too big"
	default:
		return p.String()
	}
}

// render adapts a typed printer to the byte-level Renderer, so each printer says
// what it prints and nothing about decoding. Generics, not reflection — the same
// reason the engine's typed handlers work under TinyGo.
func render[T any, PT interface {
	*T
	UnmarshalVT([]byte) error
}](print func(io.Writer, PT) error) cli.Renderer {
	return func(out io.Writer, response []byte) error {
		msg := PT(new(T))
		if err := msg.UnmarshalVT(response); err != nil {
			return err
		}
		return print(out, msg)
	}
}
