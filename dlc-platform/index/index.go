package index

import (
	"errors"
	"sort"
)

// Index is what an app actually holds: a named projection over a Store.
//
// Everything here is thin on purpose. The interesting code in an index-backed
// app is the app's own query — a sort, a filter, a slice — written against
// Scan's result in ordinary Go. If this type grows a query API, that is the
// signal it has started becoming a query language (D1), which is the one thing
// this design exists to avoid.
type Index struct {
	store Store
}

// New wraps any store. The seam is exported so a test can drive a fake and, one
// day (Phase 6), a host-provided KV store can be bound with no app change.
func New(store Store) *Index { return &Index{store: store} }

// Open is the ordinary way an app gets an index: file-backed, under the index
// directory, named for the collection it projects.
func Open(name string) (*Index, error) {
	store, err := NewFileStore(name)
	if err != nil {
		return nil, err
	}
	return New(store), nil
}

// Put records the projection of one record.
//
// WRITE ORDER (D7): the app writes the FILE first, this second, and emits its
// event last. A subscriber that re-reads on the event must find both already
// consistent — and if the process dies in between, the truth is on disk and only
// the derived thing is behind, which is exactly what rebuild-index repairs.
func (ix *Index) Put(key string, value []byte) error {
	if ix == nil || ix.store == nil {
		return errors.New("index: not open")
	}
	return ix.store.Put(key, value)
}

// Delete drops one record's projection. Absent is not an error (see the store).
func (ix *Index) Delete(key string) error {
	if ix == nil || ix.store == nil {
		return errors.New("index: not open")
	}
	return ix.store.Delete(key)
}

// Entries returns every projection, ORDERED BY KEY.
//
// The sort is here rather than in the seam because a KV store cannot order and
// the standard's list-keys does not promise to (D2). Sorting once at the top of
// every query also gives an app a stable starting point: whatever ordering it
// actually wants — by title, by created-at — is a sort.Slice over this, and a
// stable input is what stops equal keys shuffling between runs and tripping the
// parity diff.
func (ix *Index) Entries() ([]Pair, error) {
	if ix == nil || ix.store == nil {
		return nil, errors.New("index: not open")
	}
	pairs, err := ix.store.Scan()
	if err != nil {
		return nil, err
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
	return pairs, nil
}

// Rebuild discards the index and reconstructs it by scanning the source of
// truth — the files — through the app-supplied build function.
//
// This is the operation the whole design leans on (D4). The invariant it makes
// checkable is that **the maintained index equals a rebuilt one**, which catches
// a create that forgot to index, a delete that left a row, and a projection that
// drifted from the record it projects — with no second tier, no second backend,
// and no golden file.
//
// Clear happens FIRST and unconditionally. A rebuild that merged into whatever
// was already there could not remove a stale row, which is one of the three
// failures it exists to fix.
func (ix *Index) Rebuild(build func(put func(key string, value []byte) error) error) (uint32, error) {
	if ix == nil || ix.store == nil {
		return 0, errors.New("index: not open")
	}
	if build == nil {
		return 0, errors.New("index: no rebuilder")
	}
	if err := ix.store.Clear(); err != nil {
		return 0, err
	}
	var n uint32
	err := build(func(key string, value []byte) error {
		if err := ix.store.Put(key, value); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		// The count is not returned on failure: a half-built index with a plausible
		// number attached is worse than an obvious zero, because the number is what
		// a caller would trust.
		return 0, err
	}
	return n, nil
}
