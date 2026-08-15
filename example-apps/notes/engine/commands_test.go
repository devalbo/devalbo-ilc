// notes' commands, tested through the registry — the same path every host uses,
// so a passing test means the wiring works and not merely the function.
//
// These assertions are deliberately the CLI-and-browser-agnostic half: the
// browser test in hosts/web/test asserts the same behaviours through the web
// tier. If one needed a tier-specific tweak, logic has leaked out of engine/.
package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"

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
	// GRANT the root, as a host does. There is no implicit "wherever you are
	// standing" any more: `FSRoot()` panics without a grant, because falling back
	// to the cwd is what let `reset-fs` clear a user's directory.
	if err := platform.SetFSRoot("."); err != nil {
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

// assertIndexMatchesFiles is D4 (docs/INDEX-PLAN.md): the MAINTAINED index
// equals a REBUILT one.
//
// One assertion for the whole class this design is exposed to — a create that
// forgets to index, a delete that leaves a row, a projection that drifts from
// the record it projects. It needs no second tier, no second backend and no
// golden file, which is why it is cheap enough to run after every mutation
// rather than once in a dedicated test.
//
// It compares through `list`, deliberately: that is the observable an app
// actually serves, so a divergence is stated in the terms a user would see.
func assertIndexMatchesFiles(t *testing.T) {
	t.Helper()

	var maintained notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &maintained)

	call(t, platform.MethodRebuildIndex, &ilcv1.RebuildIndexRequest{}, nil)

	var rebuilt notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &rebuilt)

	if len(maintained.Entries) != len(rebuilt.Entries) {
		t.Fatalf("index drifted: maintained %v, rebuilt %v", ids(maintained.Entries), ids(rebuilt.Entries))
	}
	for i := range maintained.Entries {
		m, r := maintained.Entries[i], rebuilt.Entries[i]
		if m.GetId() != r.GetId() || m.GetTitle() != r.GetTitle() ||
			m.GetBodyPreview() != r.GetBodyPreview() || m.GetCreatedAt() != r.GetCreatedAt() {
			t.Fatalf("entry %d drifted: maintained %v, rebuilt %v", i, m, r)
		}
	}
}

func ids(entries []*notesv1.RecordEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.GetId())
	}
	return out
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

	assertIndexMatchesFiles(t)

	// Sorted, and now sorted IN GO over the index rather than by directory
	// order. Same answer, from a projection instead of two file opens.
	var list notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &list)
	if len(list.Entries) != 2 || list.Entries[0].GetId() != "apple" {
		t.Fatalf("list: %v", ids(list.Entries))
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
	assertIndexMatchesFiles(t)
}

// Events (§6.3) — what the engine ANNOUNCES is part of what a command does.
//
// Tested here rather than only in the browser because the emission is engine
// code: every tier inherits whatever this asserts. The web test covers the other
// half, that a host can actually receive it.
// The topic is a LITERAL ON PURPOSE, and must stay one.
//
// Everywhere else the string is generated from the `(topic)` option, which is
// the point — the emit side and every subscriber read one declaration. A test
// that also read that declaration would compare a generated value to itself and
// assert nothing. This is the independent pin, the same role the parse vectors
// play for request bytes.
func TestMutationsEmitRecordChanged(t *testing.T) {
	inTempRoot(t)

	type event struct {
		topic   string
		payload notesv1.RecordChangedEvent
	}
	var got []event
	platform.SetEventSink(func(topic string, payload []byte) {
		var e notesv1.RecordChangedEvent
		if err := e.UnmarshalVT(payload); err != nil {
			t.Errorf("event %q carried an undecodable payload: %v", topic, err)
			return
		}
		got = append(got, event{topic, e})
	})
	t.Cleanup(func() { platform.SetEventSink(nil) })

	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: "Buy milk"}, nil)
	// Reads announce nothing. An event per list would make every subscriber that
	// re-lists on an event loop forever.
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, nil)
	call(t, notesv1.MethodDeleteRecord, &notesv1.DeleteRecordRequest{Id: "buy-milk"}, nil)
	// A delete that removed nothing is not a change.
	call(t, notesv1.MethodDeleteRecord, &notesv1.DeleteRecordRequest{Id: "buy-milk"}, nil)

	want := []event{
		{"notes.record-changed", notesv1.RecordChangedEvent{Id: "buy-milk", Method: notesv1.MethodCreateRecord}},
		{"notes.record-changed", notesv1.RecordChangedEvent{Id: "buy-milk", Method: notesv1.MethodDeleteRecord}},
	}
	if len(got) != len(want) {
		t.Fatalf("emitted %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].topic != want[i].topic ||
			got[i].payload.Id != want[i].payload.Id ||
			got[i].payload.Method != want[i].payload.Method {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The record must be on disk BEFORE the event goes out, or a subscriber that
// re-reads on it finds nothing there — the failure that makes an event worse
// than no event at all.
func TestEventArrivesAfterTheWrite(t *testing.T) {
	root := inTempRoot(t)

	var seenAtEmit []byte
	platform.SetEventSink(func(string, []byte) {
		// Read the store from INSIDE the sink: this is the moment a host would
		// forward, and whatever is on disk now is what a subscriber can see.
		seenAtEmit, _ = os.ReadFile(filepath.Join(root, "records", "buy-milk.json"))
	})
	t.Cleanup(func() { platform.SetEventSink(nil) })

	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: "Buy milk"}, nil)
	if !strings.Contains(string(seenAtEmit), `"Buy milk"`) {
		t.Fatalf("the record was not readable when the event fired: %q", seenAtEmit)
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

// UPDATE IS THE COMMAND THE INDEX HAS BEEN WAITING FOR. Create adds a
// projection and delete removes one; only this can leave an existing projection
// disagreeing with the record it projects. So the D4 assertion here is doing
// real work rather than repeating the create case.
func TestUpdateKeepsTheIndexHonest(t *testing.T) {
	inTempRoot(t)
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{
		Title: "Buy milk", Body: "two litres",
	}, nil)

	var updated notesv1.UpdateRecordResponse
	call(t, notesv1.MethodUpdateRecord, &notesv1.UpdateRecordRequest{
		Id: "buy-milk", Title: "Buy oat milk",
	}, &updated)
	if !updated.GetChanged() {
		t.Fatal("update reported no change")
	}
	assertIndexMatchesFiles(t)

	// The LIST must show the new title, which is the projection having been
	// rewritten rather than merely still existing.
	var list notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &list)
	if len(list.Entries) != 1 || list.Entries[0].GetTitle() != "Buy oat milk" {
		t.Fatalf("list did not follow the edit: %v", list.Entries)
	}
	// …and the id did NOT move with the title. Re-slugging would turn an edit
	// into a move: a new file, a stale one, an index key to migrate.
	if list.Entries[0].GetId() != "buy-milk" {
		t.Fatalf("the id followed the title: %q", list.Entries[0].GetId())
	}
}

// ABSENT MEANS UNCHANGED, which proto3 cannot say with a bare string — so
// emptying a body takes a flag. Without that, every caller fixing a typo in a
// title would silently erase the body.
func TestUpdateLeavesOmittedFieldsAlone(t *testing.T) {
	inTempRoot(t)
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{
		Title: "Buy milk", Body: "two litres",
	}, nil)

	call(t, notesv1.MethodUpdateRecord, &notesv1.UpdateRecordRequest{
		Id: "buy-milk", Title: "Buy oat milk",
	}, nil)

	var opened notesv1.OpenRecordResponse
	call(t, notesv1.MethodOpenRecord, &notesv1.OpenRecordRequest{Id: "buy-milk"}, &opened)
	if opened.Record.GetBody() != "two litres" {
		t.Fatalf("the body was lost by a title-only update: %q", opened.Record.GetBody())
	}

	// And emptying it is possible, but only on purpose.
	call(t, notesv1.MethodUpdateRecord, &notesv1.UpdateRecordRequest{
		Id: "buy-milk", ClearBody: true,
	}, nil)
	var cleared notesv1.OpenRecordResponse
	call(t, notesv1.MethodOpenRecord, &notesv1.OpenRecordRequest{Id: "buy-milk"}, &cleared)
	if cleared.Record.GetBody() != "" {
		t.Fatalf("clear-body did not clear it: %q", cleared.Record.GetBody())
	}
	assertIndexMatchesFiles(t)
}

// An update that changes nothing writes nothing and announces nothing. An event
// here would make every subscriber that re-lists on one do a full refresh for a
// stray keystroke.
func TestNoOpUpdateIsSilent(t *testing.T) {
	inTempRoot(t)
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: "Buy milk"}, nil)

	var seen []string
	platform.SetEventSink(func(topic string, _ []byte) { seen = append(seen, topic) })
	t.Cleanup(func() { platform.SetEventSink(nil) })

	var resp notesv1.UpdateRecordResponse
	call(t, notesv1.MethodUpdateRecord, &notesv1.UpdateRecordRequest{
		Id: "buy-milk", Title: "Buy milk",
	}, &resp)
	if resp.GetChanged() {
		t.Fatal("re-setting the same title reported a change")
	}
	if len(seen) != 0 {
		t.Fatalf("a no-op update emitted %v", seen)
	}
}

func TestUpdateRequiresAnExistingRecord(t *testing.T) {
	inTempRoot(t)
	body, err := (&notesv1.UpdateRecordRequest{Id: "never-existed", Title: "x"}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if r := platform.Execute(notesv1.MethodUpdateRecord, body); r.Success {
		t.Fatal("updating a record that does not exist succeeded")
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
	// …and NOT the index (D5). A backup carrying a projection would restore one
	// built from someone else's files, and would make two identical stores
	// compare unequal.
	if strings.Contains(string(bundle.Output), platform.IndexDir) {
		t.Errorf("the index travelled in the bundle: %s", bundle.Output)
	}
}

// The index is DISPOSABLE, and this is what that means in practice: delete it
// out from under a running app and one inherited command puts it back.
//
// Note what is not needed — no migration, no repair mode, no version check. The
// answer to "what does the index say" is always "rebuild it and see".
func TestRebuildRepairsADeletedIndex(t *testing.T) {
	root := inTempRoot(t)
	for _, title := range []string{"Zebra", "Apple"} {
		call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: title}, nil)
	}

	if err := os.RemoveAll(filepath.Join(root, platform.IndexDir)); err != nil {
		t.Fatal(err)
	}
	// Gone means gone: an empty list, not an error. That is the honest state of
	// a store nothing has written to.
	var empty notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &empty)
	if len(empty.Entries) != 0 {
		t.Fatalf("a deleted index still listed %v", ids(empty.Entries))
	}

	var rebuilt ilcv1.RebuildIndexResponse
	call(t, platform.MethodRebuildIndex, &ilcv1.RebuildIndexRequest{}, &rebuilt)
	if rebuilt.GetEntries() != 2 {
		t.Fatalf("rebuilt %d entries, want 2", rebuilt.GetEntries())
	}

	var list notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &list)
	if len(list.Entries) != 2 || list.Entries[0].GetTitle() != "Apple" {
		t.Fatalf("after rebuild: %v", ids(list.Entries))
	}
}

// D6: the index holds what a LIST renders, and `open` reads the record's own
// file. A body longer than the projection's cap proves the two are different
// reads rather than the same one — the list is truncated, the record is whole.
func TestOpenReadsTheFileNotTheIndex(t *testing.T) {
	inTempRoot(t)
	body := strings.Repeat("x", 500)
	call(t, notesv1.MethodCreateRecord, &notesv1.CreateRecordRequest{Title: "Long", Body: body}, nil)

	var list notesv1.ListRecordsResponse
	call(t, notesv1.MethodListRecords, &notesv1.ListRecordsRequest{}, &list)
	if got := len(list.Entries[0].GetBodyPreview()); got != 200 {
		t.Fatalf("the projection stored %d bytes of body; it must be bounded", got)
	}

	var opened notesv1.OpenRecordResponse
	call(t, notesv1.MethodOpenRecord, &notesv1.OpenRecordRequest{Id: "long"}, &opened)
	if opened.Record.GetBody() != body {
		t.Fatalf("open returned %d bytes; the record's own file has %d", len(opened.Record.GetBody()), len(body))
	}
}

func mustMarshal(t *testing.T) []byte {
	t.Helper()
	// An empty ExportFsRequest: whole root, default (BFT) format.
	return nil
}
