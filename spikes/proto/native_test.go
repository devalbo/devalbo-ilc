// Native (standard Go) sanity for Spike 2 — regenerates/verifies goldens and
// asserts the spike package does not pull in encoding/json via go list -deps.
// Run: go test ./spikes/proto/
package main_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	spikev1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/spike/v1"
)

func fixture() *spikev1.SpikeMessage {
	return &spikev1.SpikeMessage{
		Name:  "spike",
		Count: 42,
		Ok:    true,
	}
}

func TestGoldens(t *testing.T) {
	msg := fixture()
	bin, err := msg.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	js, err := msg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	dir := "."
	wantHex := strings.TrimSpace(readFile(t, filepath.Join(dir, "golden.hex")))
	wantJSON := strings.TrimSpace(readFile(t, filepath.Join(dir, "golden.json")))
	gotHex := hex.EncodeToString(bin)
	gotJSON := strings.TrimSpace(string(js))

	if gotHex != wantHex {
		t.Fatalf("binary golden mismatch\n got: %s\nwant: %s", gotHex, wantHex)
	}
	if gotJSON != wantJSON {
		t.Fatalf("json golden mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}

	// Round-trip binary.
	var back spikev1.SpikeMessage
	if err := back.UnmarshalVT(bin); err != nil {
		t.Fatal(err)
	}
	if back.GetName() != "spike" || back.GetCount() != 42 || !back.GetOk() {
		t.Fatalf("unmarshal: %+v", &back)
	}
}

func TestSpikeMessageNoEncodingJSON(t *testing.T) {
	// go-lite's JSON codec uses json-iterator-lite, not stdlib encoding/json.
	// (The spike's WIT/cm imports may still list encoding/json — that's separate;
	// this check is about the protobuf surface the engine will speak.)
	cmd := exec.Command("go", "list", "-deps", "github.com/devalbo/devalbo-ilc/gen/go/devalbo/spike/v1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "encoding/json" {
			t.Fatal("spikev1 pulls in encoding/json — expected go-lite/json only")
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run make spike-proto-goldens first?)", path, err)
	}
	if bytes.Contains(b, []byte{0}) {
		t.Fatalf("%s looks binary; expected text", path)
	}
	return string(b)
}
