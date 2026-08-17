package engine_test

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	_ "github.com/you/countdown/engine" // registers the commands
	countdownv1 "github.com/you/countdown/gen/go/countdown/v1"
)

// Commands are tested through the registry, the same path every host uses —
// so a passing test means the wiring works, not just the function.
func TestGreet(t *testing.T) {
	request, err := (&countdownv1.GreetRequest{Name: "ILC"}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(countdownv1.MethodGreet, request)
	if !r.Success {
		t.Fatalf("greet failed: %s", r.Err)
	}
	var resp countdownv1.GreetResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		t.Fatal(err)
	}
	if resp.Text == "" {
		t.Error("empty greeting")
	}
}
