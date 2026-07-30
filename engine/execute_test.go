// Registry dispatch (Decision 29): every command reached by method_id, request
// and response crossing as flat proto bytes. Run: go test ./engine/
package engine_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devalbo/devalbo-ilc/engine"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	"github.com/devalbo/devalbo-ilc/templates"
	"github.com/devalbo/dlc-platform"
	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"
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
	// BOOT, as a host does — not merely SetRoot. dlc's engine registers its
	// filesystem verbs from the manifest (RegisterDiscovered), so a test that
	// only granted a root would find `export-fs` reporting "unknown method_id
	// 100". That is the mandatory ordering in docs/ENVIRONMENT-PLAN.md §2.5
	// biting exactly where it should: a caller that skips the manifest gets a
	// half-registered engine, and a test is a caller like any other.
	if err := platform.Boot(platform.BootOptions{
		Root:           ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return root
}

func TestNew(t *testing.T) {
	inTempRoot(t)

	var resp dlcv1.NewResponse
	out := call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp", Tiers: []string{"native", "web"}})
	if err := resp.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	if resp.Path != "myapp" {
		t.Errorf("path: got %q, want %q", resp.Path, "myapp")
	}
	// The files a scaffold cannot be without, plus the ordering invariant. NOT an
	// exhaustive list: the template is still growing, and pinning every path made
	// each addition look like a regression three times in a row. The exhaustive
	// version is the golden FS snapshot (§11), which is still an open task and
	// belongs in verify/ with the other goldens, not here.
	required := []string{
		"AGENTS.md", // the rules travel with the project, not just with dlc
		"go.mod",
		"engine/commands.go",
		"hosts/native/main.go",
		"cmd/engine-component/main.go",
		"proto/myapp/v1/commands.proto",
		"hosts/web/src/main.ts",
	}
	have := map[string]bool{}
	for _, f := range resp.Files {
		have[f] = true
	}
	for _, want := range required {
		if !have[want] {
			t.Errorf("scaffold is missing %s (got %v)", want, resp.Files)
		}
	}
	// Sorted order IS part of the contract — the parity check diffs the written
	// trees byte-for-byte across native and wasm.
	for i := 1; i < len(resp.Files); i++ {
		if resp.Files[i-1] >= resp.Files[i] {
			t.Errorf("files not sorted/unique at %d: %q then %q", i, resp.Files[i-1], resp.Files[i])
		}
	}
}

// The files must actually land on disk with the substituted content — the whole
// point of `new`, and what the golden FS snapshot will assert (§11).
func TestNewWritesTree(t *testing.T) {
	root := inTempRoot(t)

	call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp", Module: "github.com/acme/myapp", Tiers: []string{"native", "web"}})

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

	in, err := (&dlcv1.NewRequest{Name: "myapp", Tiers: []string{"native", "web"}}).MarshalVT()
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
		in, err := (&dlcv1.NewRequest{Name: name, Tiers: []string{"native", "web"}}).MarshalVT()
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
	in, err := (&dlcv1.NewRequest{Tiers: []string{"native", "web"}}).MarshalVT()
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
	if err := scaffolded.UnmarshalVT(call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "myapp", Tiers: []string{"native", "web"}})); err != nil {
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

	// Each refusal asserts its OWN wording rather than a phrase shared by all
	// four. The shared "not supported yet" was a proxy for "helpful", and it
	// stopped being one when tiers grew a third answer: a DECLARED-but-unbuilt
	// tier is a different mistake from a name that is not a tier at all, and a
	// test that cannot tell them apart cannot notice if they get collapsed again.
	cases := map[string]struct {
		req   *dlcv1.NewRequest
		wants string
	}{
		// Not a tier at all — "embedded" is a category, and a tier is a binding.
		"tier": {&dlcv1.NewRequest{Name: "a", Tiers: []string{"embedded"}}, "is not a tier"},
		// The others carry a valid tier set, so the refusal under test is the
		// one named — not the missing-tiers refusal standing in for it.
		"cap":     {&dlcv1.NewRequest{Name: "b", Tiers: []string{"native"}, Caps: []string{"sqlite"}}, "not supported yet"},
		"ui":      {&dlcv1.NewRequest{Name: "c", Tiers: []string{"native"}, Ui: dlcv1.UiKind_UI_KIND_REACT}, "not supported yet"},
		"storage": {&dlcv1.NewRequest{Name: "d", Tiers: []string{"native"}, Storage: dlcv1.StorageKind_STORAGE_KIND_SPLIT}, "not supported yet"},
	}
	for name, tc := range cases {
		req := tc.req
		in, err := req.MarshalVT()
		if err != nil {
			t.Fatal(err)
		}
		r := engine.ExecuteMethod(engine.MethodNew, in)
		if r.Success {
			t.Errorf("%s: expected refusal", name)
			continue
		}
		if !strings.Contains(r.Err, tc.wants) {
			t.Errorf("%s: error %q does not say %q", name, r.Err, tc.wants)
		}
		// A refused request must not leave a half-written tree behind.
		if _, err := os.Stat(req.Name); err == nil {
			t.Errorf("%s: scaffolded despite refusing", name)
		}
	}

	// An EMPTY tier list is its own refusal — no default, on any tier.
	if r := engine.ExecuteMethod(engine.MethodNew, mustMarshal(t, &dlcv1.NewRequest{Name: "notiers"})); r.Success {
		t.Error("an empty tier list must be refused, not defaulted")
	} else if !strings.Contains(r.Err, "no tiers requested") {
		t.Errorf("unhelpful error for empty tiers: %q", r.Err)
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

// --tiers now decides what gets emitted, which is the whole point of recording
// it: a native-only project should not carry a web slot it never builds.
func TestNewTiersSelectFiles(t *testing.T) {
	inTempRoot(t)

	var nativeOnly dlcv1.NewResponse
	out := call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "cli", Tiers: []string{"native"}})
	if err := nativeOnly.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	for _, f := range nativeOnly.Files {
		if strings.HasPrefix(f, "hosts/web/") || strings.HasPrefix(f, "cmd/engine-component/") {
			t.Errorf("native-only scaffold emitted a web-tier file: %s", f)
		}
	}
	// …and the manifest agrees with what was emitted.
	manifest, err := os.ReadFile(filepath.Join("cli", "dlc.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "[tiers.web]") {
		t.Error("native-only project declares [tiers.web]")
	}
	if !strings.Contains(string(manifest), "[tiers.native]") {
		t.Error("manifest is missing [tiers.native]")
	}

	// The default is BOTH tiers — the cross-tier story is the product.
	var both dlcv1.NewResponse
	out = call(t, engine.MethodNew, &dlcv1.NewRequest{Name: "full", Tiers: []string{"native", "web"}})
	if err := both.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	var hasWebSlot bool
	for _, f := range both.Files {
		if strings.HasPrefix(f, "hosts/web/") {
			hasWebSlot = true
		}
	}
	if !hasWebSlot {
		t.Error("default scaffold has no web tier")
	}
	if len(both.Files) <= len(nativeOnly.Files) {
		t.Errorf("both-tier scaffold (%d files) should exceed native-only (%d)",
			len(both.Files), len(nativeOnly.Files))
	}
}

// THE TIER LANDSCAPE AND THE TEMPLATE MUST AGREE, in both directions.
//
// `engine/tiers.go` names every tier; the template's `hosts/*` directories decide
// which can be emitted. Neither is the single source: the landscape alone cannot
// know whether a skeleton exists, and derivation alone leaves no place that names
// a tier — a typo'd `hosts/webb/` would silently become one.
//
// Invariants rather than a pinned list, so the next tier is not a regression
// (AGENTS.md §5).
func TestLandscapeAndTemplateAgree(t *testing.T) {
	slots := map[string]bool{}
	entries, err := fs.ReadDir(templates.FS, templates.Root+"/hosts")
	if err != nil {
		t.Fatalf("reading template host slots: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			slots[e.Name()] = true
		}
	}
	if len(slots) == 0 {
		t.Fatal("the template has no hosts/ slots — every tier would be refused")
	}

	declared := map[string]bool{}
	for _, spec := range engine.TierLandscape {
		declared[spec.Name] = true
		if spec.What == "" {
			t.Errorf("tier %q has no description; the landscape is also the docs", spec.Name)
		}
	}

	// Every slot the template ships must be a tier we have named. A directory
	// nobody declared is a template that outgrew its vocabulary.
	for slot := range slots {
		if !declared[slot] {
			t.Errorf("the template has hosts/%s/ but no tier is declared for it", slot)
		}
	}

	// And every row that CLAIMS availability must have a slot to emit from.
	//
	// This iterates the landscape, not SupportedTiers() — which was the first
	// version and was a tautology: SupportedTiers() is already filtered by the
	// slots, so it could not disagree with them. Falsification caught it (mark
	// `desktop` available and the test stayed green), which is the whole reason
	// for the practice. A row lying about availability is silently downgraded at
	// run time, so this assertion is the only thing that can see it.
	for _, spec := range engine.TierLandscape {
		if spec.Status == engine.TierAvailable && !slots[spec.Name] {
			t.Errorf("tier %q claims TierAvailable but the template has no hosts/%s/",
				spec.Name, spec.Name)
		}
	}
}

// A tier that is DECLARED but unbuilt is refused differently from a nonexistent
// one, because they are different mistakes: one is the roadmap, the other is a
// typo.
func TestPlannedTierIsRefusedByName(t *testing.T) {
	for _, tc := range []struct{ tier, wants string }{
		{engine.TierBadgeNative, "declared but has no template slot"},
		{"hologram", "is not a tier"},
	} {
		in, err := (&dlcv1.NewRequest{Name: "t-" + tc.tier, Tiers: []string{"native", tc.tier}}).MarshalVT()
		if err != nil {
			t.Fatal(err)
		}
		r := engine.ExecuteMethod(engine.MethodNew, in)
		if r.Success {
			t.Fatalf("scaffolded with tier %q", tc.tier)
		}
		if !strings.Contains(r.Err, tc.tier) {
			t.Errorf("error for %q does not name it: %q", tc.tier, r.Err)
		}
		if !strings.Contains(r.Err, tc.wants) {
			t.Errorf("error for %q = %q, want it to say %q", tc.tier, r.Err, tc.wants)
		}
	}
}
