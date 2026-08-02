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
	"encoding/binary"
	"errors"
	"sort"

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
	// backend happens to return sorted pairs because sorting is how it gets
	// deterministic bytes; nothing may depend on that.
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

// fileStore keeps a whole index in ONE file and rewrites it on every mutation.
//
// That is write amplification by construction (D10) and it is accepted, with its
// limit named: fine for bounded data on web (where the tier hydrates the whole
// store into memory anyway) and native, and exactly what §5.6 tells you not to
// do to flash. Embedded is therefore the first tier that will need a real KV
// backend — which is the point of there being a seam at all.
type fileStore struct {
	path string
}

// NewFileStore returns the file-backed store for a named index, e.g. "records".
//
// The name is a filename, not a path: it is joined under the index directory so
// an app cannot place its index outside the one location the export exclusion
// knows about.
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
	return &fileStore{path: Dir + "/" + clean}, nil
}

func (s *fileStore) Put(key string, value []byte) error {
	if key == "" {
		return errors.New("index: empty key")
	}
	pairs, err := s.Scan()
	if err != nil {
		return err
	}
	for i := range pairs {
		if pairs[i].Key == key {
			pairs[i].Value = value
			return s.write(pairs)
		}
	}
	return s.write(append(pairs, Pair{Key: key, Value: value}))
}

func (s *fileStore) Delete(key string) error {
	pairs, err := s.Scan()
	if err != nil {
		return err
	}
	for i := range pairs {
		if pairs[i].Key == key {
			// Deleting something absent is NOT an error: an index is derived, so a
			// caller deleting a record that was never indexed is describing a state
			// the store already has.
			return s.write(append(pairs[:i], pairs[i+1:]...))
		}
	}
	return nil
}

func (s *fileStore) Clear() error {
	_, err := platform.RemoveFile(s.path)
	return err
}

// Scan reads the whole file back.
//
// A store that is not there yet is EMPTY, not an error — the index is derived,
// so "no file" is the honest state of a collection nothing has written to. Note
// what this does not do: classify the error with os.IsNotExist, which does not
// match TinyGo's WASI errno and would make native and wasm behave differently
// (the same trap DirIsOccupied documents).
func (s *fileStore) Scan() ([]Pair, error) {
	raw, err := platform.ReadFile(s.path)
	if err != nil {
		return nil, nil
	}
	return decode(raw)
}

func (s *fileStore) write(pairs []Pair) error {
	return platform.WriteFile(s.path, encode(pairs))
}

// ---- the on-disk format ---------------------------------------------------
//
// Hand-rolled and binary rather than BFT's JSON, for two reasons that pull the
// same way: values are arbitrary proto bytes (not text), and this file is the
// one thing in the tree that is DISPOSABLE — nobody reads it by hand, because
// the answer to "what does it say" is always "rebuild it and see".
//
// It must be byte-deterministic. The parity check diffs the filesystems the
// native and wasm engines write, so two runs that differ only in map iteration
// order would read exactly like a real divergence. Hence the sort.

// magic tags the format so a file from a future version is recognisably not
// this one. A mismatch is not an error: it reads as an empty index, which
// rebuild-index then repopulates. That is the whole benefit of a derived file —
// its migration story is "throw it away".
var magic = []byte{'D', 'L', 'C', 'I', 1}

func encode(pairs []Pair) []byte {
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })

	out := make([]byte, 0, len(magic)+len(pairs)*32)
	out = append(out, magic...)
	var scratch [binary.MaxVarintLen64]byte
	appendUvarint := func(dst []byte, v int) []byte {
		n := binary.PutUvarint(scratch[:], uint64(v))
		return append(dst, scratch[:n]...)
	}
	out = appendUvarint(out, len(pairs))
	for _, p := range pairs {
		out = appendUvarint(out, len(p.Key))
		out = append(out, p.Key...)
		out = appendUvarint(out, len(p.Value))
		out = append(out, p.Value...)
	}
	return out
}

// decode is deliberately forgiving in one direction only: anything it cannot
// make sense of becomes an EMPTY index rather than an error, because a corrupt
// derived file must not brick an app that can rebuild it. What it must never do
// is return a partial index that looks complete — so a truncated file yields
// nothing, not the entries it managed to read.
func decode(raw []byte) ([]Pair, error) {
	if len(raw) < len(magic) {
		return nil, nil
	}
	for i, b := range magic {
		if raw[i] != b {
			return nil, nil
		}
	}
	rest := raw[len(magic):]
	count, n := binary.Uvarint(rest)
	if n <= 0 {
		return nil, nil
	}
	rest = rest[n:]

	pairs := make([]Pair, 0, count)
	for i := uint64(0); i < count; i++ {
		key, next, ok := takeBytes(rest)
		if !ok {
			return nil, nil
		}
		value, next2, ok := takeBytes(next)
		if !ok {
			return nil, nil
		}
		rest = next2
		// The value is copied out of the buffer rather than aliased: callers keep
		// these around, and a later read into a reused buffer would rewrite them
		// underneath.
		v := make([]byte, len(value))
		copy(v, value)
		pairs = append(pairs, Pair{Key: string(key), Value: v})
	}
	return pairs, nil
}

func takeBytes(buf []byte) (val, rest []byte, ok bool) {
	n, read := binary.Uvarint(buf)
	if read <= 0 {
		return nil, nil, false
	}
	buf = buf[read:]
	if uint64(len(buf)) < n {
		return nil, nil, false
	}
	return buf[:n], buf[n:], true
}
