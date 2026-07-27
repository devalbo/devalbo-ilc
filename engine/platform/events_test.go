// The native half of the events seam. The wasip2 half cannot be unit-tested —
// it needs a component boundary — which is why the parity check (Phase 2) is the
// thing that keeps the two halves honest, not this file.
package platform

import "testing"

// Each test restores the sink: it is package state, and a leaked sink would make
// a later test receive another test's events.
func withSink(t *testing.T, fn EventSink) {
	t.Helper()
	prev := sink
	SetEventSink(fn)
	t.Cleanup(func() { SetEventSink(prev) })
}

func TestEmitDeliversVerbatim(t *testing.T) {
	type event struct {
		topic   string
		payload string
	}
	var got []event
	withSink(t, func(topic string, payload []byte) {
		got = append(got, event{topic, string(payload)})
	})

	Emit("notes.record-changed", []byte("payload-one"))
	Emit("ilc.data-changed", nil)

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(got), got)
	}
	if got[0] != (event{"notes.record-changed", "payload-one"}) {
		t.Errorf("first event mangled: %v", got[0])
	}
	// A payload-less event is legitimate: "something changed, re-read it".
	if got[1] != (event{"ilc.data-changed", ""}) {
		t.Errorf("second event mangled: %v", got[1])
	}
}

// The case that must never become an error. On a tier with no listener — a CLI,
// an embedded board — every app still calls Emit, and it has to be free.
func TestEmitWithNoSinkIsANoOp(t *testing.T) {
	withSink(t, nil)
	Emit("nobody.is.listening", []byte("x")) // must not panic
}

// An app must not be able to tell the difference between tiers by emitting a
// degenerate event, so an empty topic is dropped rather than delivered.
func TestEmitIgnoresEmptyTopic(t *testing.T) {
	delivered := false
	withSink(t, func(string, []byte) { delivered = true })

	Emit("", []byte("orphan"))

	if delivered {
		t.Error("an empty topic was delivered — nobody could ever subscribe to it")
	}
}

// A host's callback is not the engine's business. The event describes a write
// that has ALREADY happened, so a buggy listener must not turn a successful
// command into a failed one.
func TestPanickingSinkDoesNotEscape(t *testing.T) {
	withSink(t, func(string, []byte) { panic("a UI callback blew up") })

	Emit("notes.record-changed", []byte("x")) // must not panic

	// …and the engine keeps working afterwards.
	var recovered bool
	withSink(t, func(string, []byte) { recovered = true })
	Emit("notes.record-changed", []byte("x"))
	if !recovered {
		t.Error("delivery stopped working after a sink panicked")
	}
}
