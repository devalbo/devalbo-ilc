// Registry dispatch (Decision 29): every command reached by method_id, request
// and response crossing as flat proto bytes. Run: go test ./engine/
package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/devalbo-ilc/engine/platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
)

// call marshals a request, dispatches on method_id, and fails on an error result.
func call(t *testing.T, method uint32, req interface{ MarshalVT() ([]byte, error) }) []byte {
	t.Helper()
	in, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := engine.ExecuteMethod(method, in)
	if !r.Success {
		t.Fatalf("method %d: %s", method, r.Err)
	}
	return r.Output
}

func TestVersion(t *testing.T) {
	var resp ilcv1.VersionResponse
	if err := resp.UnmarshalVT(call(t, platform.MethodVersion, &ilcv1.VersionRequest{})); err != nil {
		t.Fatal(err)
	}
	if resp.Version == "" {
		t.Error("empty version")
	}
}

func TestEcho(t *testing.T) {
	var resp dlcv1.EchoResponse
	out := call(t, engine.MethodEcho, &dlcv1.EchoRequest{Args: []string{"hello", "world"}})
	if err := resp.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello world" {
		t.Errorf("got %q, want %q", resp.Text, "hello world")
	}
}

// inTempRoot runs fn with the process cwd pointed at a fresh directory — the
// native stand-in for the host-bound filesystem root (§5.2). The engine writes
// relative to that root, so this is what a browser's OPFS preopen does natively.
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
	t.Cleanup(func() { os.Chdir(prev) })
	return root
}

func TestNew(t *testing.T) {
	inTempRoot(t)

	var resp dlcv1.NewResponse
	out := call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp"})
	if err := resp.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	if resp.Path != "myapp" {
		t.Errorf("path: got %q, want %q", resp.Path, "myapp")
	}
	// The full skeleton, sorted — order is part of the contract, because the
	// parity check diffs the written trees byte-for-byte.
	want := []string{
		".gitignore",
		"Makefile",
		"README.md",
		"devbox.json",
		"engine/commands.go",
		"engine/commands_test.go",
		"go.mod",
		"hosts/native/main.go",
		"proto/buf.gen.yaml",
		"proto/buf.yaml",
		"proto/devalbo/options/v1/README.md",
		"proto/devalbo/options/v1/options.proto",
		"proto/myapp/v1/commands.proto",
	}
	if len(resp.Files) != len(want) {
		t.Fatalf("files: got %v, want %v", resp.Files, want)
	}
	for i, w := range want {
		if resp.Files[i] != w {
			t.Errorf("files[%d]: got %q, want %q", i, resp.Files[i], w)
		}
	}
}

// The files must actually land on disk with the substituted content — the whole
// point of `new`, and what the golden FS snapshot will assert (§11).
func TestNewWritesTree(t *testing.T) {
	root := inTempRoot(t)

	call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp", Module: "github.com/acme/myapp"})

	// Every emitted file must be fully substituted — a stray {{.Token}} is the
	// classic scaffolder bug, and it compiles fine until someone reads it.
	var checked int
	err := filepath.Walk(filepath.Join(root, "myapp"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "{{.") {
			t.Errorf("%s: unsubstituted token", path)
		}
		if strings.Contains(path, "{{") {
			t.Errorf("%s: unsubstituted token in the PATH", path)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Errorf("only %d files written; expected the full skeleton", checked)
	}

	goMod, err := os.ReadFile(filepath.Join(root, "myapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMod), "module github.com/acme/myapp") {
		t.Errorf("go.mod: %q", goMod)
	}
	// The template file is go.mod.tmpl (a literal go.mod would make the
	// template a nested module and break embedding) — the suffix must be gone.
	if _, err := os.Stat(filepath.Join(root, "myapp", "go.mod.tmpl")); err == nil {
		t.Error("go.mod.tmpl was emitted verbatim; the .tmpl suffix should be stripped")
	}
}

// Scaffolding must never silently overwrite existing work.
func TestNewRefusesOccupiedDir(t *testing.T) {
	root := inTempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "myapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "myapp", "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	in, err := (&dlcv1.NewRequest{Name: "myapp"}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if r := engine.ExecuteMethod(engine.MethodNew, in); r.Success {
		t.Fatal("expected failure scaffolding over a non-empty dir")
	}
	got, err := os.ReadFile(filepath.Join(root, "myapp", "keep.txt"))
	if err != nil || string(got) != "mine" {
		t.Errorf("pre-existing file was disturbed: %q, %v", got, err)
	}
}

// A name that escapes the host-bound root must be refused — `import-fs` will
// accept untrusted bundles through the same safeJoin door.
func TestNewRejectsEscapingNames(t *testing.T) {
	inTempRoot(t)
	for _, name := range []string{"../evil", "/etc/evil", "a/../../evil"} {
		in, err := (&dlcv1.NewRequest{Name: name}).MarshalVT()
		if err != nil {
			t.Fatal(err)
		}
		if r := engine.ExecuteMethod(engine.MethodNew, in); r.Success {
			t.Errorf("%q: expected refusal", name)
		}
	}
}

// A missing name is a command error, carried by the envelope rather than a
// response field (Decision 28).
func TestNewRequiresName(t *testing.T) {
	in, err := (&dlcv1.NewRequest{}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if r := engine.ExecuteMethod(engine.MethodNew, in); r.Success {
		t.Error("expected failure for a nameless new")
	}
}

// An unknown id must fail cleanly, never panic — hosts and engines version
// independently, so a host may well ask for a command this engine lacks.
func TestUnknownMethod(t *testing.T) {
	if r := engine.ExecuteMethod(9999, nil); r.Success {
		t.Error("expected failure for an unknown method_id")
	}
}

// export-fs → import-fs across the command boundary: scaffold, bundle the tree,
// import it somewhere else, and get the same files. This is the browser's
// "download my project" and `dlc new` in one round-trip (§7.3).
func TestExportImportRoundTrip(t *testing.T) {
	inTempRoot(t)

	var scaffolded dlcv1.NewResponse
	if err := scaffolded.UnmarshalVT(call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp"})); err != nil {
		t.Fatal(err)
	}

	var exported ilcv1.ExportFsResponse
	out := call(t, platform.MethodExportFs, &ilcv1.ExportFsRequest{Prefix: "myapp"})
	if err := exported.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	bundle := string(exported.Bundle)
	if !strings.Contains(bundle, `"type": "directory"`) {
		t.Fatalf("not a BFT bundle:\n%s", bundle)
	}
	// The scaffold is all text, so the bundle should be readable — that is the
	// point of BFT over a zip.
	if !strings.Contains(bundle, "module github.com/you/myapp") {
		t.Errorf("expected readable go.mod content in the bundle:\n%s", bundle)
	}

	var imported ilcv1.ImportFsResponse
	out = call(t, platform.MethodImportFs, &ilcv1.ImportFsRequest{
		Bundle: exported.Bundle,
		Prefix: "restored",
	})
	if err := imported.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	// Everything the scaffold wrote comes back, in the same order — the bundle
	// is sorted, which is what makes it byte-stable across tiers.
	if len(imported.Files) != len(scaffolded.Files) {
		t.Fatalf("imported %d files, scaffolded %d: %v", len(imported.Files), len(scaffolded.Files), imported.Files)
	}
	for i, w := range scaffolded.Files {
		if imported.Files[i] != w {
			t.Errorf("files[%d]: got %q, want %q", i, imported.Files[i], w)
		}
	}

	// Same bytes, not just the same names.
	original, err := os.ReadFile(filepath.Join("myapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join("restored", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(restored) {
		t.Errorf("go.mod differs after round-trip: %q vs %q", original, restored)
	}
}

// zip and proto are declared in the schema but not implemented — say so rather
// than quietly returning BFT under another name.
func TestExportRejectsUnimplementedFormats(t *testing.T) {
	inTempRoot(t)
	in, err := (&ilcv1.ExportFsRequest{Format: ilcv1.BundleFormat_BUNDLE_FORMAT_ZIP}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := engine.ExecuteMethod(platform.MethodExportFs, in)
	if r.Success {
		t.Fatal("expected refusal for zip")
	}
	if !strings.Contains(r.Err, "only BFT") {
		t.Errorf("unhelpful error: %q", r.Err)
	}
}

// A prefix must not be able to reach outside the host-bound root.
func TestExportImportRejectEscapingPrefix(t *testing.T) {
	inTempRoot(t)
	in, err := (&ilcv1.ExportFsRequest{Prefix: "../.."}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if r := engine.ExecuteMethod(platform.MethodExportFs, in); r.Success {
		t.Error("expected refusal for an escaping prefix")
	}
}

// The scaffold options exist in the schema but the single template emits one
// shape. Accepting them and quietly emitting something else would hand the user
// a project that contradicts what they asked for — so they are refused by name.
func TestNewRejectsUnsupportedOptions(t *testing.T) {
	inTempRoot(t)

	cases := map[string]*dlcv1.NewRequest{
		"tier":    {Name: "a", Tiers: []string{"web"}},
		"cap":     {Name: "b", Caps: []string{"sqlite"}},
		"ui":      {Name: "c", Ui: dlcv1.UiKind_UI_KIND_REACT},
		"storage": {Name: "d", Storage: dlcv1.StorageKind_STORAGE_KIND_SPLIT},
	}
	for name, req := range cases {
		in, err := req.MarshalVT()
		if err != nil {
			t.Fatal(err)
		}
		r := engine.ExecuteMethod(engine.MethodNew, in)
		if r.Success {
			t.Errorf("%s: expected refusal", name)
			continue
		}
		if !strings.Contains(r.Err, "not supported yet") {
			t.Errorf("%s: unhelpful error %q", name, r.Err)
		}
		// A refused request must not leave a half-written tree behind.
		if _, err := os.Stat(req.Name); err == nil {
			t.Errorf("%s: scaffolded despite refusing", name)
		}
	}

	// The supported subset still works.
	if r := engine.ExecuteMethod(engine.MethodNew, mustMarshal(t, &dlcv1.NewRequest{
		Name:  "ok",
		Caps:  []string{"console", "filesystem"},
		Tiers: []string{"native"},
	})); !r.Success {
		t.Errorf("supported options were refused: %s", r.Err)
	}
}

func mustMarshal(t *testing.T, m interface{ MarshalVT() ([]byte, error) }) []byte {
	t.Helper()
	b, err := m.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
