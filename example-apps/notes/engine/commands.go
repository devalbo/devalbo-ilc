// Package engine holds notes' business logic — all of it.
//
// SPLIT STORAGE (§7.1) in practice: each record is ONE canonical-JSON file under
// `records/`, and that file is the source of truth. There is no database here.
//
// The DERIVED INDEX (docs/INDEX-PLAN.md) sits beside those files: a projection
// of what a list view renders, maintained on every write and thrown away
// whenever it is doubted. It is not a second source of truth and it never
// travels in a bundle. Note what is absent — there is no branch anywhere in this
// file for "this tier has no index", because the index is a projection the
// engine owns and its floor is a file, so it exists wherever `records/` does.
package engine

import (
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/devalbo/dlc-platform"
	"github.com/devalbo/dlc-platform/index"

	"github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/dlcconfig"

	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
)

// No version string here: it lives in dlc.toml and arrives as generated code.

// recordsDir holds one JSON file per record, relative to the host-bound root.
const recordsDir = "records"

// records is the index over recordsDir, keyed by record id.
//
// Opened at init and not lazily: Open only composes a path, so there is nothing
// to fail at that can be fixed by waiting, and a nil index reached from three
// handlers is a worse failure than a loud one here.
var records = mustIndex("records")

func mustIndex(name string) *index.Index {
	ix, err := index.Open(name)
	if err != nil {
		panic("notes: opening the " + name + " index: " + err.Error())
	}
	return ix
}

func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())
	// The platform owns `rebuild-index`; notes owns the knowledge of what to
	// scan. This call is also what registers the verb — an app with no index
	// does not get one.
	platform.SetIndexRebuilder(rebuildIndex)

	platform.RegisterRaw(notesv1.NotesServiceHandlers(
		handleCreateRecord,
		handleListRecords,
		handleOpenRecord,
		handleDeleteRecord,
	))
}

func handleCreateRecord(req *notesv1.CreateRecordRequest) (*notesv1.CreateRecordResponse, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("create-record: title is required")
	}
	// The id is derived from the title so the filename is legible in a directory
	// listing and in a BFT bundle — the whole point of files-as-truth is that a
	// human can read the store without the app.
	id := slug(req.Title)
	if id == "" {
		return nil, errors.New("create-record: title has no usable characters")
	}

	record := &notesv1.Record{
		Id:        id,
		Title:     req.Title,
		Body:      req.Body,
		CreatedAt: req.CreatedAt,
	}
	// FILE FIRST, INDEX SECOND, EVENT LAST (D7). A subscriber that re-lists on
	// the event must find both already consistent; and if this dies in between,
	// the truth is on disk with only the derived thing behind — which is exactly
	// what `rebuild-index` repairs.
	if err := writeRecord(record); err != nil {
		return nil, err
	}
	if err := indexRecord(record); err != nil {
		return nil, err
	}
	emitRecordChanged(id, notesv1.MethodCreateRecord)
	return &notesv1.CreateRecordResponse{
		Record: record,
		Path:   filepath.Join(recordsDir, id+".json"),
	}, nil
}

// handleListRecords answers from the index. ONE code path, on every tier.
//
// There is no `HasIndex()` check, no degraded mode and no fallback to a scan —
// not because the fallback was deleted, but because there is nothing for it to
// fall back from: the index's floor is a file on the filesystem this app already
// has. The scan still exists; it moved to rebuildIndex, which is the one
// operation whose job is reconstructing a projection from the source of truth.
//
// This is also where an ORDERING lives — in Go, over a materialized slice. That
// was the whole argument for SQL, and moving it here is what let the index stop
// being a query engine.
func handleListRecords(*notesv1.ListRecordsRequest) (*notesv1.ListRecordsResponse, error) {
	pairs, err := records.Entries()
	if err != nil {
		return nil, errors.New("list-records: " + err.Error())
	}
	resp := &notesv1.ListRecordsResponse{}
	for _, p := range pairs {
		var entry notesv1.RecordEntry
		if err := entry.UnmarshalVT(p.Value); err != nil {
			// A projection that will not decode is a corrupt index, not a corrupt
			// record — so say which, and name the repair. Anything else sends
			// someone looking through their notes for damage that is not there.
			return nil, errors.New("list-records: the index is unreadable (run rebuild-index): " + err.Error())
		}
		resp.Entries = append(resp.Entries, &entry)
	}
	// By id, which is what the directory listing gave before and what the tests
	// and both slots expect. Sorting here rather than trusting the store is the
	// point of D2: a KV store does not promise an order, so the app states one.
	sort.Slice(resp.Entries, func(i, j int) bool { return resp.Entries[i].GetId() < resp.Entries[j].GetId() })
	return resp, nil
}

// rebuildIndex reconstructs the index by scanning `records/` — the scan that
// used to be `list`, in the one place it belongs.
//
// Reached through the platform's inherited `rebuild-index` verb, which is why
// it returns a count rather than a response message: the platform owns the
// envelope, this owns the knowledge.
func rebuildIndex() (uint32, error) {
	return records.Rebuild(func(put func(string, []byte) error) error {
		names, err := platform.ListDir(recordsDir)
		if err != nil {
			// No directory yet means no records, which rebuilds to an empty index
			// rather than failing. A fresh app has never written one.
			return nil
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			record, err := readRecord(strings.TrimSuffix(name, ".json"))
			if err != nil {
				return err
			}
			value, err := project(record).MarshalVT()
			if err != nil {
				return err
			}
			if err := put(record.GetId(), value); err != nil {
				return err
			}
		}
		return nil
	})
}

// project turns a record into what a list view renders (D6) — and nothing more.
func project(r *notesv1.Record) *notesv1.RecordEntry {
	return &notesv1.RecordEntry{
		Id:          r.GetId(),
		Title:       r.GetTitle(),
		BodyPreview: preview(r.GetBody()),
		CreatedAt:   r.GetCreatedAt(),
	}
}

// preview is the first line of a body, capped.
//
// Capped in the ENGINE because it is a storage decision — an index holding whole
// bodies would be the whole store. Where to cut that line for a 24-column table
// is a different decision, and it belongs to the slot.
func preview(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	const max = 200
	if len(line) > max {
		return line[:max]
	}
	return line
}

// indexRecord writes one record's projection.
func indexRecord(r *notesv1.Record) error {
	value, err := project(r).MarshalVT()
	if err != nil {
		return err
	}
	return records.Put(r.GetId(), value)
}

func handleOpenRecord(req *notesv1.OpenRecordRequest) (*notesv1.OpenRecordResponse, error) {
	if req.Id == "" {
		return nil, errors.New("open-record: id is required")
	}
	record, err := readRecord(req.Id)
	if err != nil {
		return nil, err
	}
	return &notesv1.OpenRecordResponse{Record: record}, nil
}

func handleDeleteRecord(req *notesv1.DeleteRecordRequest) (*notesv1.DeleteRecordResponse, error) {
	if req.Id == "" {
		return nil, errors.New("delete-record: id is required")
	}
	ok, err := platform.RemoveFile(filepath.Join(recordsDir, req.Id+".json"))
	if err != nil {
		return nil, errors.New("delete-record: " + err.Error())
	}
	// Only when something was actually removed. Deleting a record that was never
	// there changed nothing, and an event saying otherwise would make every
	// subscriber re-read for no reason — and would make a no-op look like a write
	// to anyone counting events.
	//
	// The index still gets the Delete either way: it costs nothing (an absent key
	// is a no-op there too) and it is the branch where a leftover row would hide
	// if the file and the index ever disagreed about whether the record existed.
	if err := records.Delete(req.Id); err != nil {
		return nil, errors.New("delete-record: " + err.Error())
	}
	if ok {
		emitRecordChanged(req.Id, notesv1.MethodDeleteRecord)
	}
	return &notesv1.DeleteRecordResponse{Deleted: ok}, nil
}

// emitRecordChanged announces that a record appeared or vanished (§6.3).
//
// Called AFTER the write succeeds, never before: a subscriber that re-lists on
// this event must find the new state already there. This mirrors the platform's
// own emitDataChanged, and the mirroring is the point — an app's events are made
// of the same parts as the platform's, with no extra machinery.
//
// A marshal failure is swallowed rather than returned: the command has already
// succeeded, and failing it now because a notification could not be encoded
// would report data as unwritten that is on disk.
func emitRecordChanged(id string, method uint32) {
	// The topic lives on the message (generated from the .proto), so the emit
	// side and every subscriber read the same declaration.
	platform.EmitEvent(&notesv1.RecordChangedEvent{Id: id, Method: method})
}

// slug turns a title into a filesystem-safe id.
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeRecord(r *notesv1.Record) error {
	// Canonical JSON, not binary: the file is meant to be read, diffed, and
	// git-merged (§7.2). go-lite emits it without reflection.
	body, err := r.MarshalJSON()
	if err != nil {
		return err
	}
	return platform.WriteTree(platform.Root(), []platform.File{{
		Path:    filepath.Join(recordsDir, r.Id+".json"),
		Content: body,
	}})
}

func readRecord(id string) (*notesv1.Record, error) {
	body, err := platform.ReadFile(filepath.Join(recordsDir, id+".json"))
	if err != nil {
		return nil, errors.New("record " + strconv.Quote(id) + " not found")
	}
	var r notesv1.Record
	if err := r.UnmarshalJSON(body); err != nil {
		return nil, errors.New("record " + strconv.Quote(id) + " is corrupt: " + err.Error())
	}
	return &r, nil
}
