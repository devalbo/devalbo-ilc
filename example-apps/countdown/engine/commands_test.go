package engine_test

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
	_ "github.com/you/countdown/engine" // registers the commands
	countdownv1 "github.com/you/countdown/gen/go/countdown/v1"
)

// Commands are tested through the registry, the same path every host uses —
// so a passing test means the wiring works, not just the function.
//
// THIS FILE TESTED `greet` UNTIL NOW, a command countdown has never had: it was
// scaffolded from hello and the test came with it. It compiled for exactly as
// long as the generated `GreetRequest` lingered and then failed the build, which
// is the good version of that mistake — a stale test that still PASSES is the
// one that costs something.
func TestCount(t *testing.T) {
	request, err := (&countdownv1.CountRequest{From: 1}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(countdownv1.MethodCount, request)
	if !r.Success {
		t.Fatalf("count failed: %s", r.Err)
	}
	var resp countdownv1.CountResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		t.Fatal(err)
	}
	if resp.Text == "" {
		t.Error("empty result")
	}
}

// Every style is accepted, including the one nobody set.
//
// The unset case matters most: proto3 has no presence, so a request that omits
// `style` and one that sets `STYLE_UNSPECIFIED` are the same bytes — and both
// must behave as the app did before the field existed (Decision 33).
func TestEveryStyleRuns(t *testing.T) {
	for _, style := range []countdownv1.Style{
		countdownv1.Style_STYLE_UNSPECIFIED,
		countdownv1.Style_STYLE_PLAIN,
		countdownv1.Style_STYLE_ROCKET,
		countdownv1.Style_STYLE_WORDS,
	} {
		request, err := (&countdownv1.CountRequest{From: 1, Style: style}).MarshalVT()
		if err != nil {
			t.Fatal(err)
		}
		r := platform.Execute(countdownv1.MethodCount, request)
		if !r.Success {
			t.Errorf("style %v: %s", style, r.Err)
		}
	}
}

// The spec a host collects from must describe BOTH fields, with the choices.
//
// THROUGH `Execute`, not through an accessor: this is the exact path the badge
// takes — method 5, over the wire, decoded from bytes — so a pass means the
// badge would see what this sees. A test reaching into the registry directly
// would prove the registry and not the contract.
func TestSpecDescribesBothFields(t *testing.T) {
	ask, err := (&ilcv1.GetCommandSpecRequest{MethodId: countdownv1.MethodCount}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(platform.MethodGetCommandSpec, ask)
	if !r.Success {
		t.Fatalf("GetCommandSpec failed: %s", r.Err)
	}
	var spec ilcv1.GetCommandSpecResponse
	if err := spec.UnmarshalVT(r.Output); err != nil {
		t.Fatal(err)
	}
	if len(spec.Commands) != 1 {
		t.Fatalf("expected one command, got %d", len(spec.Commands))
	}
	flags := spec.Commands[0].Flags
	if len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags))
	}

	var style *ilcv1.SpecFlag
	for _, f := range flags {
		if f.GetName() == "style" {
			style = f
		}
	}
	if style == nil {
		t.Fatal("no style flag in the spec")
	}
	if len(style.GetEnumValues()) != 4 {
		t.Errorf("expected 4 choices, got %v", style.GetEnumValues())
	}
	// AN ORDINAL IS NOT A VALUE. The numbers travel beside the names so a host
	// encodes what the app declared rather than a position in a list — see
	// SpecFlag.enum_numbers.
	if len(style.GetEnumNumbers()) != len(style.GetEnumValues()) {
		t.Fatalf("%d names but %d numbers",
			len(style.GetEnumValues()), len(style.GetEnumNumbers()))
	}
	for i, name := range style.GetEnumValues() {
		if got := style.GetEnumNumbers()[i]; got != int32(i) {
			t.Errorf("%s: number %d, position %d", name, got, i)
		}
	}

	// THE ORDER TO ASK IN comes from the app, and the badge sorts by it.
	for _, f := range flags {
		if f.GetPositional() == 0 {
			t.Errorf("%s has no position, so a world must guess when to ask", f.GetName())
		}
	}
}
