// Package engine holds countdown's business logic — ALL of it.
//
// # What this app is for
//
// It is the smallest thing that proves the world does not own time.
//
// The badge's `wasi:clocks/monotonic-clock` used to count CALLS, and
// `subscribe_duration` threw its argument away — so `time.Sleep` returned
// instantly and nothing could unfold over time. The alternative was a world that
// called `tick` repeatedly, which works but puts the INTERVAL in the world: "how
// often" is a policy, and a policy is logic, and logic does not belong in a
// shell.
//
// So this app sleeps. It decides where to start, how long a tick is, and when it
// is done. The world provides a clock and a screen and knows none of that.
package engine

import (
	"fmt"
	"time"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	countdownv1 "github.com/you/countdown/gen/go/countdown/v1"
	"github.com/you/countdown/gen/go/dlcconfig"
)

const MethodCount = countdownv1.MethodCount

// How long a tick lasts. THE APP'S CHOICE, which is the point of the app.
const tick = time.Second

// Where to start when nobody says. Small enough to watch to the end.
const defaultFrom = 5

// The largest count this will run. A person typing `999999` on a badge should
// get a bounded run rather than a board that looks hung for a quarter of an
// hour — and there is no way to interrupt it yet (SESSION-AND-SURFACE-PLAN D5).
const maxFrom = 60

func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())
	platform.RegisterRaw(countdownv1.AppServiceHandlers(handleCount))
	platform.RegisterCommandSpec(countdownv1.AppServiceCLI)
}

// itoa without fmt: this runs once per tick and `fmt` pulls formatting machinery
// into a component that has to fit on a badge. Writes into a caller-owned buffer
// so nothing allocates on a path an app may run in a loop.
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
	return string(buffer[digits:]) + " left"
}

func handleCount(req *countdownv1.CountRequest) (*countdownv1.CountResponse, error) {
	// NO PARSING. `from` is an int32 in the schema, so every tier delivers a
	// number and this app never sees text it has to interpret — the CLI parses
	// the flag, the browser parses the field, and the badge's spinner produces
	// one directly.
	//
	// It was a string until the badge grew a number widget (WORLD-INPUT-PLAN
	// D3d), and the `strconv.Atoi` plus its error path went with it.
	from := int(req.From)
	if from < 1 {
		// ZERO MEANS UNSET, not zero. Proto3 scalars have no presence, so an
		// omitted field and an explicit 0 are the same bytes — which is exactly
		// why the default lives in the schema as well, for hosts that read it.
		from = defaultFrom
	}
	if from > maxFrom {
		from = maxFrom
	}

	// EACH TICK IS PRINTED AS IT HAPPENS, not collected and returned at the end.
	//
	// `fmt.Println` reaches `wasi:cli/stdout.write`, which is an IMPORT — host
	// code running while this goroutine is suspended inside the call. So a world
	// that echoes those bytes sees them at the moment they are written, a second
	// apart, rather than all at once when this returns.
	//
	// That is the whole demonstration: output during a command, paced by the app.
	for remaining := from; remaining > 0; remaining-- {
		if platform.CanShowText() {
			fmt.Printf("%d...\n", remaining)
		}
		// STATUS ON EVERY TICK, so a world with only an LED still sees motion.
		// Slot 2 is the short-term value — a tier can render change as a blink,
		// and an app that twiddles it produces visible activity without knowing
		// blinking exists.
		platform.SetStatus(1, byte(remaining), 0)

		// AND IN WORDS, for a world that can show them. The dispatcher already
		// publishes "count" — the command's name — so an app that says nothing
		// still reports something true. This REFINES that, which is the only
		// reason for an app to mention activity at all.
		//
		// It arrives while this command is still running: `emit` is an import,
		// so the world sees it as it is set rather than when `count` returns.
		var label [16]byte
		platform.SetActivity(itoa(remaining, &label))

		// THE SLEEP THIS APP EXISTS TO TEST. On a tier whose clock is a stub this
		// returns immediately and the countdown finishes instantly — which is
		// wrong but not broken, and is exactly what the badge did before TIMER0
		// was wired to `monotonic-clock`.
		time.Sleep(tick)
	}

	text := "liftoff"
	if platform.CanShowText() {
		fmt.Println(text)
	}
	platform.SetStatus(1, 0, 0)
	return &countdownv1.CountResponse{Text: text}, nil
}
