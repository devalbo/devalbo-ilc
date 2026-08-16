package platform

import (
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// TestStatusRoundTrip pins the WIRE SHAPE: three bytes, in order.
//
// The slot names are provisional and expected to change. The shape is not — it
// is what every tier maps against, so changing it silently would leave hosts
// rendering the wrong slot with no error anywhere.
func TestStatusRoundTrip(t *testing.T) {
	var topic string
	var payload []byte
	SetEventSink(func(tp string, pl []byte) { topic, payload = tp, pl })
	defer SetEventSink(nil)

	SetStatus(1, 2, 3)

	if topic != StatusTopic {
		t.Fatalf("emitted on %q, want %q", topic, StatusTopic)
	}
	if len(payload) != StatusBytes {
		t.Fatalf("payload is %d bytes, want %d", len(payload), StatusBytes)
	}
	s1, s2, s3, ok := ParseStatus(payload)
	if !ok {
		t.Fatal("ParseStatus refused a payload SetStatus produced")
	}
	if s1 != 1 || s2 != 2 || s3 != 3 {
		t.Errorf("round trip got (%d,%d,%d), want (1,2,3)", s1, s2, s3)
	}
}

// TestParseStatusRefusesWrongShape — a host must not render garbage from a topic
// collision. Events are strings and any app may emit any topic it likes, so the
// three-byte shape is the only thing separating a status from someone else's
// payload that happens to share a name.
func TestParseStatusRefusesWrongShape(t *testing.T) {
	for _, payload := range [][]byte{nil, {}, {1}, {1, 2}, {1, 2, 3, 4}} {
		if _, _, _, ok := ParseStatus(payload); ok {
			t.Errorf("accepted a %d-byte payload", len(payload))
		}
	}
}

// TestStatusIsANoOpWithoutAHost — a capability's absence must be a no-op, never
// an error (Decision 33). An app calls this unconditionally and cannot tell
// whether anything is watching.
func TestStatusIsANoOpWithoutAHost(t *testing.T) {
	SetEventSink(nil)
	SetStatus(1, 2, 3) // must not panic
}

// TestCanShowTextFailsOpen — an unset advertisement must mean "print".
//
// The common case is an app that declared no world at all, and the asymmetry
// matters: printing where nobody looks costs a few discarded bytes, while
// staying silent where somebody is watching makes the app look broken.
func TestCanShowTextFailsOpen(t *testing.T) {
	defer resetEnvironment()
	resetEnvironment()

	// Before any manifest — the pre-boot window. Nobody has said, so print.
	if !CanShowText() {
		t.Error("silence should mean yes")
	}

	for _, outlet := range []ilcv1.TextOutlet{
		ilcv1.TextOutlet_TEXT_OUTLET_DISPLAY,
		ilcv1.TextOutlet_TEXT_OUTLET_UART,
		ilcv1.TextOutlet_TEXT_OUTLET_TERMINAL,
	} {
		resetEnvironment()
		if _, err := applyEnvironment(&ilcv1.Environment{
			Revision: 1,
			TextOut: &ilcv1.TextOut{
				Outlet: outlet,
			},
		}); err != nil {
			t.Fatal(err)
		}
		if !CanShowText() {
			t.Errorf("%v should mean yes", outlet)
		}
	}
}

// TestOutletComesFromTheManifest pins the single source.
//
// There used to be a second one — the `ILC_STDOUT` wasi key — and the two could
// disagree, because one is frozen at `_initialize` and the other is corrected
// mid-session. This asserts the key is now inert: a stale value in the
// environment must not reach an app, or the bug the manifest exists to fix comes
// straight back.
func TestOutletComesFromTheManifest(t *testing.T) {
	t.Setenv("ILC_STDOUT", "uart")
	defer resetEnvironment()
	resetEnvironment()

	if got := Env().GetTextOut().GetOutlet(); got != ilcv1.TextOutlet_TEXT_OUTLET_UNSPECIFIED {
		t.Fatalf("wasi keys must not be read: got %v", got)
	}

	if _, err := applyEnvironment(&ilcv1.Environment{
		Revision: 1,
		TextOut: &ilcv1.TextOut{
			Outlet: ilcv1.TextOutlet_TEXT_OUTLET_DISPLAY,
			Cols:   40, Rows: 12,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if got := Env().GetTextOut().GetOutlet(); got != ilcv1.TextOutlet_TEXT_OUTLET_DISPLAY {
		t.Fatalf("outlet: got %v want display", got)
	}
	if got, want := Env().GetTextOut().GetRows(), uint32(12); got != want {
		t.Fatalf("rows: got %d want %d", got, want)
	}
}

// TestOutletNoneSuppressesText: NONE is a claim, and the one value that turns
// printing off. An app trusts it to skip formatting work entirely.
func TestOutletNoneSuppressesText(t *testing.T) {
	defer resetEnvironment()
	resetEnvironment()
	if _, err := applyEnvironment(&ilcv1.Environment{
		Revision: 1,
		TextOut: &ilcv1.TextOut{
			Outlet: ilcv1.TextOutlet_TEXT_OUTLET_NONE,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if CanShowText() {
		t.Fatal("a declared NONE outlet must suppress printing")
	}
}

// TestOnEnvironmentChangeFiresOnlyOnChange guards the thing that makes the
// callback trustworthy.
//
// A watcher that fires on every re-send would make a host saying hello twice
// look like an allocation change, and an app would recompute its layout for
// nothing. A watcher that fires LATE — before the swap — would hand the app the
// manifest being replaced, which is worse than not firing: it acts confidently
// on stale data.
func TestOnEnvironmentChangeFiresOnlyOnChange(t *testing.T) {
	defer resetEnvironment()
	resetEnvironment()

	var calls int
	var sawRows uint32
	OnEnvironmentChange(func() {
		calls++
		sawRows = Env().GetTextOut().GetRows()
	})

	first := &ilcv1.Environment{Revision: 7, TextOut: &ilcv1.TextOut{Rows: 12}}
	if _, err := applyEnvironment(first); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("a new manifest must notify: calls=%d", calls)
	}
	if sawRows != 12 {
		t.Fatalf("watcher must see the NEW manifest, not the old: rows=%d", sawRows)
	}

	// Same revision: the host repeated itself, nothing changed.
	if _, err := applyEnvironment(first); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("a repeated revision must not notify: calls=%d", calls)
	}

	// The world takes four rows back.
	if _, err := applyEnvironment(&ilcv1.Environment{Revision: 8, TextOut: &ilcv1.TextOut{Rows: 8}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || sawRows != 8 {
		t.Fatalf("a changed allocation must notify with the new value: calls=%d rows=%d", calls, sawRows)
	}
}

// TestUnknownBudgetIsNotZeroSpace pins the reading of 0.
//
// Zero means "the host did not measure", which is the ordinary state of a
// terminal. An app that read it as "you have no room" would print nothing on the
// most capable tier there is — the failure is total and it is silent.
func TestUnknownBudgetIsNotZeroSpace(t *testing.T) {
	defer resetEnvironment()
	resetEnvironment()
	if got := Env().GetTextOut().GetCols(); got != 0 {
		t.Fatalf("unmeasured width reads as 0 (unknown): got %d", got)
	}
	if !CanShowText() {
		t.Fatal("unknown budget must not suppress printing; CanShowText fails open")
	}
}
