package platform

// The command spec — what each command TAKES, as data a host can act on
// (docs/WORLD-INPUT-PLAN.md D2).
//
// # The gap this fills
//
// `GetCommandSurface` reports which methods exist. Nothing reported what they
// ACCEPT, and the description already existed: `protoc-gen-dlc-registry` emits a
// `clispec.Command` per rpc, with a flag per request field carrying the number,
// kind, source, help and enum values.
//
// It reached exactly one consumer. `AppServiceCLI` is referenced only by the
// native host, so TinyGo's dead-code elimination — correctly — strips it from
// the wasm. `"who to greet"` appears ZERO times in hello's component: the
// description of an app's inputs has never been present on the tier that most
// needs it.
//
// # Who needs it, since the CLI does not
//
// The CLI compiles the same data in, and that is the point of comparison rather
// than a reason to skip it: a test asserts the queried spec and the compiled one
// agree, so the two cannot drift.
//
// It is for hosts that CANNOT compile an app's schema in:
//
//   - a BADGE running payloads it was never built for. It cannot know that hello
//     wants a name; it can know method 10000 takes a string in field 1 called
//     `name`, and that is enough to render a prompt and encode the answer.
//   - a BROWSER, which today hand-writes `<input id="name">` in HTML and has no
//     way to notice when the field is renamed.
//
// # Why the request can be built without the schema, and the response cannot
//
// Encoding a string into field 1 is `tag(1, LEN) + varint(len) + bytes`. Field
// number and wire kind are sufficient, which is what lets a generic loader build
// a valid request for an app it has never seen.
//
// Rendering a RESPONSE is a different problem and is deliberately unsolved: field
// numbers alone do not say what a value MEANS. That asymmetry is why the badge
// prints `stdout` rather than the typed result, and it is unchanged.

import (
	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// commandSpec is what the app registered, or nil if it registered nothing.
var commandSpec []clispec.Command

// RegisterCommandSpec publishes what this app's commands accept.
//
// Call it from the app's init, beside RegisterRaw:
//
//	platform.RegisterAll()
//	platform.RegisterRaw(appv1.AppServiceHandlers(handleGreet))
//	platform.RegisterCommandSpec(appv1.AppServiceCLI)
//
// EXPLICIT RATHER THAN AUTOMATIC, and not for want of trying. The obvious move
// is for the generated registry to register itself in an `init()`, which would
// mean generated code importing `platform` — and `platform` imports the
// generated `ilcv1`, so that is a cycle. `clispec` is a leaf package for exactly
// this reason. One line in the app is the price of that layering, and it is the
// same shape `RegisterRaw` already has.
//
// **This is also what keeps the spec out of the wasm until asked for.** An app
// that never calls this keeps the DCE it has today; one that does pays for its
// flag names and help strings in the guest, which on a badge is a real cost.
func RegisterCommandSpec(commands []clispec.Command) {
	commandSpec = commands
}

// resetCommandSpec drops the registration. Tests only.
func resetCommandSpec() { commandSpec = nil }

// handleGetCommandSpec answers what a command accepts.
//
// A zero `method_id` means EVERY command, because a browser building a whole
// surface should not pay a round trip per command while a badge asking about one
// should not receive the rest.
func handleGetCommandSpec(req *ilcv1.GetCommandSpecRequest) (*ilcv1.GetCommandSpecResponse, error) {
	out := make([]*ilcv1.SpecCommand, 0, len(commandSpec))
	for _, command := range commandSpec {
		if req.GetMethodId() != 0 && command.Method != req.GetMethodId() {
			continue
		}
		out = append(out, specCommand(command))
	}
	// An app that registered nothing, or a method that does not exist, gets an
	// EMPTY list rather than an error. A host asking "what does this take" and
	// being told "nothing" can proceed; being told "failed" cannot distinguish
	// an app with no inputs from an app that is broken.
	return &ilcv1.GetCommandSpecResponse{Commands: out}, nil
}

func specCommand(c clispec.Command) *ilcv1.SpecCommand {
	flags := make([]*ilcv1.SpecFlag, 0, len(c.Flags))
	for _, f := range c.Flags {
		flags = append(flags, &ilcv1.SpecFlag{
			Name:         f.Name,
			Field:        f.Field,
			Kind:         specKind(f.Kind),
			Source:       specSource(f.Source),
			Help:         f.Help,
			Required:     f.Required,
			DefaultValue: f.Default,
			Repeated:     f.Repeated,
			Positional:   f.Positional,
			EnumValues:   f.EnumValues,
			EnumNumbers:  f.EnumNumbers,
			Short:        f.Short,
		})
	}
	results := make([]*ilcv1.SpecResult, 0, len(c.Results))
	for _, r := range c.Results {
		results = append(results, &ilcv1.SpecResult{
			Name:        r.Name,
			Field:       r.Field,
			Kind:        specKind(r.Kind),
			Repeated:    r.Repeated,
			Help:        r.Help,
			EnumValues:  r.EnumValues,
			EnumNumbers: r.EnumNumbers,
		})
	}
	return &ilcv1.SpecCommand{
		Name:                c.Name,
		Method:              c.Method,
		Summary:             c.Summary,
		Flags:               flags,
		Request:             c.Request,
		Local:               c.Local,
		Unsupported:         c.Unsupported,
		Response:            c.Response,
		Results:             results,
		ResponseUnsupported: c.ResponseUnsupported,
	}
}

// specKind maps the in-process enum to the wire one.
//
// WRITTEN OUT rather than cast, even though the numbers currently line up.
// `clispec.Kind` is a Go iota that anyone may reorder while reading it as a
// local detail; the proto numbers are a wire contract that cannot move. A cast
// would silently translate one into the other, and the failure would be a host
// encoding a string as an int64 — which produces a request that decodes.
func specKind(k clispec.Kind) ilcv1.SpecKind {
	switch k {
	case clispec.KindString:
		return ilcv1.SpecKind_SPEC_KIND_STRING
	case clispec.KindBool:
		return ilcv1.SpecKind_SPEC_KIND_BOOL
	case clispec.KindInt32:
		return ilcv1.SpecKind_SPEC_KIND_INT32
	case clispec.KindInt64:
		return ilcv1.SpecKind_SPEC_KIND_INT64
	case clispec.KindUint32:
		return ilcv1.SpecKind_SPEC_KIND_UINT32
	case clispec.KindUint64:
		return ilcv1.SpecKind_SPEC_KIND_UINT64
	case clispec.KindEnum:
		return ilcv1.SpecKind_SPEC_KIND_ENUM
	case clispec.KindBytes:
		return ilcv1.SpecKind_SPEC_KIND_BYTES
	}
	// KindUnsupported and anything added later. UNSPECIFIED is the safe answer:
	// a host that cannot render a kind skips the field and the app takes its
	// default, which is a no-op rather than an error (Decision 33).
	return ilcv1.SpecKind_SPEC_KIND_UNSPECIFIED
}

// specSource maps where a value comes from. See specKind on why not a cast.
func specSource(s clispec.Source) ilcv1.SpecSource {
	switch s {
	case clispec.SourceLiteral:
		return ilcv1.SpecSource_SPEC_SOURCE_LITERAL
	case clispec.SourceFile:
		return ilcv1.SpecSource_SPEC_SOURCE_FILE
	case clispec.SourceStdin:
		return ilcv1.SpecSource_SPEC_SOURCE_STDIN
	}
	return ilcv1.SpecSource_SPEC_SOURCE_UNSPECIFIED
}
