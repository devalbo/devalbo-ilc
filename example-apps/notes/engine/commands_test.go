// notes' commands, tested through the registry — the same path every host uses,
// so a passing test means the wiring works and not merely the function.
//
// These assertions are deliberately the CLI-and-browser-agnostic half: the
// browser test in frontend/test asserts the same behaviours through the web
// tier. If one needed a tier-specific tweak, logic has leaked out of engine/.
package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devalbo/devalbo-ilc/engine/platform"

	_ "github.com/devalbo/devalbo-ilc/example-apps/notes/engine" // registers commands
	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
)

func inTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return root
}

func call(t *testing.T, method uint32, req interface{ MarshalVT() ([]byte, error) }, resp interface{ UnmarshalVT([]byte) error }) {
	t.Helper()
	body, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(method, body)
	if !r.Success {
		t.Fatalf("method %d: %s", method, r.Err)
	}
	if resp != nil {
		if err := resp.UnmarshalVT(r.Output); err != nil {
			t.Fatal(err)
		}
	}
}

// Split storage (§7.1): the record IS a readable JSON file, and that file is the
// source of truth. Asserting the file — not just the response — is what makes
// this a test of the storage model rather than of the return value.
func TestCreateWritesAReadableFile(t *testing.T) {
	root := inTempRoot(t)

	var created notesv1.CreateRecordResponse
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{
		Title: "Buy milk", Body: "two litres", CreatedAt: 1700000000,
	}, &created)

	if created.Record.Id != "buy-milk" {
		t.Errorf("id: got %q, want buy-milk", created.Record.Id)
	}
	body, err := os.ReadFile(filepath.Join(root, "records", "buy-milk.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(body, &onDisk); err != nil {
		t.Fatalf("the record is not readable JSON: %v", err)
	}
	if onDisk["title"] != "Buy milk" {
		t.Errorf("on disk: %v", onDisk)
	}
}

func TestListOpenDelete(t *testing.T) {
	inTempRoot(t)
	for _, title := range []string{"Zebra", "Apple"} {
		call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: title}, nil)
	}

	// Sorted, because ListDir sorts — unsorted directory order would differ
	// between filesystems and diverge native vs wasm.
	var list notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &list)
	if len(list.Records) != 2 || list.Records[0].Id != "apple" {
		t.Fatalf("list: %v", list.Records)
	}

	var opened notesv1.OpenRecordResponse
	call(t, notesv1.MethodOpenRecord, &notesv1.OpenRecordRequest{Id: "zebra"}, &opened)
	if opened.Record.Title != "Zebra" {
		t.Errorf("open: %v", opened.Record)
	}

	var deleted notesv1.DeleteRecordResponse
	call(t, notesv1.MethodDeleteRecord, &notesv1.DeleteRecordRequest{Id: "zebra"}, &deleted)
	if !deleted.Deleted {
		t.Error("delete reported nothing removed")
	}
	// Deleting is idempotent — "not there" is a false, not an error.
	//
	// A FRESH response struct, not the one above: UnmarshalVT MERGES, and proto3
	// omits zero values from the wire, so a `false` never arrives to overwrite a
	// previous `true`. Reusing the struct made this assert the opposite of what
	// it looked like.
	var again notesv1.DeleteRecordResponse
	call(t, notesv1.MethodDeleteRecord, &notesv1.DeleteRecordRequest{Id: "zebra"}, &again)
	if again.Deleted {
		t.Error("second delete claimed to remove something")
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	inTempRoot(t)
	body, err := (&notesv1.CreateRecordRequest{Body: "no title"}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(notesv1.MethodCreateRecord, body)
	if r.Success {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(r.Err, "title is required") {
		t.Errorf("unhelpful error: %q", r.Err)
	}
}

// The app's whole state is a filesystem tree, so the INHERITED export-fs is a
// complete backup — no notes-specific code involved.
func TestExportIsACompleteBackup(t *testing.T) {
	inTempRoot(t)
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: "Keep me"}, nil)

	bundle := platform.Execute(platform.MethodExportFs, mustMarshal(t))
	if !bundle.Success {
		t.Fatalf("export-fs: %s", bundle.Err)
	}
	if !strings.Contains(string(bundle.Output), "keep-me.json") {
		t.Error("the bundle does not contain the record")
	}
}

func mustMarshal(t *testing.T) []byte {
	t.Helper()
	// An empty ExportFsRequest: whole root, default (BFT) format.
	return nil
}
