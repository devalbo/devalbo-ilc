// notes — native CLI host (the native tier slot, Decision 34).
//
// There is no `switch args[0]` here, and that is the point. The command surface
// — which subcommands exist, what flags they take, which are required, what the
// help says — is GENERATED from commands.proto (Decision 29), so adding an rpc
// adds a subcommand and nothing has to be hand-mirrored. The last hand-written
// `switch` was a second place for the command surface to live and a second place
// for it to be wrong.
//
// What is left in this file is exactly two things, and both are presentation:
//
//	Render  how a response is printed
//	Fill    values the user should not have to type (the clock)
//
// A SLOT RENDERS; IT NEVER DECIDES. Nothing here works out what a command means
// — that lives in engine/, shared with the browser, which is why a note created
// here reads identically in a tab.
//
// The engine is linked in-process (no wasm runtime in the run path), which is a
// build seam and not a fork: the same engine package compiles to wasm for the
// web tier.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/dlcconfig"
	"github.com/devalbo/dlc-platform"
	"github.com/devalbo/dlc-platform/cli"
	"github.com/devalbo/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"

	_ "github.com/devalbo/devalbo-ilc/example-apps/notes/engine" // importing the engine registers its commands
	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
)

func main() {
	// GRANT the filesystem before anything can touch it — the native equivalent
	// of the WASI preopen a browser host installs before instantiating.
	//
	// `./.notes/`: project-local like git, so running in two projects keeps
	// two stores, but CONFINED — which matters because `reset-fs` is inherited
	// and would otherwise clear whatever directory you happened to be in.
	if err := platform.Boot(platform.BootOptions{
		Root:           platform.AppRoot(dlcconfig.Name),
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR,
	}); err != nil {
		os.Stderr.WriteString("notes: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(app(platform.Live, os.Stdout, os.Stderr, os.Stdin, time.Now).Run(os.Args[1:]))
}

// app builds the command line. Every dependency is an argument so the whole CLI
// can be run against a fake engine, a buffer, and a fixed clock — a slot is the
// one part of an ILC app parity cannot check, so it needs its own way to fail.
func app(port platform.EnginePort, stdout, stderr io.Writer, stdin io.Reader, now func() time.Time) cli.App {
	return cli.App{
		Name:  "notes",
		Short: "notes — one JSON file per record, the same engine in a terminal and a browser",

		// The app's own commands PLUS the platform's inherited verbs. This is
		// what an app gets for free by being an ILC app: version, export-fs,
		// import-fs and reset-fs are not written here, they arrive with the
		// platform's schema. Previously each was a hand-written case.
		Commands: append(append([]clispec.Command{}, notesv1.NotesServiceCLI...), ilcv1.PlatformServiceCLI...),

		Port:   port,
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,

		// The HOST supplies the clock. The engine has no clock capability — it
		// runs in a browser tab and on devices without an RTC — so "now" is
		// native input, exactly like argv. Filled rather than prompted for,
		// because no user should have to type an epoch.
		Fill: func(cmd clispec.Command, values map[string][]string) {
			if cmd.Method == notesv1.MethodCreateRecord && len(values["created-at"]) == 0 {
				values["created-at"] = []string{fmt.Sprint(now().Unix())}
			}
		},

		Render: map[uint32]cli.Renderer{
			notesv1.MethodCreateRecord: render(func(out io.Writer, r *notesv1.CreateRecordResponse) error {
				_, err := fmt.Fprintf(out, "created %s -> %s\n", r.Record.GetId(), r.GetPath())
				return err
			}),
			notesv1.MethodListRecords: render(func(out io.Writer, r *notesv1.ListRecordsResponse) error {
				if len(r.Records) == 0 {
					_, err := fmt.Fprintln(out, "(no notes)")
					return err
				}
				// A HEADER, and a body excerpt — matching the terminal. Printing
				// bare id and title read as a duplicated column, because the id
				// is slugged from the title and the two are usually identical.
				if _, err := fmt.Fprintf(out, "%-24s %-24s %s\n", "ID", "TITLE", "BODY"); err != nil {
					return err
				}
				for _, rec := range r.Records {
					if _, err := fmt.Fprintf(out, "%-24s %-24s %s\n",
						rec.GetId(), rec.GetTitle(), excerpt(rec.GetBody())); err != nil {
						return err
					}
				}
				return nil
			}),
			notesv1.MethodOpenRecord: render(func(out io.Writer, r *notesv1.OpenRecordResponse) error {
				_, err := fmt.Fprintf(out, "# %s\n\n%s\n", r.Record.GetTitle(), r.Record.GetBody())
				return err
			}),
			notesv1.MethodDeleteRecord: render(func(out io.Writer, r *notesv1.DeleteRecordResponse) error {
				if !r.GetDeleted() {
					// Not an error: deleting nothing is a legitimate outcome, and
					// the engine already said so in the response.
					_, err := fmt.Fprintln(out, "no such note")
					return err
				}
				_, err := fmt.Fprintln(out, "deleted")
				return err
			}),

			// The inherited verbs. Only their PRINTING is ours — every record is
			// a plain JSON file, so a bundle is a complete backup of the app.
			ilcv1.MethodVersion: render(func(out io.Writer, r *ilcv1.VersionResponse) error {
				_, err := fmt.Fprintln(out, r.GetVersion())
				return err
			}),
			ilcv1.MethodExportFs: render(func(out io.Writer, r *ilcv1.ExportFsResponse) error {
				_, err := out.Write(r.GetBundle()) // the bundle IS the output; no decoration
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
			// notes is the app the index exists for, but it does not maintain one
			// YET (INDEX-PLAN.md Phase 3) — so this renderer is here and the verb
			// is marked unavailable until create/list/delete start using it. The
			// count is the whole response on purpose: it distinguishes "the
			// rebuild did nothing" from "the collection is empty".
			ilcv1.MethodRebuildIndex: render(func(out io.Writer, r *ilcv1.RebuildIndexResponse) error {
				_, err := fmt.Fprintf(out, "indexed %d note(s)\n", r.GetEntries())
				return err
			}),
		},
	}
}

// excerpt is the first line of a body, shortened — a list is an index, not the
// content. `open <id>` is what shows the whole thing.
func excerpt(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	const max = 40
	if len(line) > max {
		return line[:max-1] + "…"
	}
	return line
}

// render adapts a typed printer to the byte-level Renderer, so each printer
// above says what it prints and nothing about decoding. Generics, not
// reflection — the same reason the engine's typed handlers work under TinyGo.
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
