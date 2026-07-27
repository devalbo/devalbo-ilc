// notes — native CLI host.
//
// The host's job is to turn native input into a REQUEST and hand it to the
// engine; it holds no business logic. That is the ILC inversion: argv is one way
// to build a request, a React form is another, a serial REPL a third — and all
// three reach the same handlers.
//
// The engine is linked in-process here (no wasm runtime in the run path), which
// is a build seam, not a fork: the same engine package compiles to wasm for the
// browser tier.
//
// Parsing lives HERE, not in the engine — so this file may use any parser you
// like (cobra, kong, huh menus). It is deliberately stdlib-only to start.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"

	_ "github.com/devalbo/devalbo-ilc/example-apps/notes/engine" // importing the engine registers its commands
	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	switch args[0] {
	case "version":
		var resp ilcv1.VersionResponse
		must(call(platform.MethodVersion, &ilcv1.VersionRequest{}, &resp))
		fmt.Println(resp.Version)

	case "create":
		if len(args) < 2 {
			fail("create <title> [body]")
		}
		body := ""
		if len(args) > 2 {
			body = strings.Join(args[2:], " ")
		}
		// The HOST supplies the clock. The engine has no clock capability — it
		// runs in a browser tab and on a device without an RTC — so "now" is
		// native input, exactly like argv.
		req := &notesv1.CreateRecordRequest{
			Title:     args[1],
			Body:      body,
			CreatedAt: time.Now().Unix(),
		}
		var resp notesv1.CreateRecordResponse
		must(call(notesv1.MethodCreateRecord, req, &resp))
		fmt.Printf("created %s -> %s\n", resp.Record.Id, resp.Path)

	case "list":
		var resp notesv1.ListRecordsResponse
		must(call(notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &resp))
		if len(resp.Records) == 0 {
			fmt.Println("(no notes)")
		}
		for _, r := range resp.Records {
			fmt.Printf("%-24s %s\n", r.Id, r.Title)
		}

	case "open":
		if len(args) != 2 {
			fail("open <id>")
		}
		var resp notesv1.OpenRecordResponse
		must(call(notesv1.MethodOpenRecord, &notesv1.OpenRecordRequest{Id: args[1]}, &resp))
		fmt.Printf("# %s\n\n%s\n", resp.Record.Title, resp.Record.Body)

	case "delete":
		if len(args) != 2 {
			fail("delete <id>")
		}
		var resp notesv1.DeleteRecordResponse
		must(call(notesv1.MethodDeleteRecord, &notesv1.DeleteRecordRequest{Id: args[1]}, &resp))
		if resp.Deleted {
			fmt.Println("deleted " + args[1])
		} else {
			fmt.Println("no such note: " + args[1])
		}

	// export-fs / import-fs come from the platform — and because every record is
	// a plain JSON file, a bundle IS a complete backup of the app's state.
	case "export-fs":
		var resp ilcv1.ExportFsResponse
		must(call(platform.MethodExportFs, &ilcv1.ExportFsRequest{}, &resp))
		os.Stdout.Write(resp.Bundle)

	case "import-fs":
		if len(args) != 2 {
			fail("import-fs <bundle.json>")
		}
		bundle, err := os.ReadFile(args[1])
		if err != nil {
			fail(err.Error())
		}
		var resp ilcv1.ImportFsResponse
		must(call(platform.MethodImportFs, &ilcv1.ImportFsRequest{Bundle: bundle}, &resp))
		for _, f := range resp.Files {
			fmt.Println("  + " + f)
		}

	default:
		usage()
		os.Exit(2)
	}
}

// call is the whole host↔engine boundary: encode the request, dispatch on the
// method id, decode the response. Errors arrive in the result envelope.
func call(method uint32, req interface{ MarshalVT() ([]byte, error) }, resp interface{ UnmarshalVT([]byte) error }) error {
	request, err := req.MarshalVT()
	if err != nil {
		return err
	}
	r := platform.Execute(method, request)
	if !r.Success {
		return fmt.Errorf("%s", r.Err)
	}
	return resp.UnmarshalVT(r.Output)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: notes <version|create <title> [body]|list|open <id>|delete <id>|export-fs|import-fs <bundle>>")
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "notes: "+msg)
	os.Exit(1)
}
