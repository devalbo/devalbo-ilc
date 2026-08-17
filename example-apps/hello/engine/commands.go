// Package engine holds hello's business logic — ALL of it, and nothing
// else. This package is the one portable artifact: it is linked natively by the
// CLI host and compiles to wasm for the browser, so anything platform-specific
// here would break a tier.
//
// Rules that keep it portable:
//   - Never call a platform API directly; use the injected capabilities.
//   - Stay TinyGo-safe and reflection-free — no encoding/json, no text/template.
//   - Touch the filesystem only through platform.SafeJoin / platform.WriteTree,
//     so path containment is inherited rather than re-implemented.
package engine

import (
	"fmt"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	"github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/dlcconfig"
	hellov1 "github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/hello/v1"
)

// No version string here. It lives in dlc.toml and reaches this package as
// GENERATED code — the same rule as method ids: one place to edit, and no
// second copy that can disagree.

// Method ids are GENERATED from commands.proto — you never write one down.
// Yours live at 1000+; 1–999 belong to the platform.
const MethodGreet = hellov1.MethodGreet

// init wires the app up. This is the whole pattern:
//
//	inherit the platform's verbs → say who you are → register your own.
//
// The generated Handlers map carries the ids, so adding a command means adding
// an rpc to commands.proto and a handler here — never an id in two places.
func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())

	platform.RegisterRaw(hellov1.AppServiceHandlers(handleGreet))

	// WHAT THE COMMANDS TAKE, so a host that cannot compile this app's
	// schema can still collect input for it — a badge running payloads it
	// was never built for, or a browser that would otherwise hand-write an
	// <input> per field. Without this the description is stripped from the
	// wasm as dead code, because only the native CLI referenced it.
	platform.RegisterCommandSpec(hellov1.AppServiceCLI)
}

// handleGreet is a command: typed request in, typed response out. Failure is an
// `error` — it rides the command-result envelope, which is why no response
// message has an error field.
func handleGreet(req *hellov1.GreetRequest) (*hellov1.GreetResponse, error) {
	name := req.Name
	if name == "" {
		name = "world"
	}
	// ASCII ONLY IN APP-VISIBLE TEXT.
	//
	// This said `—` and the badge drew `?`: its font is ASCII, so
	// embedded-graphics substitutes for any glyph it lacks. The bytes crossed
	// every boundary intact — Component Model `string` is UTF-8 and
	// `wasi:cli/stdout` is opaque `list<u8>` — and then died at the last step,
	// where characters become pixels.
	//
	// An app cannot know which tier it is on, so it should not spend characters
	// the poorest one cannot draw. A hyphen renders identically everywhere; an
	// em dash renders on two tiers out of three and degrades to punctuation that
	// reads like an error.
	text := "hello, " + name + " - from hello"

	// ASK THE WORLD WHERE TEXT GOES, then defer to it.
	//
	// A typed response is protobuf, and decoding it needs THIS APP'S SCHEMA —
	// which the CLI has compiled in and a badge running apps it was never built
	// for does not. So on a constrained tier the return value is invisible and
	// stdout is the only channel carrying anything a person can read.
	//
	// Learned on hardware: this app ran correctly on a badge and showed a blank
	// screen, because it returned a response and printed nothing.
	//
	// `CanShowText` reads what the tier advertised. It FAILS OPEN, so an
	// unadorned host still prints — and a tier that says "none" saves the app
	// the heap of formatting output nobody will ever see.
	if platform.CanShowText() {
		fmt.Println(text)
	}

	// AND ALWAYS THE STATUS BYTES. This is the channel that survives when text
	// does not: a tier with one LED can render it, and a tier with nothing
	// discards it. Absence is a no-op, never an error.
	platform.SetStatus(1, 0, 0)

	return &hellov1.GreetResponse{Text: text}, nil
}
