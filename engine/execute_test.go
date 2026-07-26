// Registry dispatch (Decision 29): every command reached by method_id, request
// and response crossing as flat proto bytes. Run: go test ./engine/
package engine_test

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/engine"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
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
	var resp dlcv1.VersionResponse
	if err := resp.UnmarshalVT(call(t, engine.MethodVersion, &dlcv1.VersionRequest{})); err != nil {
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

func TestNew(t *testing.T) {
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

// Unknown and not-yet-registered ids must fail cleanly, never panic — hosts and
// engines version independently. export-fs (4) is declared in commands.proto but
// waits on the filesystem capability seam.
func TestUnregisteredMethods(t *testing.T) {
	for _, method := range []uint32{engine.MethodExportFs, 9999} {
		if r := engine.ExecuteMethod(method, nil); r.Success {
			t.Errorf("method %d: expected failure", method)
		}
	}
}
