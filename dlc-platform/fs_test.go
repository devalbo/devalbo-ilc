// The per-file API apps use. Every one of these is a path-containment decision,
// so the containment cases matter more than the happy paths — an app that can
// read `../../etc/passwd` through the platform has no sandbox at all.
package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWriteFile(t *testing.T) {
	inTempRoot(t)

	if err := WriteFile("notes/a.json", []byte(`{"id":"a"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile("notes/a.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":"a"}` {
		t.Errorf("got %q", got)
	}
	// Parent directories are created — an app should not have to mkdir first.
	if _, err := os.Stat(filepath.Join(Root(), "notes")); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestListDirSorted(t *testing.T) {
	inTempRoot(t)
	for _, name := range []string{"z.json", "a.json", "m.json"} {
		if err := WriteFile("r/"+name, []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListDir("r")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.json", "m.json", "z.json"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Sorted, because unsorted directory order differs between filesystems and
	// would diverge native vs wasm on nothing but that.
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

// Deleting is idempotent: "not there" is a false, not an error. This is the case
// that must NOT be implemented with os.IsNotExist, which does not match TinyGo's
// WASI errno.
func TestRemoveFileIdempotent(t *testing.T) {
	inTempRoot(t)
	if err := WriteFile("r/a.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveFile("r/a.json")
	if err != nil || !removed {
		t.Fatalf("first remove: removed=%v err=%v", removed, err)
	}
	removed, err = RemoveFile("r/a.json")
	if err != nil {
		t.Fatalf("second remove errored: %v", err)
	}
	if removed {
		t.Error("second remove reported it deleted something")
	}
	removed, err = RemoveFile("never/existed.json")
	if err != nil || removed {
		t.Errorf("missing path: removed=%v err=%v", removed, err)
	}
}

// The whole point of routing app file access through the platform.
func TestPerFileAPIRefusesEscape(t *testing.T) {
	inTempRoot(t)
	for _, bad := range []string{"../escape.json", "a/../../escape.json", "/etc/passwd", ""} {
		if _, err := ReadFile(bad); err == nil {
			t.Errorf("ReadFile(%q): expected refusal", bad)
		}
		if err := WriteFile(bad, []byte("x")); err == nil {
			t.Errorf("WriteFile(%q): expected refusal", bad)
		}
		if _, err := ListDir(bad); err == nil {
			t.Errorf("ListDir(%q): expected refusal", bad)
		}
		if _, err := RemoveFile(bad); err == nil {
			t.Errorf("RemoveFile(%q): expected refusal", bad)
		}
	}
	// Nothing escaped.
	if _, err := os.Stat(filepath.Join(filepath.Dir(Root()), "escape.json")); err == nil {
		t.Error("a file escaped the root")
	}
}

// FSError words filesystem failures itself, because a raw OS error is not
// portable text — the same condition reads "no such file or directory"
// natively and "file does not exist" under wasip2, and the joined path differs
// too (`app-x` vs `/app-x`). The parity check diffs error strings, so any
// command forwarding a runtime's phrasing was un-parity-able.
func TestFSErrorUsesTheCallersPathAndOurWording(t *testing.T) {
	inTempRoot(t)

	err := FSError("export-fs", "no-such-tree", errors.New("open /wherever: file does not exist"))
	if err == nil {
		t.Fatal("expected an error")
	}
	got := err.Error()

	if got != "export-fs: no-such-tree: does not exist" {
		t.Errorf("got %q", got)
	}
	// The two things that made it tier-dependent must be gone: the runtime's
	// phrasing, and the absolute path it was joined against.
	for _, leak := range []string{"no such file", "file does not exist", "/wherever"} {
		if strings.Contains(got, leak) {
			t.Errorf("the raw OS error leaked %q into %q", leak, got)
		}
	}
}

// An existing path that cannot be read is reported differently from a missing
// one — coarse, but the distinction a caller can act on.
func TestFSErrorDistinguishesPresentFromMissing(t *testing.T) {
	inTempRoot(t)
	if err := WriteFile("here.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}

	if got := FSError("op", "here.txt", errors.New("boom")).Error(); got != "op: here.txt: cannot be read" {
		t.Errorf("present path: got %q", got)
	}
	if got := FSError("op", "gone.txt", errors.New("boom")).Error(); got != "op: gone.txt: does not exist" {
		t.Errorf("missing path: got %q", got)
	}
}

// An empty prefix means the ROOT, and must not produce a bare "::".
func TestFSErrorNamesTheRoot(t *testing.T) {
	inTempRoot(t)
	if got := FSError("export-fs", "", errors.New("boom")).Error(); got != "export-fs: .: cannot be read" {
		t.Errorf("root: got %q", got)
	}
}
