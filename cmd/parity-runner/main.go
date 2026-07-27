// parity-runner — the native half of the `execute(method, request)` parity check
// (Decision 26/31). It is a DEV TOOL, not a shipped host: it streams golden
// method-id + proto-request vectors through the in-process engine and prints one
// `<success>\t<base64(output)>\t<error>` line each, exactly as verify/parity's
// harness.mjs does for the wasip2 component. scripts/verify-parity.sh diffs the
// two streams.
//
// The argv half of the check runs the real `dlc` binary. This runner exists
// because hosts/native does not yet build requests host-side (that lands with
// the host-side parser); when it does, the real binary replaces this.
//
// Two modes:
//
//	parity-runner -gen  <file>   rewrite the vectors file from the fixtures below
//	parity-runner       <file>   run the vectors, one result line each
//
// The requests are hex in the JSON but are *derived* from typed messages here —
// never hand-authored — so the goldens stay honest when the schema moves. This
// mirrors the Spike 2 golden.hex discipline. encoding/json is fine in this
// package: it is native-only and never crosses into the engine.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/devalbo-ilc/engine/platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
)

// vector is one golden call across the boundary. Request is hex-encoded proto
// bytes so both runners decode identically without needing an encoder.
type vector struct {
	Name    string `json:"name"`
	Method  uint32 `json:"method"`
	Request string `json:"request"`
}

// marshaler is what every go-lite request message satisfies.
type marshaler interface{ MarshalVT() ([]byte, error) }

// fixtures are the source of truth for the vectors file. Cover the happy path,
// the envelope-carried errors, and the two ways dispatch can fail — an
// unregistered id and a request that will not decode. TinyGo and native Go must
// agree on all of them, error strings included.
//
// Every `new` fixture uses a DISTINCT app name. `new` writes a real tree and
// refuses to overwrite one, so sharing a name would make each vector's result
// depend on whether an earlier vector already created it — the vectors must be
// order-independent within a run.
var fixtures = []struct {
	name    string
	method  uint32
	request marshaler
	raw     string // hex, for deliberately malformed bytes (wins over request)
}{
	{name: "version", method: platform.MethodVersion, request: &ilcv1.VersionRequest{}},
	{name: "echo hello world", method: engine.MethodEcho, request: &dlcv1.EchoRequest{Args: []string{"hello", "world"}}},
	{name: "echo empty", method: engine.MethodEcho, request: &dlcv1.EchoRequest{}},
	{name: "new (defaults)", method: engine.MethodNew, request: &dlcv1.NewRequest{Name: "app-default"}},
	{name: "new with module", method: engine.MethodNew, request: &dlcv1.NewRequest{Name: "app-module", Module: "github.com/acme/app-module"}},
	{name: "new with enums", method: engine.MethodNew, request: &dlcv1.NewRequest{
		Name:    "app-enums",
		Caps:    []string{"console", "filesystem"},
		Tiers:   []string{"native", "web"},
		Ui:      dlcv1.UiKind_UI_KIND_REACT,
		Storage: dlcv1.StorageKind_STORAGE_KIND_SPLIT,
	}},
	{name: "new missing name (envelope error)", method: engine.MethodNew, request: &dlcv1.NewRequest{}},
	// export-fs runs AFTER the `new` fixtures above, so it bundles a root that
	// already has trees in it — the vector is only meaningful because the
	// ordering is fixed and the root starts empty.
	{name: "export-fs whole root (BFT)", method: platform.MethodExportFs, request: &ilcv1.ExportFsRequest{}},
	{name: "export-fs subtree", method: platform.MethodExportFs, request: &ilcv1.ExportFsRequest{Prefix: "app-default"}},
	{name: "export-fs unimplemented format", method: platform.MethodExportFs, request: &ilcv1.ExportFsRequest{Format: ilcv1.BundleFormat_BUNDLE_FORMAT_ZIP}},
	{name: "import-fs a hand-written bundle", method: platform.MethodImportFs, request: &ilcv1.ImportFsRequest{
		Prefix: "imported",
		Bundle: []byte("{\n  \"type\": \"directory\",\n  \"entries\": {\n    \"hello.txt\": {\n      \"type\": \"text\",\n      \"content\": \"hi \\u00e9\\n\"\n    },\n    \"data.bin\": {\n      \"type\": \"binary\",\n      \"encoding\": \"base64\",\n      \"content\": \"AAECAwQF\"\n    }\n  }\n}\n"),
	}},
	{name: "import-fs replace mode", method: platform.MethodImportFs, request: &ilcv1.ImportFsRequest{
		Prefix: "imported",
		Mode:   ilcv1.ImportMode_IMPORT_MODE_REPLACE,
		Bundle: []byte("{\"type\":\"directory\",\"entries\":{\"only.txt\":{\"type\":\"text\",\"content\":\"replaced\\n\"}}}"),
	}},
	{name: "reset-fs a subtree", method: platform.MethodResetFs, request: &ilcv1.ResetFsRequest{Prefix: "app-enums"}},
	{name: "reset-fs a missing subtree (idempotent)", method: platform.MethodResetFs, request: &ilcv1.ResetFsRequest{Prefix: "never-existed"}},
	{name: "reset-fs escaping prefix", method: platform.MethodResetFs, request: &ilcv1.ResetFsRequest{Prefix: "../.."}},
	{name: "import-fs empty bundle", method: platform.MethodImportFs, request: &ilcv1.ImportFsRequest{}},
	{name: "import-fs malformed bundle", method: platform.MethodImportFs, request: &ilcv1.ImportFsRequest{Bundle: []byte("not json")}},
	{name: "unknown method_id", method: 9999, request: &ilcv1.VersionRequest{}},
	{name: "malformed request bytes", method: engine.MethodNew, raw: "ff"},
}

func main() {
	args := os.Args[1:]
	gen := len(args) > 0 && args[0] == "-gen"
	if gen {
		args = args[1:]
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: parity-runner [-gen] <vectors.json>")
		os.Exit(2)
	}
	if gen {
		if err := writeVectors(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "parity-runner:", err)
			os.Exit(1)
		}
		return
	}
	if err := runVectors(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "parity-runner:", err)
		os.Exit(1)
	}
}

// writeVectors derives the hex requests from the typed fixtures.
func writeVectors(path string) error {
	vectors := make([]vector, 0, len(fixtures))
	for _, f := range fixtures {
		req := f.raw
		if req == "" {
			b, err := f.request.MarshalVT()
			if err != nil {
				return fmt.Errorf("%s: %w", f.name, err)
			}
			req = hex.EncodeToString(b)
		}
		vectors = append(vectors, vector{Name: f.name, Method: f.method, Request: req})
	}
	out, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// runVectors streams results in the shared line format.
func runVectors(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var vectors []vector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		return err
	}
	// Record events so they can be compared against the wasm side. On this tier
	// the sink is a Go function; on wasm it is the `devalbo:ilc/events` import
	// satisfied by verify/parity/events-sink.mjs. Both record identically —
	// that symmetry IS the check.
	var events []string
	platform.SetEventSink(func(topic string, payload []byte) {
		events = append(events, fmt.Sprintf("EVENT\t%s\t%s",
			topic, base64.StdEncoding.EncodeToString(payload)))
	})

	for _, v := range vectors {
		request, err := hex.DecodeString(v.Request)
		if err != nil {
			return fmt.Errorf("%s: bad request hex: %w", v.Name, err)
		}
		events = events[:0]
		r := engine.ExecuteMethod(v.Method, request)
		fmt.Printf("%t\t%s\t%s\n", r.Success, base64.StdEncoding.EncodeToString(r.Output), r.Err)
		// INTERLEAVED with the result, not a separate stream: this attributes
		// each event to the command that caused it, so a divergence in *which*
		// command emitted is caught, not just the set of events.
		for _, e := range events {
			fmt.Println(e)
		}
	}
	return nil
}
