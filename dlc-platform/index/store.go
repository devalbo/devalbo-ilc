// Package index is the derived projection index (§6.2, §7.1,
// docs/INDEX-PLAN.md): how an app answers "list the things" without opening
// every file, with the same answer on every tier.
//
// The shape that makes that possible is that **the engine owns the projection
// and the query, and only STORAGE is a seam** (D1). Ordering, filtering and
// paging happen in ordinary Go in the app's handler, so a storage backend can
// change a duration but never a result. That is the difference from the SQLite
// design this replaced, where the backend *was* the query engine and every
// backend swap was a semantics risk.
//
// This package imports the platform for its filesystem root; the platform must
// never import this package. The `rebuild-index` verb reaches an app's rebuilder
// through platform.SetIndexRebuilder — a func value, not an import — which is
// what keeps that direction one-way.
//
// TinyGo-safe and reflection-free, like everything the engine compiles.
package index

import (
	"errors"
	"strconv"
	"strings"

	platform "github.com/devalbo/dlc-platform"
)

// Pair is one stored projection: an app-defined key and opaque bytes.
//
// The value is opaque ON PURPOSE. It is an app's own proto message, which this
// package cannot know and must not decode — the moment the seam understands the
// payload it has opinions about it, and those opinions become a second query
// implementation (D1).
type Pair struct {
	Key   string
	Value []byte
}

// Store is the storage seam, deliberately shaped like `wasi:keyvalue` (D2).
//
// Four operations, every one of which the standard already has, because §6.6's
// rule is to mirror a standard even when implementing it ourselves — so binding
// a real `wasi:keyvalue` host store later (Phase 6) is wiring rather than
// redesign.
//
// The omissions each have a reason rather than being oversights:
//
//   - no Get(key) — a point lookup reads the RECORD, not the index (D6), and a
//     list query wants everything anyway
//   - no cursor on Scan — the standard has one for unbounded stores; §5.6 says
//     assume bounded data, and a cursor now would be a branch nothing tests
//   - no exists, no atomics, no batch — nothing needs them
//
// Read this as if implementing it on littlefs: nothing here may assume a file.
// Keys are opaque strings, values are opaque bytes, and Scan hands back
// everything because that is what a KV store can honestly do.
type Store interface {
	Put(key string, value []byte) error
	Delete(key string) error
	// Scan returns every pair. UNORDERED by contract — a KV store cannot order,
	// which is precisely why the sort lives in Go, above this seam (D1). The file
	// backend returns them in directory order, which is neither sorted by key nor
	// stable across filesystems; nothing may depend on it.
	Scan() ([]Pair, error)
	// Clear empties the store. Rebuild starts from nothing rather than trying to
	// diff, because a diff is how a stale row survives a rebuild.
	Clear() error
}

// Dir is where file-backed indexes live, relative to the filesystem root.
//
// Re-exported from the platform, which owns the constant because IT owns the
// export exclusion (D5) — the index is derived, so it must never travel in a
// bundle, and the one place that can enforce that is the tree reader.
const Dir = platform.IndexDir

// fileStore keeps ONE FILE PER KEY, in a directory named for the index.
//
// It was one whole-file blob until 2026-08-02, and the rewrite fixed a real bug
// rather than tidying: `Put` was a read-modify-write of the entire index, so two
// native processes creating a record at the same time each read, added, and
// rewrote — and one entry was silently lost. The records survived (a file each);
// the projection of one did not, so `list` under-reported until someone ran
// `rebuild-index`. That is §7.1 D7's "second writer that does not exist yet",
// and it exists for any CLI a script runs twice.
//
// A file per key removes it WITHOUT A LOCK: two processes writing different keys
// touch different files and cannot conflict, and two writing the same key are
// last-write-wins on one small file — which is exactly the semantics the records
// themselves have. That matters more now that Windows is a target, because the
// portable-locking story is genuinely bad: flock is unix, LockFileEx is Windows,
// and an O_EXCL lockfile wedges the app when a process dies holding it.
//
// It also improves D10 rather than trading against it: a Put rewrites one small
// file instead of the whole projection, so the flash-endurance problem that was
// going to force a KV backend on embedded gets smaller, not larger.
//
// The cost is n small files where there was one. On web that is free (the tier
// hydrates everything anyway), and on native it is what the records directory
// already looks like.
type fileStore struct {
	dir string
}

// NewFileStore returns the file-backed store for a named index, e.g. "records".
//
// The name is a directory name, not a path: it is joined under the index
// directory so an app cannot place its index outside the one location the export
// exclusion knows about.
func NewFileStore(name string) (Store, error) {
	if name == "" {
		return nil, errors.New("index: empty name")
	}
	// SafeJoin with an empty root does the containment check and hands back a
	// clean relative path — the same door every other app path goes through.
	clean, err := platform.SafeJoin("", name)
	if err != nil {
		return nil, errors.New("index: " + err.Error())
	}
	return &fileStore{dir: platform.JoinPath(Dir, clean)}, nil
}

func (s *fileStore) Put(key string, value []byte) error {
	if err := validateKey(key); err != nil {
		return err
	}
	// The case check reads the directory, which is why it is here and not in
	// validateKey: it is a fact about the STORE, not about the key. Skipped when
	// the key is already present exactly — overwriting yourself cannot collide
	// with yourself, and that is the common path.
	names, err := platform.ListDir(s.dir)
	if err == nil {
		for _, existing := range names {
			if foldsTo(existing, key) {
				return errors.New("index: key " + strconv.Quote(key) +
					" differs from " + strconv.Quote(existing) +
					" only by case, and most filesystems (Windows, macOS) cannot tell them apart")
			}
		}
	}
	return platform.WriteFile(platform.JoinPath(s.dir, key), value)
}

func (s *fileStore) Delete(key string) error {
	// Not validated: a key that could never have been written cannot be present,
	// and refusing to delete it would turn "this is already gone" into an error.
	// Deleting something absent is NOT an error for the same reason — an index is
	// derived, so the caller is describing a state the store already has.
	if err := validateKey(key); err != nil {
		return nil
	}
	_, err := platform.RemoveFile(platform.JoinPath(s.dir, key))
	return err
}

func (s *fileStore) Clear() error {
	names, err := platform.ListDir(s.dir)
	if err != nil {
		return nil // nothing there is already clear
	}
	for _, name := range names {
		if _, err := platform.RemoveFile(platform.JoinPath(s.dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// Scan reads every key file.
//
// A store that is not there yet is EMPTY, not an error — the index is derived,
// so "no directory" is the honest state of a collection nothing has written to.
// Note what this does not do: classify the error with os.IsNotExist, which does
// not match TinyGo's WASI errno and would make native and wasm behave
// differently (the same trap DirIsOccupied documents).
//
// A file that vanishes between the listing and the read is SKIPPED rather than
// failing the scan: another process deleting a record concurrently is a normal
// thing for a derived store to observe, not a fault.
func (s *fileStore) Scan() ([]Pair, error) {
	names, err := platform.ListDir(s.dir)
	if err != nil {
		return nil, nil
	}
	pairs := make([]Pair, 0, len(names))
	for _, name := range names {
		// A filename IS a key, so anything that could not have been written as one
		// is not ours — a `.DS_Store` or an editor's swap file. Skipped rather than
		// fatal: a stray file must not take an app's whole list down with it.
		if validateKey(name) != nil {
			continue
		}
		value, err := platform.ReadFile(platform.JoinPath(s.dir, name))
		if err != nil {
			continue
		}
		pairs = append(pairs, Pair{Key: name, Value: value})
	}
	return pairs, nil
}

// ---- keys ARE filenames, and the platform says so out loud ---------------
//
// A key is used verbatim: `.dlc-index/records/buy-milk`, not a hex blob. That is
// a deliberate choice in favour of a store a human can read, which is the same
// argument canonical JSON and slugged record names already make — the index was
// the one directory you could not eyeball, and the answer to "what does it say"
// should not have to be "write a decoder".
//
// The price is that a key must actually BE a legal filename everywhere, and the
// platform enforces that rather than hoping. Every rule below exists because a
// real filesystem breaks without it:
//
//	separators & illegal chars   `/` `\` `:` `*` `?` `"` `<` `>` `|` and control
//	                             bytes — a key with `/` would silently create a
//	                             subdirectory, and the rest are illegal on Windows
//	reserved device names        CON, PRN, AUX, NUL, COM1-9, LPT1-9, extension or
//	                             not: `CON.json` is still the console on Windows
//	trailing dot or space        Windows strips them silently, so "a." and "a"
//	                             become the same file with no error anywhere
//	leading dot                  reserved for the store's own housekeeping, so a
//	                             stray `.DS_Store` is never mistaken for an entry
//	length                       255 bytes is the shared filename ceiling
//
// UNIFORM ON EVERY PLATFORM, and that is the important part. It would be cheaper
// to check only where a rule bites — Windows for `:`, case-insensitive volumes
// for case — but then an app works on the machine it was written on and fails on
// a user's, which is exactly the class of failure this project writes checks to
// prevent. An app that runs anywhere must be told the rules everywhere.

// maxKeyLen is the shared filename ceiling. Windows also has a total-path limit
// (260 by default), which no key-length rule can enforce alone — it depends on
// where the app's root is — so this bounds what it can.
const maxKeyLen = 255

// reserved are Windows device names, which are unopenable as files whatever the
// extension. Compared case-insensitively against the part before the first dot.
var reserved = []string{
	"con", "prn", "aux", "nul",
	"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
	"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
}

// validateKey reports why a key cannot be a filename, or nil.
//
// The error names the rule AND the key, because this surfaces to an app author
// through their own command — "index: key …" with no key in it is a bug report
// nobody can act on.
func validateKey(key string) error {
	if key == "" {
		return errors.New("index: empty key")
	}
	if len(key) > maxKeyLen {
		return errors.New("index: key is longer than a filename can be: " + key[:32] + "…")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x20 || c == 0x7f {
			return errors.New("index: key contains a control character: " + strconv.Quote(key))
		}
		switch c {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return errors.New("index: key contains " + strconv.Quote(string(c)) +
				", which is not legal in a filename: " + strconv.Quote(key))
		}
	}
	if key[0] == '.' {
		return errors.New("index: key starts with a dot, which the store reserves: " + strconv.Quote(key))
	}
	if last := key[len(key)-1]; last == '.' || last == ' ' {
		return errors.New("index: key ends with a dot or space, which Windows silently strips: " +
			strconv.Quote(key))
	}
	base := key
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	for _, r := range reserved {
		if strings.EqualFold(base, r) {
			return errors.New("index: key is a reserved device name on Windows: " + strconv.Quote(key))
		}
	}
	return nil
}

// foldsTo reports whether two keys would be the same file on a case-insensitive
// filesystem — which is the DEFAULT on both Windows and macOS.
//
// This is the one rule that cannot be checked from a key alone: it is relational,
// so Put pays a directory listing to enforce it. That cost is deliberate and
// bounded — it is the same listing `Scan` already does for every query — and the
// alternative is two records' projections silently merging into one file on most
// of the machines this will ever run on.
func foldsTo(a, b string) bool { return a != b && strings.EqualFold(a, b) }
