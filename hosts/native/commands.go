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
	"fmt"
	"io"
	"os"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	"github.com/devalbo/devalbo-ilc/engine/platform/cli"
	"github.com/devalbo/devalbo-ilc/engine/platform/clispec"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
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
		Commands: append(append([]clispec.Command{}, dlcv1.DlcServiceCLI...), ilcv1.PlatformServiceCLI...),

		Port:   port,
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,

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
		},
	}
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
