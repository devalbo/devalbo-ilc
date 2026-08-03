package engine_test

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	_ "github.com/devalbo/devalbo-ilc/example-apps/hello/engine" // registers the commands
	hellov1 "github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/hello/v1"
)

// Commands are tested through the registry, the same path every host uses —
// so a passing test means the wiring works, not just the function.
func TestGreet(t *testing.T) {
	request, err := (&hellov1.GreetRequest{Name: "ILC"}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(hellov1.MethodGreet, request)
	if !r.Success {
		t.Fatalf("greet failed: %s", r.Err)
	}
	var resp hellov1.GreetResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		t.Fatal(err)
	}
	if resp.Text == "" {
		t.Error("empty greeting")
	}
}
