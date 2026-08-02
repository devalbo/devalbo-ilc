package index

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platform "github.com/devalbo/dlc-platform"
)

// inTempRoot grants a filesystem root the way a host does, in a throwaway
// directory. Deliberately a copy of the platform package's helper rather than an
// export of it: a test helper exported for one caller becomes API.
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
	if err := platform.SetRoot("."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return root
}

func open(t *testing.T) *Index {
	t.Helper()
	ix, err := Open("records")
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func keys(pairs []Pair) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.Key)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- the store, round trip --------------------------------------------------

func TestPutScanDelete(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	if err := ix.Put("b", []byte("beta")); err != nil {
		t.Fatal(err)
	}
	if err := ix.Put("a", []byte("alpha")); err != nil {
		t.Fatal(err)
	}

	got, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a", "b"}) {
		t.Fatalf("entries = %v, want [a b]", keys(got))
	}
	if !bytes.Equal(got[0].Value, []byte("alpha")) {
		t.Fatalf("value = %q", got[0].Value)
	}

	if err := ix.Delete("a"); err != nil {
		t.Fatal(err)
	}
	got, err = ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"b"}) {
		t.Fatalf("after delete, entries = %v", keys(got))
	}
}

// A key written twice is one entry, not two. The index projects records, and a
// record updated twice is still one record.
func TestPutOverwrites(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	if err := ix.Put("a", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := ix.Put("a", []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %v, want one", keys(got))
	}
	if !bytes.Equal(got[0].Value, []byte("second")) {
		t.Fatalf("value = %q, want the second write", got[0].Value)
	}
}

// Deleting something that was never indexed is not an error: the index is
// derived, so the caller is describing a state the store already has.
func TestDeleteMissingIsNotAnError(t *testing.T) {
	inTempRoot(t)
	if err := open(t).Delete("never-existed"); err != nil {
		t.Fatalf("delete of an absent key: %v", err)
	}
}

// An index nothing has written to is EMPTY, not an error. "No file yet" is the
// honest state of a fresh collection, and it is also the state right after a
// rebuild clears the store.
func TestMissingStoreScansEmpty(t *testing.T) {
	inTempRoot(t)
	got, err := open(t).Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a store that was never written returned %v", keys(got))
	}
}

// The write must actually reach the filesystem, not just a process's memory —
// otherwise every claim about surviving a restart is untested. A second Index
// over the same path is the cheapest honest stand-in for reopening.
func TestWritesSurviveReopening(t *testing.T) {
	inTempRoot(t)
	if err := open(t).Put("a", []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	got, err := open(t).Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a"}) {
		t.Fatalf("a reopened index lost the write: %v", keys(got))
	}
}

// --- keys ARE filenames, and the rules are the same everywhere --------------

// The readable half of the bargain: a key lands as itself, so the store can be
// eyeballed. This is what the validation below is paying for.
func TestKeysAreTheFilenames(t *testing.T) {
	root := inTempRoot(t)
	if err := open(t).Put("buy-milk", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, Dir, "records", "buy-milk")); err != nil {
		t.Fatalf("the key is not the filename: %v", err)
	}
}

// THE CASE TRAP. Windows and macOS are both case-insensitive by default, so "a"
// and "A" would be one file and two records' projections would silently merge.
//
// Refused UNIFORMLY, including on a case-sensitive filesystem where it would
// technically work — an app that runs on the machine it was written on and
// fails on a user's is the failure this rule exists to prevent, and a check that
// only fires on some machines cannot prevent it.
func TestKeysDifferingOnlyByCaseAreRefused(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	if err := ix.Put("a", []byte("lower")); err != nil {
		t.Fatal(err)
	}
	err := ix.Put("A", []byte("upper"))
	if err == nil {
		t.Fatal("a key differing only by case was accepted")
	}
	// The message must name BOTH keys: an app author sees this through their own
	// command, and "keys collide" without the pair is unactionable.
	if !strings.Contains(err.Error(), `"A"`) || !strings.Contains(err.Error(), `"a"`) {
		t.Fatalf("error does not name both keys: %v", err)
	}
	// Re-writing the SAME key is not a collision with itself.
	if err := ix.Put("a", []byte("again")); err != nil {
		t.Fatalf("overwriting a key was treated as a collision: %v", err)
	}
}

// Every way a key stops being a legal filename, refused with a reason. Each of
// these is a real filesystem behaviour, not a style rule — see the comment block
// in store.go for which system breaks on which.
func TestUnusableKeysAreRefused(t *testing.T) {
	inTempRoot(t)
	ix := open(t)
	for _, tc := range []struct{ name, key string }{
		{"empty", ""},
		{"separator", "a/b"},
		{"backslash", `a\b`},
		{"colon", "a:b"},
		{"glob", "a*b"},
		{"question", "a?b"},
		{"quote", `a"b`},
		{"angle", "a<b"},
		{"pipe", "a|b"},
		{"control byte", "a\x01b"},
		{"leading dot", ".hidden"},
		{"trailing dot", "trailing."},
		{"trailing space", "trailing "},
		{"reserved device", "CON"},
		{"reserved with extension", "nul.json"},
		{"too long", strings.Repeat("x", 300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ix.Put(tc.key, []byte("v")); err == nil {
				t.Fatalf("Put(%q) was accepted", tc.key)
			}
		})
	}
}

// …and ordinary keys are not caught by any of it. A validation rule that also
// refuses `buy-milk` would be worse than no rule.
func TestOrdinaryKeysAreAccepted(t *testing.T) {
	inTempRoot(t)
	ix := open(t)
	for _, key := range []string{
		"buy-milk", "call-mum-2", "a", "note_1", "with space", "with.dot",
		"unicode-é-日本", "CONtext", "communication",
	} {
		t.Run(key, func(t *testing.T) {
			if err := ix.Put(key, []byte("v")); err != nil {
				t.Fatalf("Put(%q): %v", key, err)
			}
		})
	}
}

// A stray file is skipped, not read as an entry. With keys as filenames there is
// no prefix to filter on, so the same validation does the work — which is why
// leading dots are reserved.
func TestStrayFilesAreIgnored(t *testing.T) {
	root := inTempRoot(t)
	ix := open(t)
	if err := ix.Put("a", []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dir, "records", ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a"}) {
		t.Fatalf("entries = %v", keys(got))
	}
}

// --- one file per key, which is what removes the cross-process race ---------

// Two writers, no lock, no lost update. This is the bug the whole-file store
// had: both would read, add, and rewrite the entire index, and one entry would
// vanish. Separate stores over the same directory stand in for separate
// processes — the mechanism being tested is that the WRITES DO NOT OVERLAP, and
// that is a property of the layout, not of who is running.
func TestConcurrentKeysDoNotOverwriteEachOther(t *testing.T) {
	inTempRoot(t)
	first, second := open(t), open(t)

	// Interleaved on purpose: each store reads nothing and writes only its own
	// file, so ordering cannot matter. Under the old layout this sequence lost
	// whichever entry was written first.
	if err := first.Put("a", []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	if err := second.Put("b", []byte("beta")); err != nil {
		t.Fatal(err)
	}
	if err := first.Put("c", []byte("gamma")); err != nil {
		t.Fatal(err)
	}

	got, err := open(t).Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a", "b", "c"}) {
		t.Fatalf("entries = %v, want all three", keys(got))
	}
}

// --- D4: the maintained index equals a rebuilt one --------------------------

// The invariant the whole design leans on. This is the assertion an app repeats
// after every mutation test (Phase 3), so it is worth having the helper here.
func assertRebuildMatches(t *testing.T, ix *Index, build func(put func(string, []byte) error) error) {
	t.Helper()
	before, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Rebuild(build); err != nil {
		t.Fatal(err)
	}
	after, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("maintained index has %v, rebuilt has %v", keys(before), keys(after))
	}
	for i := range before {
		if before[i].Key != after[i].Key || !bytes.Equal(before[i].Value, after[i].Value) {
			t.Fatalf("entry %d drifted: maintained %q=%q, rebuilt %q=%q",
				i, before[i].Key, before[i].Value, after[i].Key, after[i].Value)
		}
	}
}

// Records are the source of truth; the index is a projection of them. Maintain
// it through Put/Delete, then rebuild from the records — the two must agree.
func TestRebuildEqualsMaintained(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	records := map[string]string{"a": "alpha", "b": "beta"}
	for id, title := range records {
		if err := ix.Put(id, []byte(title)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Delete("b"); err != nil {
		t.Fatal(err)
	}
	delete(records, "b")

	assertRebuildMatches(t, ix, func(put func(string, []byte) error) error {
		for id, title := range records {
			if err := put(id, []byte(title)); err != nil {
				return err
			}
		}
		return nil
	})
}

// The failure D4 exists to catch, made to happen on purpose: a mutation that
// forgot to touch the index. Without the rebuild comparison this is invisible —
// every query keeps answering, just wrongly, forever.
func TestRebuildCatchesAnUnindexedRecord(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	// The app wrote two records but only indexed one.
	if err := ix.Put("a", []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	records := map[string]string{"a": "alpha", "b": "beta"}

	before, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Rebuild(func(put func(string, []byte) error) error {
		for id, title := range records {
			if err := put(id, []byte(title)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == len(after) {
		t.Fatal("rebuild did not notice the record that was never indexed")
	}
}

// Rebuild CLEARS first. A rebuild that merged could never remove a stale row —
// which is one of the three failures it exists to fix.
func TestRebuildDropsStaleRows(t *testing.T) {
	inTempRoot(t)
	ix := open(t)

	if err := ix.Put("deleted-long-ago", []byte("ghost")); err != nil {
		t.Fatal(err)
	}
	n, err := ix.Rebuild(func(put func(string, []byte) error) error {
		return put("a", []byte("alpha"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rebuilt %d entries, want 1", n)
	}
	got, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a"}) {
		t.Fatalf("stale row survived the rebuild: %v", keys(got))
	}
}

// An empty collection rebuilds to zero and says so. That is the distinction the
// count exists for: "the rebuilder is broken" and "there is nothing to index"
// look identical without it.
func TestRebuildOfNothingIsZeroAndNotAnError(t *testing.T) {
	inTempRoot(t)
	n, err := open(t).Rebuild(func(func(string, []byte) error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rebuilt %d entries from an empty collection", n)
	}
}

// A rebuild that fails part-way reports the error and NO count. A plausible
// number attached to a half-built index is worse than an obvious zero, because
// the number is the part a caller would trust.
func TestFailedRebuildReportsNoCount(t *testing.T) {
	inTempRoot(t)
	n, err := open(t).Rebuild(func(put func(string, []byte) error) error {
		if err := put("a", []byte("alpha")); err != nil {
			return err
		}
		return errors.New("records/b.json is unreadable")
	})
	if err == nil {
		t.Fatal("a failing rebuilder produced no error")
	}
	if n != 0 {
		t.Fatalf("a failed rebuild reported %d entries", n)
	}
}

// --- the seam ---------------------------------------------------------------

// memStore is a Store with no filesystem anywhere in it. Its existence is the
// check on D2: if the seam had grown a file-shaped assumption, this would not
// compile — which is the question "read it as if implementing on littlefs" is
// really asking.
type memStore struct{ pairs []Pair }

func (m *memStore) Put(key string, value []byte) error {
	for i := range m.pairs {
		if m.pairs[i].Key == key {
			m.pairs[i].Value = value
			return nil
		}
	}
	m.pairs = append(m.pairs, Pair{Key: key, Value: value})
	return nil
}

func (m *memStore) Delete(key string) error {
	for i := range m.pairs {
		if m.pairs[i].Key == key {
			m.pairs = append(m.pairs[:i], m.pairs[i+1:]...)
			return nil
		}
	}
	return nil
}

// Scan returns REVERSE insertion order on purpose: the seam's contract says
// unordered, so a backend that honours it awkwardly must still produce ordered
// queries. If Entries ever stopped sorting, this is what would notice.
func (m *memStore) Scan() ([]Pair, error) {
	out := make([]Pair, 0, len(m.pairs))
	for i := len(m.pairs) - 1; i >= 0; i-- {
		out = append(out, m.pairs[i])
	}
	return out, nil
}

func (m *memStore) Clear() error { m.pairs = nil; return nil }

func TestAnyStoreBacksTheIndex(t *testing.T) {
	ix := New(&memStore{})
	for _, k := range []string{"c", "a", "b"} {
		if err := ix.Put(k, []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ix.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !equal(keys(got), []string{"a", "b", "c"}) {
		t.Fatalf("entries = %v, want sorted despite an unordered store", keys(got))
	}
}

// The index lives under one directory, so the exclusion that keeps it out of
// bundles has exactly one path to know about. A name that escaped it would put
// a derived file somewhere that ships.
func TestIndexNameCannotEscapeTheIndexDirectory(t *testing.T) {
	inTempRoot(t)
	for _, name := range []string{"", "../outside", "/absolute"} {
		if _, err := Open(name); err == nil {
			t.Errorf("Open(%q) was allowed", name)
		}
	}
}
