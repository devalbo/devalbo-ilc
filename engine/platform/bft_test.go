// BFT bundle round-trips (§7.3). The bundle is the interchange format — a blob
// that has to survive a text channel, a git diff, another machine, and a
// hand-edit — so these tests care about determinism and about rejecting bad
// input, not just about the happy path.
//
// Internal test (package engine) because the format's guarantees are internal:
// what crosses the boundary is bytes.
package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBFTRoundTrip(t *testing.T) {
	root := newDir()
	mustInsert(t, root, "go.mod", []byte("module example.com/x\n"))
	mustInsert(t, root, "engine/execute.go", []byte("package engine\n"))
	mustInsert(t, root, "assets/logo.bin", []byte{0x00, 0x01, 0xff, 0xfe})

	bundle := encodeBFT(root)
	back, err := decodeBFT(bundle)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, bundle)
	}

	got := map[string]string{}
	if err := back.walk("", func(path string, content []byte) error {
		got[path] = string(content)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"assets/logo.bin":   "\x00\x01\xff\xfe",
		"engine/execute.go": "package engine\n",
		"go.mod":            "module example.com/x\n",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d: %v", len(got), len(want), got)
	}
	for path, wantContent := range want {
		if got[path] != wantContent {
			t.Errorf("%s: got %q, want %q", path, got[path], wantContent)
		}
	}
}

// Text stays readable and binary goes to base64 — the property that makes a
// bundle diffable instead of an opaque blob.
func TestBFTTextVsBinary(t *testing.T) {
	root := newDir()
	mustInsert(t, root, "readme.txt", []byte("hello \"world\"\n\tindented\n"))
	mustInsert(t, root, "data.bin", []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05})
	bundle := string(encodeBFT(root))

	if !strings.Contains(bundle, `"type": "text"`) {
		t.Error("expected a text node")
	}
	if !strings.Contains(bundle, `hello \"world\"\n\tindented`) {
		t.Errorf("text content not escaped readably:\n%s", bundle)
	}
	if !strings.Contains(bundle, `"encoding": "base64"`) {
		t.Error("expected a binary node")
	}
	// The spec's own example encoding of these bytes.
	if !strings.Contains(bundle, "AAECAwQF") {
		t.Errorf("unexpected base64 for 00..05:\n%s", bundle)
	}
}

// Byte-stability is the whole reason entries are alphabetical: two exports of
// the same tree must produce identical bytes, whatever order the filesystem or
// a Go map hands things back in.
func TestBFTDeterministic(t *testing.T) {
	build := func() *bftNode {
		n := newDir()
		for _, p := range []string{"z.txt", "a.txt", "m/b.txt", "m/a.txt", "B.txt"} {
			mustInsert(t, n, p, []byte(p))
		}
		return n
	}
	first := encodeBFT(build())
	for i := 0; i < 20; i++ {
		if got := encodeBFT(build()); string(got) != string(first) {
			t.Fatalf("bundle not deterministic on run %d:\n%s\n---\n%s", i, first, got)
		}
	}
	// …and alphabetical, per the spec.
	idxA := strings.Index(string(first), `"a.txt"`)
	idxZ := strings.Index(string(first), `"z.txt"`)
	if idxA < 0 || idxZ < 0 || idxA > idxZ {
		t.Errorf("entries not alphabetical (a at %d, z at %d)", idxA, idxZ)
	}
}

// The example from the BFT README must parse — this is the compatibility claim.
func TestBFTParsesSpecExample(t *testing.T) {
	src := []byte(`{
  "type": "directory",
  "entries": {
    "README.txt": {
      "type": "text",
      "content": "Hello Brute Force Transfer!\n"
    },
    "data.bin": {
      "type": "binary",
      "encoding": "base64",
      "content": "AAECAwQF"
    }
  }
}`)
	tree, err := decodeBFT(src)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]byte{}
	tree.walk("", func(path string, content []byte) error {
		got[path] = content
		return nil
	})
	if string(got["README.txt"]) != "Hello Brute Force Transfer!\n" {
		t.Errorf("README.txt: %q", got["README.txt"])
	}
	if string(got["data.bin"]) != "\x00\x01\x02\x03\x04\x05" {
		t.Errorf("data.bin: %v", got["data.bin"])
	}
}

// Comments are optional metadata the spec allows and extraction ignores. An
// unknown object-valued field must not break an older reader either.
func TestBFTIgnoresComments(t *testing.T) {
	src := []byte(`{
  "type": "directory",
  "comment": "provenance goes here",
  "entries": {
    "a.txt": {"type": "text", "content": "a", "comment": "why this file exists"}
  }
}`)
	tree, err := decodeBFT(src)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	tree.walk("", func(string, []byte) error { count++; return nil })
	if count != 1 {
		t.Errorf("got %d files, want 1", count)
	}
}

func TestBFTRejectsGarbage(t *testing.T) {
	cases := map[string]string{
		"not json":            `hello`,
		"missing type":        `{"entries":{}}`,
		"unknown type":        `{"type":"symlink"}`,
		"bad base64":          `{"type":"binary","encoding":"base64","content":"!!!!"}`,
		"unsupported coding":  `{"type":"binary","encoding":"hex","content":"00"}`,
		"text without body":   `{"type":"text"}`,
		"unterminated string": `{"type":"text","content":"oops`,
		"trailing content":    `{"type":"text","content":"a"} junk`,
		"empty":               ``,
	}
	for name, src := range cases {
		if _, err := decodeBFT([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// A bundle is untrusted input. Importing one must never write outside the root,
// however the path is spelled.
func TestImportRejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	for _, evil := range []string{"../escape.txt", "a/../../escape.txt", "/abs.txt"} {
		tree := newDir()
		// insert() splits on "/", so build the hostile node directly — this is
		// what a hand-edited or malicious bundle looks like on the wire.
		tree.entries[evil] = &bftNode{content: []byte("pwned")}
		if _, err := writeBFTTree(root, tree); err == nil {
			t.Errorf("%q: expected refusal", evil)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); err == nil {
		t.Error("a file escaped the root")
	}
}

// export → import → export must reproduce the bundle byte-for-byte, through the
// real filesystem. This is the property the cross-tier story rests on: a bundle
// exported in the browser has to rebuild the same tree in the terminal.
func TestBFTFilesystemRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeFixture(t, src, "go.mod", "module example.com/x\n")
	writeFixture(t, src, "engine/execute.go", "package engine\n")
	writeFixture(t, src, "deep/nested/dir/file.txt", "deep\n")

	tree, err := ReadTree(src)
	if err != nil {
		t.Fatal(err)
	}
	bundle := encodeBFT(tree)

	dest := t.TempDir()
	decoded, err := decodeBFT(bundle)
	if err != nil {
		t.Fatal(err)
	}
	files, err := writeBFTTree(dest, decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("wrote %v, want 3 files", files)
	}

	again, err := ReadTree(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeBFT(again); string(got) != string(bundle) {
		t.Errorf("round-trip changed the bundle:\n%s\n---\n%s", bundle, got)
	}
}

func mustInsert(t *testing.T, n *bftNode, path string, content []byte) {
	t.Helper()
	if err := n.insert(path, content); err != nil {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Export must not die on entries BFT cannot represent. A symlink is the common
// case (a `node_modules` or a nix profile in the tree) and reading one can fail
// outright, so they are skipped rather than fatal.
func TestReadTreeSkipsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "real.txt", "kept\n")
	if err := os.MkdirAll(filepath.Join(root, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target"), filepath.Join(root, "link-to-dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link-to-file")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	tree, err := ReadTree(root)
	if err != nil {
		t.Fatalf("export should survive symlinks: %v", err)
	}
	got := map[string]bool{}
	tree.walk("", func(path string, _ []byte) error { got[path] = true; return nil })
	if !got["real.txt"] {
		t.Error("real file missing from the bundle")
	}
	for _, skipped := range []string{"link-to-dir", "link-to-file"} {
		if got[skipped] {
			t.Errorf("%s should have been skipped", skipped)
		}
	}
}
