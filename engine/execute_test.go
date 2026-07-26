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
	want := []string{"go.mod", "engine/execute.go", "README.md"}
	if len(resp.Files) != len(want) {
		t.Fatalf("files: got %v, want %v", resp.Files, want)
	}
	// Order is part of the contract — the parity check diffs bytes.
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

	want := map[string]string{
		"go.mod":            "module github.com/acme/myapp\n\ngo 1.23.0\n",
		"README.md":         "# myapp\n\nScaffolded by `dlc new`. Module `github.com/acme/myapp`.\n",
		"engine/execute.go": "", // content checked for the token only
	}
	for name, wantContent := range want {
		got, err := os.ReadFile(filepath.Join(root, "myapp", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if wantContent != "" && string(got) != wantContent {
			t.Errorf("%s: got %q, want %q", name, got, wantContent)
		}
		if strings.Contains(string(got), "{{.") {
			t.Errorf("%s: unsubstituted token in %q", name, got)
		}
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

	call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp"})

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
	want := []string{"README.md", "engine/execute.go", "go.mod"}
	if len(imported.Files) != len(want) {
		t.Fatalf("imported %v, want %v", imported.Files, want)
	}
	for i, w := range want {
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
