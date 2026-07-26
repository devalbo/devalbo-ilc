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
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/devalbo-ilc/engine/platform"
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
	platformIDs := protoIDs(t, "../proto/devalbo/ilc/v1/platform.proto")
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

// The ranges are the platform/app contract; a stray id on the wrong side of the
// line is exactly what the reserved range exists to prevent.
func TestMethodIDsRespectRanges(t *testing.T) {
	for name, id := range protoIDs(t, "../proto/devalbo/ilc/v1/platform.proto") {
		if id == 0 || id >= platform.AppMethodBase {
			t.Errorf("platform %s = %d, must be in 1–%d", name, id, platform.AppMethodBase-1)
		}
	}
	for name, id := range protoIDs(t, "../proto/devalbo/dlc/v1/commands.proto") {
		if id < platform.AppMethodBase {
			t.Errorf("app %s = %d, must be >= %d (1–%d is reserved for the platform)",
				name, id, platform.AppMethodBase, platform.AppMethodBase-1)
		}
	}
}
