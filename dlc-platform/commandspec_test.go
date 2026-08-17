package platform

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// sample is a command with every field populated, because the failure this
// guards against is a field silently NOT crossing.
func sample() []clispec.Command {
	return []clispec.Command{{
		Name:    "greet",
		Method:  10000,
		Request: "GreetRequest",
		Summary: "Say hello to someone",
		Flags: []clispec.Flag{{
			Name:       "name",
			Short:      "n",
			Field:      1,
			Kind:       clispec.KindString,
			Source:     clispec.SourceStdin,
			Repeated:   false,
			Positional: 1,
			Help:       "who to greet",
			Required:   true,
			Default:    "world",
			EnumValues: []string{"alice", "bob"},
		}},
		Local:       false,
		Unsupported: []string{"nested"},
	}}
}

func query(t *testing.T, methodID uint32) *ilcv1.GetCommandSpecResponse {
	t.Helper()
	res, err := handleGetCommandSpec(&ilcv1.GetCommandSpecRequest{MethodId: methodID})
	if err != nil {
		t.Fatalf("GetCommandSpec: %v", err)
	}
	return res
}

// TestEveryFieldCrossesTheBoundary is the whole point of the spec method.
//
// A host that cannot compile the app's schema acts on THIS data and nothing
// else. A field that is dropped in translation does not fail — it produces a
// host that renders a prompt without help text, or encodes into field 0, or
// misses that a value is required. All of those look like working code.
//
// So this asserts every field, not a representative sample.
func TestEveryFieldCrossesTheBoundary(t *testing.T) {
	defer resetCommandSpec()
	RegisterCommandSpec(sample())

	res := query(t, 0)
	if len(res.GetCommands()) != 1 {
		t.Fatalf("commands: got %d want 1", len(res.GetCommands()))
	}
	got := res.GetCommands()[0]

	if got.GetName() != "greet" {
		t.Errorf("name: %q", got.GetName())
	}
	if got.GetMethod() != 10000 {
		t.Errorf("method: %d", got.GetMethod())
	}
	if got.GetSummary() != "Say hello to someone" {
		t.Errorf("summary: %q", got.GetSummary())
	}
	if got.GetRequest() != "GreetRequest" {
		t.Errorf("request: %q", got.GetRequest())
	}
	if got.GetLocal() {
		t.Error("local should be false")
	}
	if len(got.GetUnsupported()) != 1 || got.GetUnsupported()[0] != "nested" {
		t.Errorf("unsupported: %v", got.GetUnsupported())
	}

	if len(got.GetFlags()) != 1 {
		t.Fatalf("flags: got %d want 1", len(got.GetFlags()))
	}
	f := got.GetFlags()[0]
	if f.GetName() != "name" {
		t.Errorf("flag name: %q", f.GetName())
	}
	if f.GetShort() != "n" {
		t.Errorf("short: %q", f.GetShort())
	}
	// THE FIELD NUMBER IS THE WIRE. A host encodes by number, so a wrong one
	// produces a request that decodes into the wrong field — valid protobuf
	// carrying the wrong meaning, which no error path will catch.
	if f.GetField() != 1 {
		t.Errorf("field number: %d", f.GetField())
	}
	if f.GetKind() != ilcv1.SpecKind_SPEC_KIND_STRING {
		t.Errorf("kind: %v", f.GetKind())
	}
	if f.GetSource() != ilcv1.SpecSource_SPEC_SOURCE_STDIN {
		t.Errorf("source: %v", f.GetSource())
	}
	if f.GetHelp() != "who to greet" {
		t.Errorf("help: %q", f.GetHelp())
	}
	if !f.GetRequired() {
		t.Error("required should be true")
	}
	if f.GetDefaultValue() != "world" {
		t.Errorf("default: %q", f.GetDefaultValue())
	}
	if f.GetPositional() != 1 {
		t.Errorf("positional: %d", f.GetPositional())
	}
	if len(f.GetEnumValues()) != 2 {
		t.Errorf("enum values: %v", f.GetEnumValues())
	}
}

// TestKindsAndSourcesMapExhaustively guards the hand-written switches.
//
// `clispec.Kind` is a Go iota that anyone may reorder while reading it as a
// local detail; the proto numbers are a wire contract that cannot move. The
// switches exist so the two are not the same number by luck — and a switch is
// only as good as a test that walks every arm.
//
// The failure mode if one is wrong: a string advertised as an int64. A host
// encodes it as a varint, the guest decodes a garbage value, and nothing errors.
func TestKindsAndSourcesMapExhaustively(t *testing.T) {
	kinds := map[clispec.Kind]ilcv1.SpecKind{
		clispec.KindUnsupported: ilcv1.SpecKind_SPEC_KIND_UNSPECIFIED,
		clispec.KindString:      ilcv1.SpecKind_SPEC_KIND_STRING,
		clispec.KindBool:        ilcv1.SpecKind_SPEC_KIND_BOOL,
		clispec.KindInt32:       ilcv1.SpecKind_SPEC_KIND_INT32,
		clispec.KindInt64:       ilcv1.SpecKind_SPEC_KIND_INT64,
		clispec.KindUint32:      ilcv1.SpecKind_SPEC_KIND_UINT32,
		clispec.KindUint64:      ilcv1.SpecKind_SPEC_KIND_UINT64,
		clispec.KindEnum:        ilcv1.SpecKind_SPEC_KIND_ENUM,
		clispec.KindBytes:       ilcv1.SpecKind_SPEC_KIND_BYTES,
	}
	for in, want := range kinds {
		if got := specKind(in); got != want {
			t.Errorf("kind %v: got %v want %v", in, got, want)
		}
	}

	sources := map[clispec.Source]ilcv1.SpecSource{
		clispec.SourceLiteral: ilcv1.SpecSource_SPEC_SOURCE_LITERAL,
		clispec.SourceFile:    ilcv1.SpecSource_SPEC_SOURCE_FILE,
		clispec.SourceStdin:   ilcv1.SpecSource_SPEC_SOURCE_STDIN,
	}
	for in, want := range sources {
		if got := specSource(in); got != want {
			t.Errorf("source %v: got %v want %v", in, got, want)
		}
	}
}

// TestFilterByMethod: a badge asks about one command and must not receive the
// rest — it has one screen and no reason to page through a surface.
func TestFilterByMethod(t *testing.T) {
	defer resetCommandSpec()
	RegisterCommandSpec([]clispec.Command{
		{Name: "greet", Method: 10000},
		{Name: "wave", Method: 10001},
	})

	res := query(t, 10001)
	if len(res.GetCommands()) != 1 || res.GetCommands()[0].GetName() != "wave" {
		t.Fatalf("filtering by method: got %v", res.GetCommands())
	}
	if all := query(t, 0); len(all.GetCommands()) != 2 {
		t.Fatalf("zero must mean every command: got %d", len(all.GetCommands()))
	}
}

// TestNoSpecIsNotAnError: an app that registered nothing must answer "nothing",
// not fail.
//
// A host asking "what does this take" and being told "empty" can proceed and
// execute with defaults (Decision 33). Being told "failed" cannot tell an app
// with no inputs from an app that is broken, and would strand a loader that has
// no other way to find out.
func TestNoSpecIsNotAnError(t *testing.T) {
	defer resetCommandSpec()
	resetCommandSpec()

	res := query(t, 0)
	if len(res.GetCommands()) != 0 {
		t.Fatalf("expected an empty list, got %v", res.GetCommands())
	}
	if res := query(t, 99999); len(res.GetCommands()) != 0 {
		t.Fatal("an unknown method must answer empty, not error")
	}
}
