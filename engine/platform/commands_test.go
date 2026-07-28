// The platform's own verbs, tested where they live. An app inherits these, so a
// regression here is a regression in every app — which is the whole argument for
// them being platform code rather than template code.
package platform

import (
	"os"
	"path/filepath"
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
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
	// standing" any more: `Root()` panics without a grant, because falling back
	// to the cwd is what let `reset-fs` clear a user's directory.
	if err := SetRoot("."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return root
}

func seed(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(Root(), path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// MERGE cannot express a deletion — that is exactly why REPLACE exists.
func TestImportMergeKeepsExistingFiles(t *testing.T) {
	inTempRoot(t)
	seed(t, map[string]string{"app/keep.txt": "old", "app/a.txt": "old"})

	bundle := []byte(`{"type":"directory","entries":{"a.txt":{"type":"text","content":"new"}}}`)
	if _, err := handleImportFs(&ilcv1.ImportFsRequest{
		Bundle: bundle, Prefix: "app", Mode: ilcv1.ImportMode_IMPORT_MODE_MERGE,
	}); err != nil {
		t.Fatal(err)
	}

	if got := read(t, "app/a.txt"); got != "new" {
		t.Errorf("a.txt should be overwritten: %q", got)
	}
	if got := read(t, "app/keep.txt"); got != "old" {
		t.Errorf("merge must not delete: keep.txt = %q", got)
	}
}

func TestImportReplaceClearsFirst(t *testing.T) {
	inTempRoot(t)
	seed(t, map[string]string{"app/gone.txt": "old", "app/nested/deep.txt": "old"})

	bundle := []byte(`{"type":"directory","entries":{"only.txt":{"type":"text","content":"new"}}}`)
	if _, err := handleImportFs(&ilcv1.ImportFsRequest{
		Bundle: bundle, Prefix: "app", Mode: ilcv1.ImportMode_IMPORT_MODE_REPLACE,
	}); err != nil {
		t.Fatal(err)
	}

	if got := read(t, "app/only.txt"); got != "new" {
		t.Errorf("only.txt = %q", got)
	}
	for _, gone := range []string{"app/gone.txt", "app/nested/deep.txt"} {
		if _, err := os.Stat(filepath.Join(Root(), gone)); err == nil {
			t.Errorf("%s should have been replaced away", gone)
		}
	}
}

func TestResetFs(t *testing.T) {
	inTempRoot(t)
	seed(t, map[string]string{"app/a.txt": "x", "app/sub/b.txt": "y", "other/c.txt": "z"})

	resp, err := handleResetFs(&ilcv1.ResetFsRequest{Prefix: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Removed) != 2 || resp.Removed[0] != "a.txt" || resp.Removed[1] != "sub" {
		t.Errorf("removed = %v, want [a.txt sub]", resp.Removed)
	}
	// The subtree's own directory survives — only its contents go.
	if _, err := os.Stat(filepath.Join(Root(), "app")); err != nil {
		t.Errorf("the prefix directory itself should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(Root(), "app", "a.txt")); err == nil {
		t.Error("a.txt survived reset")
	}
	// Untouched siblings stay untouched.
	if got := read(t, "other/c.txt"); got != "z" {
		t.Errorf("reset reached outside its prefix: %q", got)
	}
}

// Reset on nothing is a no-op, not an error — it has to be safe to call before
// an import without checking first.
func TestResetFsIdempotent(t *testing.T) {
	inTempRoot(t)
	resp, err := handleResetFs(&ilcv1.ResetFsRequest{Prefix: "never-existed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Removed) != 0 {
		t.Errorf("removed = %v, want none", resp.Removed)
	}
}

func TestResetFsRefusesEscapingPrefix(t *testing.T) {
	inTempRoot(t)
	for _, prefix := range []string{"../..", "/", "a/../../.."} {
		if _, err := handleResetFs(&ilcv1.ResetFsRequest{Prefix: prefix}); err == nil {
			t.Errorf("%q: expected refusal", prefix)
		}
	}
}

// An app may take over a platform verb, but only deliberately: a plain
// re-Register panics, and Unregister is the explicit opt-in. (Nothing is
// registered in this package's own test binary — RegisterAll is the app's call —
// so the fixture registers first.)
func TestRegisterRefusesSilentOverride(t *testing.T) {
	const id uint32 = 4242
	noop := func([]byte) Result { return Result{Success: true} }
	Register(id, noop)
	t.Cleanup(func() { Unregister(id) })

	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected a panic on duplicate registration")
			}
		}()
		Register(id, noop)
	}()

	// …and the deliberate path works.
	if !Unregister(id) {
		t.Fatal("Unregister reported nothing was registered")
	}
	Register(id, noop)
	if r := Execute(id, nil); !r.Success {
		t.Error("re-registered handler did not run")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Root(), path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
