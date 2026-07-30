// The Go method-id constants are mirrored by hand from the .proto files until
// protoc-gen-dlc-registry generates them. Nothing else catches a mismatch: the
// parity check uses the Go constants on BOTH sides, so it would happily compare
// a wrong id against itself and pass. The web host reads its ids from the proto,
// so a drift would surface only as a browser bug — far from the cause.
//
// This test reads the protos as text and asserts they agree with the constants.
// Native-only (it reads the repo), which is fine: it guards source, not runtime.
package engine_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/dlc-platform"
)

// rpc Foo(FooRequest) returns (FooResponse) { option (…method_id) = N; }
var rpcRE = regexp.MustCompile(`(?s)rpc\s+(\w+)\s*\([^)]*\)\s*returns\s*\([^)]*\)\s*\{.*?method_id\)\s*=\s*(\d+)`)

func protoIDs(t *testing.T, path string) map[string]uint32 {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]uint32{}
	for _, m := range rpcRE.FindAllStringSubmatch(string(src), -1) {
		id, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil {
			t.Fatalf("%s: %v", m[1], err)
		}
		out[m[1]] = uint32(id)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no rpcs found — did the file move or the syntax change?", path)
	}
	return out
}

func TestMethodIDsMatchProto(t *testing.T) {
	platformIDs := protoIDs(t, "../dlc-platform/proto/devalbo/ilc/v1/platform.proto")
	appIDs := protoIDs(t, "../proto/devalbo/dlc/v1/commands.proto")

	for name, want := range map[string]struct {
		got uint32
		ids map[string]uint32
	}{
		"Version":  {platform.MethodVersion, platformIDs},
		"ExportFs": {platform.MethodExportFs, platformIDs},
		"ImportFs": {platform.MethodImportFs, platformIDs},
		"ResetFs":  {platform.MethodResetFs, platformIDs},
		"New":      {engine.MethodNew, appIDs},
		"Echo":     {engine.MethodEcho, appIDs},
	} {
		declared, ok := want.ids[name]
		if !ok {
			t.Errorf("%s: no rpc with that name in the proto", name)
			continue
		}
		if declared != want.got {
			t.Errorf("%s: Go says %d, proto says %d", name, want.got, declared)
		}
	}
}

// The ranges are the platform/app/dlc contract; a stray id on the wrong side of a
// line is exactly what the bands exist to prevent.
//
// Every boundary is QUOTED from a constant, never retyped — the message this test
// replaced said "1–9999" while comparing against a constant that read 1000, which
// is the failure mode a range test is least entitled to have.
func TestMethodIDsRespectRanges(t *testing.T) {
	// The platform's inherited verbs sit below dlc's block, which sits below the
	// app band.
	for name, id := range protoIDs(t, "../dlc-platform/proto/devalbo/ilc/v1/platform.proto") {
		if id == 0 || id >= engine.DlcMethodBase {
			t.Errorf("platform %s = %d, must be in 1–%d (ILC's inherited verbs)",
				name, id, engine.DlcMethodBase-1)
		}
	}
	// dlc's ENGINE-SERVED verbs: 9000–9099.
	for name, id := range protoIDs(t, "../proto/devalbo/dlc/v1/commands.proto") {
		if id < engine.DlcMethodBase || id >= engine.DlcHostLocalBase {
			t.Errorf("dlc %s = %d, must be in %d–%d (dlc's engine-served block)",
				name, id, engine.DlcMethodBase, engine.DlcHostLocalBase-1)
		}
	}
	// dlc's HOST-LOCAL verbs: 9100 up to, but not into, the app band.
	for name, id := range protoIDs(t, "../proto/devalbo/dlc/v1/toolchain.proto") {
		if id < engine.DlcHostLocalBase || id >= platform.AppMethodBase {
			t.Errorf("dlc toolchain %s = %d, must be in %d–%d (dlc's host-local block)",
				name, id, engine.DlcHostLocalBase, platform.AppMethodBase-1)
		}
	}
	// And the app band belongs to apps. notes is the stand-in: if a framework id
	// ever leaks into an example app, that is the same mistake from the other
	// side.
	for name, id := range protoIDs(t, "../example-apps/notes/proto/notes/v1/commands.proto") {
		if id < platform.AppMethodBase {
			t.Errorf("app %s = %d, must be >= %d (below that is the framework's)",
				name, id, platform.AppMethodBase)
		}
	}
}

// The scaffold carries its own copy of options.proto (a generated project has no
// other way to resolve `method_id` until the platform is published). A copy that
// drifts from the original is the classic vendoring failure — an app would
// generate against options the platform does not actually read.
func TestTemplateOptionsProtoInSync(t *testing.T) {
	// The vendored copies keep the path they had; the SOURCE now lives in the
	// platform module, which is what they are a copy OF.
	const rel = "proto/devalbo/options/v1/options.proto"
	source, err := os.ReadFile("../dlc-platform/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	vendored, err := os.ReadFile("../templates/component-model/" + rel + ".tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != string(vendored) {
		t.Error("templates/…/options.proto has drifted — run `make sync-template-proto`")
	}

	// The EXAMPLE APPS vendor the same file, and until now nothing checked them.
	// Adding `cli_name` to the platform's copy broke notes' codegen with an error
	// that named neither this file nor the app — buf cancels every plugin when
	// one fails, so the real cause was three layers down. An example app is a
	// worked demonstration of current practice; one built against a stale copy of
	// the platform's own schema teaches the wrong thing.
	apps, err := filepath.Glob("../example-apps/*/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Error("no example app vendors options.proto — if that is deliberate, delete this check")
	}
	for _, path := range apps {
		appCopy, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(source) != string(appCopy) {
			t.Errorf("%s has drifted from the platform's options.proto — run `make sync-template-proto`", path)
		}
	}
}

// Every file under templates/ must declare its intent with a suffix: `.tmpl`
// (substitute tokens) or `.raw` (copy verbatim). The suffix does double duty —
// it declares what the renderer does, and it keeps the Go tool out. Without it,
// a template `.go` file is not valid Go, and a file named `go.mod` makes the
// directory a nested module that embed refuses outright. Neither failure points
// at the template that caused it, so the rule is enforced rather than remembered.
func TestTemplateFilesAreSuffixed(t *testing.T) {
	err := filepath.WalkDir("../templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		// templates.go is the embed declaration, not a template.
		if filepath.Ext(path) == ".go" && filepath.Dir(path) == "../templates" {
			return nil
		}
		if filepath.Base(path) == "README.md" && filepath.Dir(path) == "../templates" {
			return nil
		}
		switch filepath.Ext(path) {
		case ".tmpl", ".raw":
		default:
			t.Errorf("%s: every template file must end in .tmpl (substituted) or .raw (verbatim) — see templates/README.md", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
