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
	"time"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	"github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/dlcconfig"
	hellov1 "github.com/devalbo/devalbo-ilc/example-apps/hello/gen/go/hello/v1"
)

// No version string here. It lives in dlc.toml and reaches this package as
// GENERATED code — the same rule as method ids: one place to edit, and no
// second copy that can disagree.

// Method ids are GENERATED from commands.proto — you never write one down.
// Yours live at 1000+; 1–999 belong to the platform.
const (
	MethodGreet = hellov1.MethodGreet
	MethodCount = hellov1.MethodCount
	MethodMath  = hellov1.MethodMath
	MethodLight = hellov1.MethodLight
)

// How long a tick lasts. THE APP'S CHOICE, which is the point of `count`.
const tick = time.Second

// The largest count this will run. Somebody typing 999999 on a badge should get
// a bounded run rather than a board that looks hung for a quarter of an hour,
// and there is no way to interrupt one yet.
const maxFrom = 60

// init wires the app up. This is the whole pattern:
//
//	inherit the platform's verbs → say who you are → register your own.
//
// The generated Handlers map carries the ids, so adding a command means adding
// an rpc to commands.proto and a handler here — never an id in two places.
func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())

	platform.RegisterRaw(hellov1.AppServiceHandlers(handleGreet, handleCount, handleMath, handleLight))

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

// ---------------------------------------------------------------------------
// count — output DURING a command
// ---------------------------------------------------------------------------

// handleCount emits a tick a second, then answers.
//
// WHAT MAKES IT DIFFERENT FROM `greet`: everything interesting happens before it
// returns. `fmt.Println` reaches `wasi:cli/stdout.write`, which is an IMPORT —
// host code running while this goroutine is suspended inside the call — so a
// world sees each line as it is written rather than all at once at the end.
//
// That is also what makes the sleep meaningful. On a tier whose clock is a stub
// this finishes instantly, which is wrong but not broken, and is exactly what a
// badge did before its TIMER0 was wired to `monotonic-clock`.
func handleCount(req *hellov1.CountRequest) (*hellov1.CountResponse, error) {
	from := int(req.From)
	if from < 1 {
		// ZERO MEANS UNSET, not zero. Proto3 scalars have no presence, so an
		// omitted field and an explicit 0 are the same bytes — which is why the
		// default is declared in the schema as well, for hosts that read it.
		from = 5
	}
	if from > maxFrom {
		from = maxFrom
	}

	counted := 0
	for remaining := from; remaining > 0; remaining-- {
		if platform.CanShowText() {
			fmt.Println(phrase(req.Style, remaining))
		}
		// STATUS ON EVERY TICK, so a world with only a light still sees motion.
		platform.SetStatus(1, byte(remaining), 0)
		// AND IN WORDS, for a world that can show them. The dispatcher already
		// publishes the command's name by default, so an app that says nothing
		// still reports something true; this REFINES that, which is the only
		// reason for an app to mention activity at all.
		var label [16]byte
		platform.SetActivity(itoa(remaining, &label))
		counted++
		time.Sleep(tick)
	}

	text := "liftoff"
	if platform.CanShowText() {
		fmt.Println(text)
	}
	platform.SetStatus(1, 0, 0)
	return &hellov1.CountResponse{Text: text, Counted: int32(counted)}, nil
}

// phrase renders one tick in the style the caller asked for.
//
// THE APP OWNS ITS OWN WORDS. A world offering "plain or rocket" as a built-in
// would be guessing at a vocabulary; here the choices travel with the command
// spec, a host shows the names the app declared, and this turns the chosen one
// into text.
func phrase(style hellov1.Style, remaining int) string {
	switch style {
	case hellov1.Style_STYLE_ROCKET:
		return fmt.Sprintf("T-minus %d", remaining)
	case hellov1.Style_STYLE_WORDS:
		words := []string{"zero", "one", "two", "three", "four", "five",
			"six", "seven", "eight", "nine", "ten"}
		if remaining < len(words) {
			return words[remaining]
		}
		return fmt.Sprintf("%d", remaining)
	default:
		// STYLE_UNSPECIFIED lands here too: an absent field gets the behaviour
		// the app had before the field existed (Decision 33).
		return fmt.Sprintf("%d...", remaining)
	}
}

// itoa without fmt: this runs once per tick, and `fmt` pulls formatting
// machinery into a component that has to fit on a badge. Writes into a
// caller-owned buffer so nothing allocates on a path an app runs in a loop.
func itoa(value int, buffer *[16]byte) string {
	if value == 0 {
		return "0 left"
	}
	digits := len(buffer)
	for value > 0 && digits > 0 {
		digits--
		buffer[digits] = byte('0' + value%10)
		value /= 10
	}
	copy(buffer[len(buffer)-5:], " left")
	return string(buffer[digits : len(buffer)-5+5])
}

// ---------------------------------------------------------------------------
// math — several inputs, a structured answer
// ---------------------------------------------------------------------------

// handleMath computes one expression.
//
// # Why divide-by-zero is not an error
//
// Returning `error` would put the failure in the command-result envelope, where
// it reads as "the app broke". It did not: it was asked something with no answer
// and said so. So the failure is a FIELD — `problem` — which means a host can
// act on it, a test can assert on it, and a world with nothing but a light can
// render it as amber.
//
// That last one is the real argument. An error string reaches a terminal and
// nowhere else; a declared enum reaches every surface, including the ones that
// cannot show text at all.
func handleMath(req *hellov1.MathRequest) (*hellov1.MathResponse, error) {
	left, right := req.Left, req.Right

	if req.Op == hellov1.Operator_OPERATOR_DIVIDE && right == 0 {
		// AMBER, NOT RED. Red is for a world reporting that something is broken;
		// this is a warning about what was asked, and the badge across the room
		// should say so differently.
		platform.SetStatus(2, 0, 0)
		if platform.CanShowText() {
			fmt.Println("cannot divide by zero")
		}
		return &hellov1.MathResponse{
			Expression: expression(left, req.Op, right),
			Problem:    hellov1.Problem_PROBLEM_DIVIDE_BY_ZERO,
		}, nil
	}

	var result int64
	switch req.Op {
	case hellov1.Operator_OPERATOR_SUBTRACT:
		result = left - right
	case hellov1.Operator_OPERATOR_MULTIPLY:
		result = left * right
	case hellov1.Operator_OPERATOR_DIVIDE:
		result = left / right
	default:
		// OPERATOR_UNSPECIFIED adds, which is what an omitted field means here.
		result = left + right
	}

	platform.SetStatus(1, 0, 0)
	text := expression(left, req.Op, right) + " = " + fmt.Sprint(result)
	if platform.CanShowText() {
		fmt.Println(text)
	}
	return &hellov1.MathResponse{Result: result, Expression: text}, nil
}

// expression spells an operator the way this app spells it.
//
// A HOST COULD BUILD THIS from the request it sent, and then every host would
// build it slightly differently. The app knows whether it writes `x` or `*`.
func expression(left int64, op hellov1.Operator, right int64) string {
	symbol := "+"
	switch op {
	case hellov1.Operator_OPERATOR_SUBTRACT:
		symbol = "-"
	case hellov1.Operator_OPERATOR_MULTIPLY:
		symbol = "x"
	case hellov1.Operator_OPERATOR_DIVIDE:
		symbol = "/"
	}
	return fmt.Sprint(left) + " " + symbol + " " + fmt.Sprint(right)
}

// ---------------------------------------------------------------------------
// light — changing the device
// ---------------------------------------------------------------------------

// handleLight sets the world's status colour and says whether anyone saw it.
//
// THE REPLY IS NOT THE POINT. What matters happens on the device, and on a tier
// with no light nothing happens at all — which is not an error. The app asks,
// the world does what it can, and `shown` reports which of those it was so a
// caller is never left guessing whether the command worked.
func handleLight(req *hellov1.LightRequest) (*hellov1.LightResponse, error) {
	// SLOT 1 IS THE LONG-LIVED VALUE — "how is it going" — as opposed to slot 2,
	// which apps twiddle to show motion. A colour somebody set deliberately
	// belongs in the one that persists.
	var status byte
	switch req.Colour {
	case hellov1.Colour_COLOUR_AMBER:
		status = 2
	case hellov1.Colour_COLOUR_RED:
		status = 3
	case hellov1.Colour_COLOUR_OFF:
		status = 0
	default:
		status = 1
	}
	platform.SetStatus(status, 0, 0)

	// WHETHER IT REACHED ANYTHING. There is no capability query for a light, so
	// this reports the nearest true thing the platform can answer — a world that
	// renders text renders status too, and one that renders neither is the case
	// this field exists to make visible.
	shown := platform.CanShowText()
	return &hellov1.LightResponse{Shown: shown}, nil
}
