package main

// dlc's own command line — built from the .proto like every other app's
// (Decision 29). `dlc` is an app like any other (AGENTS.md §3), so if it needed
// a hand-written `switch` while the template shipped a generated surface, the
// template would be teaching something dlc does not do.
//
// TWO KINDS OF VERB reach a user, and only one of them is here (Decision 30):
//
//   - IN-ENGINE verbs (`new`, `version`, `echo`, `export-fs`, …) are rpcs, so
//     their whole surface — subcommands, flags, required, help — is generated.
//     This file supplies only the renderers.
//   - TOOLCHAIN verbs (`build`, `gen`) spawn processes and inspect the machine.
//     They are NOT rpcs and never reach the engine, so they stay hand-written
//     in main.go and are attached to the command list separately.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	"github.com/devalbo/dlc-platform"
	"github.com/devalbo/dlc-platform/cli"
	"github.com/devalbo/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"
)

// app builds dlc's command line. Every dependency is an argument so the whole
// CLI can be run against a fake engine and a buffer.
func app(port platform.EnginePort, stdout, stderr io.Writer, stdin io.Reader) cli.App {
	return cli.App{
		Name:  "dlc",
		Short: "dlc — scaffold and drive ILC apps; one engine, every tier",

		// dlc's own commands PLUS the platform verbs it inherits — the same two
		// lines a scaffolded app writes. dlc claims no privileged block: its
		// ids start at 10000 like any app's.
		// Three sources, one surface: dlc's engine verbs, the platform's
		// inherited ones, and dlc's HOST-LOCAL toolchain verbs (Decision 30).
		// The last group used to bypass this entirely and was therefore missing
		// from `--help`.
		Commands: append(append(append([]clispec.Command{},
			dlcv1.DlcServiceCLI...), ilcv1.PlatformServiceCLI...), dlcv1.ToolchainServiceCLI...),

		// Behaviour for the host-local verbs. The runner refuses to build the
		// surface if a declared local command has no entry here.
		Local: toolchainVerbs,

		Port:   port,
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,

		// Tier selection is a SETUP QUESTION, not just a flag.
		//
		// `--tiers` decides which slots get scaffolded, and since the host layer
		// landed that is a consequential choice: a tier is a directory of host
		// code plus a `dlc.toml` entry that is checked to exist. Defaulting
		// silently leaves someone deleting a slot by hand.
		//
		// Host-side by Decision 28 — the engine receives a resolved NewRequest
		// and never prompts, because it also runs in a browser tab where there
		// is no terminal to prompt on. The web tier asks the same question as
		// checkboxes.
		Fill: func(cmd clispec.Command, values map[string][]string) {
			if cmd.Method == engineMethodNew && len(values["tiers"]) == 0 {
				if picked := promptTiers(stderr, stdout, stdin); len(picked) > 0 {
					values["tiers"] = picked
				}
			}
		},

		Render: map[uint32]cli.Renderer{
			dlcv1.MethodNew: render(func(out io.Writer, r *dlcv1.NewResponse) error {
				// Presentation is the host's job — the engine returned
				// structured data, and the browser renders the same response as
				// a file list.
				fmt.Fprintln(out, "scaffold "+r.GetPath())
				for _, f := range r.GetFiles() {
					fmt.Fprintln(out, "  + "+r.GetPath()+"/"+f)
				}
				fmt.Fprintln(out, "\nnext:")
				fmt.Fprintln(out, "  cd "+r.GetPath()+" && devbox shell")
				_, err := fmt.Fprintln(out, "  make gen && go mod tidy && make verify")
				return err
			}),
			dlcv1.MethodEcho: render(func(out io.Writer, r *dlcv1.EchoResponse) error {
				_, err := fmt.Fprintln(out, r.GetText())
				return err
			}),

			ilcv1.MethodVersion: render(func(out io.Writer, r *ilcv1.VersionResponse) error {
				_, err := fmt.Fprintln(out, r.GetVersion())
				return err
			}),
			ilcv1.MethodExportFs: render(func(out io.Writer, r *ilcv1.ExportFsResponse) error {
				// The bundle is the program's output, so it goes to stdout and
				// pipes: dlc export-fs --prefix myapp > myapp.bft.json
				_, err := out.Write(r.GetBundle())
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
			ilcv1.MethodResetFs: render(func(out io.Writer, r *ilcv1.ResetFsResponse) error {
				for _, rm := range r.GetRemoved() {
					if _, err := fmt.Fprintln(out, "  - "+rm); err != nil {
						return err
					}
				}
				return nil
			}),
			// dlc keeps NO index — it has no collection to list — so its engine
			// never registers this verb and the CLI marks it unavailable. The
			// renderer exists anyway because the CLI surface is generated from the
			// platform's .proto for every app that inherits it, and a declared
			// command with no renderer is a startup error by design (see
			// cli/run.go). Recorded rather than worked around: "dlc does not adopt
			// the index" is a legitimate answer (INDEX-PLAN.md Phase 5), not an
			// omission for a later reader to re-derive.
			ilcv1.MethodRebuildIndex: render(func(out io.Writer, r *ilcv1.RebuildIndexResponse) error {
				_, err := fmt.Fprintf(out, "indexed %d entries\n", r.GetEntries())
				return err
			}),
		},
	}
}

// engineMethodNew is dlc's own `new`, named here so the Fill hook above reads
// as a rule about one command rather than a magic number.
const engineMethodNew = dlcv1.MethodNew

// promptTiers asks which tiers to scaffold, and returns nothing if it cannot.
//
// ONLY WHEN INTERACTIVE, and getting that test right took two goes.
//
// A prompt with nobody there is worse than the silent default it replaces: on a
// stream nobody writes to it hangs forever, and in a log it is noise in the
// middle of a command's output. Every automated caller — verify-scaffold.sh,
// CI, anyone's script — runs `dlc new` without a person attached.
//
// TWO GUARDS, because the obvious one is not enough:
//
//  1. stdin AND stdout must both be character devices. `ModeCharDevice` alone
//     is NOT an "is a human there" test — /dev/null is a character device, so
//     `dlc new foo </dev/null` (what CI usually does) sailed past a stdin-only
//     check and printed the whole menu before the read hit EOF. Requiring
//     stdout too catches it, because an automated caller virtually always
//     captures or redirects output.
//  2. The prompt goes to STDERR. Even if the heuristic is wrong somewhere, a
//     stray menu lands in the log rather than corrupting the command's output,
//     which for `dlc export-fs` would mean a corrupted bundle.
//
// The genuinely correct test is `golang.org/x/term.IsTerminal`, an ioctl rather
// than a mode bit. It is not a dependency yet, and would bring x/sys with it —
// worth taking if this heuristic is ever found insufficient.
func promptTiers(stderr io.Writer, stdout io.Writer, stdin io.Reader) []string {
	// CI FIRST, before any terminal test. A suite run from a developer's
	// terminal has a real TTY on both ends, so the device check below passes and
	// a script that forgot `--tiers` HANGS — which is exactly what happened to
	// `verify-bundle-xtier.sh`, a caller this change missed. Honouring `CI` turns
	// that from a hang into the required-flag error, which names the fix.
	if os.Getenv("CI") != "" {
		return nil
	}
	f, ok := stdin.(*os.File)
	if !ok || !isCharDevice(f) || !isCharDevice(stdout) {
		return nil
	}

	fmt.Fprintln(stderr, "Which tiers? Each one becomes a hosts/<tier>/ slot you write code in.")
	fmt.Fprintln(stderr, "  1) native + web  (default)")
	fmt.Fprintln(stderr, "  2) native only")
	fmt.Fprintln(stderr, "  3) web only")
	fmt.Fprint(stderr, "> ")

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil {
		return nil
	}
	switch strings.TrimSpace(line) {
	case "2":
		return []string{"native"}
	case "3":
		return []string{"web"}
	default:
		// Enter, or anything unrecognised, takes the default. A setup question
		// should never be a wall.
		return []string{"native", "web"}
	}
}

// isCharDevice reports whether w is a terminal-ish device rather than a file or
// a pipe. See promptTiers for why this is checked on both ends.
func isCharDevice(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// render adapts a typed printer to the byte-level Renderer, so each printer says
// what it prints and nothing about decoding. Generics, not reflection.
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

// runCommand dispatches an in-engine verb through the generated surface.
func runCommand(args []string) int {
	return app(platform.Live, os.Stdout, os.Stderr, os.Stdin).Run(args)
}
