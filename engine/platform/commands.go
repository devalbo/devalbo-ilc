package platform

// The verbs every app inherits. An app calls RegisterAll() once and gets
// version / export-fs / import-fs / reset-fs — the same implementations, so a
// fix here reaches every app rather than only the ones scaffolded after it.

import (
	"errors"
	"os"
	"path/filepath"

	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
)

// version is app-supplied: the platform owns the *command*, the app owns the
// string. SetVersion is how an app configures it.
var version = "unset"

// SetVersion sets what the `version` command reports, e.g. "myapp 1.2.3".
func SetVersion(v string) { version = v }

// RegisterAll registers the platform's commands. Call it from the app's init
// before registering the app's own — the ids cannot collide (1–99 vs 100+), so
// order is a readability choice, not a correctness one.
func RegisterAll() {
	RegisterRaw(ilcv1.PlatformServiceHandlers(
		handleVersion,
		handleExportFs,
		handleImportFs,
		handleResetFs,
	))
}

func handleVersion(*ilcv1.VersionRequest) (*ilcv1.VersionResponse, error) {
	return &ilcv1.VersionResponse{Version: version}, nil
}

// handleExportFs bundles a subtree into a single BFT blob (§7.3). This is the
// same operation as the browser's "download my project" — export and download
// are one primitive, not two features.
func handleExportFs(req *ilcv1.ExportFsRequest) (*ilcv1.ExportFsResponse, error) {
	if req.Format != ilcv1.BundleFormat_BUNDLE_FORMAT_UNSPECIFIED &&
		req.Format != ilcv1.BundleFormat_BUNDLE_FORMAT_BFT {
		// zip / proto are declared in the schema and additive (§7.3); refusing
		// loudly beats silently handing back BFT under another name.
		return nil, errors.New("export-fs: only BFT is implemented; got " + req.Format.String())
	}
	dir, err := ResolveUnder(req.Prefix)
	if err != nil {
		return nil, errors.New("export-fs: " + err.Error())
	}
	tree, err := ReadTree(dir)
	if err != nil {
		return nil, errors.New("export-fs: " + err.Error())
	}
	return &ilcv1.ExportFsResponse{Bundle: encodeBFT(tree)}, nil
}

// handleImportFs writes a BFT bundle into the filesystem. Scaffolding is the
// same operation (§7.3) — `dlc new` is an import of a template bundle — which is
// why this and an app's scaffolder share WriteTree/SafeJoin rather than each
// having their own.
func handleImportFs(req *ilcv1.ImportFsRequest) (*ilcv1.ImportFsResponse, error) {
	if len(req.Bundle) == 0 {
		return nil, errors.New("import-fs: empty bundle")
	}
	tree, err := decodeBFT(req.Bundle)
	if err != nil {
		return nil, errors.New("import-fs: " + err.Error())
	}
	dir, err := ResolveUnder(req.Prefix)
	if err != nil {
		return nil, errors.New("import-fs: " + err.Error())
	}
	// MERGE writes over whatever is there and cannot express a deletion: drop a
	// file from the bundle, re-import, and the old file survives. REPLACE clears
	// first, so the destination ends up as exactly what the bundle says.
	if req.Mode == ilcv1.ImportMode_IMPORT_MODE_REPLACE {
		if _, err := removeTree(dir); err != nil {
			return nil, errors.New("import-fs: " + err.Error())
		}
	}
	files, err := writeBFTTree(dir, tree)
	if err != nil {
		return nil, errors.New("import-fs: " + err.Error())
	}
	// ONE event per command, not one per file: a 1000-file bundle must not become
	// 1000 messages. The subscriber re-reads what it cares about (§7.1) — the
	// event says something moved, not what.
	emitDataChanged(req.Prefix, MethodImportFs)
	return &ilcv1.ImportFsResponse{Files: files}, nil
}

// handleResetFs deletes a subtree (§7.3) — the counterpart that makes an app
// re-scaffoldable without wiping the whole root.
func handleResetFs(req *ilcv1.ResetFsRequest) (*ilcv1.ResetFsResponse, error) {
	dir, err := ResolveUnder(req.Prefix)
	if err != nil {
		return nil, errors.New("reset-fs: " + err.Error())
	}
	removed, err := removeTree(dir)
	if err != nil {
		return nil, errors.New("reset-fs: " + err.Error())
	}
	emitDataChanged(req.Prefix, MethodResetFs)
	return &ilcv1.ResetFsResponse{Removed: removed}, nil
}

// removeTree empties dir without removing dir itself — the root is a host-bound
// preopen, and deleting it would leave the engine with nowhere to write.
// Returns the top-level entries it removed.
func removeTree(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Nothing there is not a failure: reset is idempotent, and classifying
		// the error is exactly the portability trap dirIsOccupied documents.
		return nil, nil
	}
	removed := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := removeRecursive(filepath.Join(dir, entry.Name())); err != nil {
			return nil, err
		}
		removed = append(removed, entry.Name())
	}
	sortStrings(removed)
	return removed, nil
}

// removeRecursive deletes a file or directory tree.
//
// Hand-rolled instead of os.RemoveAll: RemoveAll fails under TinyGo/wasip2 with
// errno 52 (notsup) even on a plain file, because it reaches for syscalls the
// wasip2 target does not implement. Walking the tree with ReadDir + os.Remove
// uses only the calls WASI actually provides, so one implementation serves every
// tier — which the parity check enforces.
func removeRecursive(path string) error {
	entries, err := os.ReadDir(path)
	if err == nil {
		for _, entry := range entries {
			if err := removeRecursive(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	return os.Remove(path)
}

// ResolveUnder turns a caller-supplied prefix into a path under the tier's
// filesystem root, refusing anything that escapes it. An empty prefix means the
// whole root. Exported because app commands need the same containment.
func ResolveUnder(prefix string) (string, error) {
	if prefix == "" {
		return Root(), nil
	}
	clean, err := SafeJoin("", prefix)
	if err != nil {
		return "", err
	}
	return filepath.Join(Root(), clean), nil
}

// emitDataChanged announces that the filesystem moved under a prefix.
//
// Emitted AFTER the write succeeds, never before: a subscriber that re-reads on
// this event must find the new state already there. Emitting first would race
// every listener against the write it is describing.
//
// Marshal failure is swallowed rather than returned — the command has already
// succeeded, and failing it now because a notification could not be encoded
// would turn a cosmetic problem into data the caller thinks was not written.
func emitDataChanged(prefix string, method uint32) {
	// The topic comes off the message (generated from the .proto), so it cannot
	// disagree with the one a subscriber matches on.
	EmitEvent(&ilcv1.DataChangedEvent{Prefix: prefix, Method: method})
}
