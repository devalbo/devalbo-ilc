// Package engine holds notes' business logic — all of it.
//
// SPLIT STORAGE (§7.1) in practice: each record is ONE canonical-JSON file under
// `records/`, and that file is the source of truth. There is no database here.
// An index would only ever be a query accelerator, and a disposable one — which
// is precisely what lets this same code run on a tier that has no index at all.
package engine

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"

	"github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/dlcconfig"

	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
)

// No version string here: it lives in dlc.toml and arrives as generated code.

// recordsDir holds one JSON file per record, relative to the host-bound root.
const recordsDir = "records"

func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())

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
	if err := writeRecord(record); err != nil {
		return nil, err
	}
	return &notesv1.CreateRecordResponse{
		Record: record,
		Path:   filepath.Join(recordsDir, id+".json"),
	}, nil
}

// handleListRecords scans the directory.
//
// A FULL SCAN, deliberately, and this is the finding App #2 exists to produce:
// §7.1 wants a SQLite index here, and the platform has none — so the fallback
// path is the only path. It is also proof the fallback is real, since every
// tier runs it today. When the index lands, this becomes the `unavailable`
// branch rather than being rewritten.
func handleListRecords(*notesv1.ListRecordsRequest) (*notesv1.ListRecordsResponse, error) {
	paths, err := platform.ListDir(recordsDir)
	if err != nil {
		// No directory yet is an empty list, not a failure.
		return &notesv1.ListRecordsResponse{}, nil
	}
	resp := &notesv1.ListRecordsResponse{}
	for _, name := range paths {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		record, err := readRecord(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		resp.Records = append(resp.Records, record)
	}
	return resp, nil
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
	return &notesv1.DeleteRecordResponse{Deleted: ok}, nil
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
