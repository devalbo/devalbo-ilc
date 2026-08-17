// Package engine holds badge-selftest's business logic — ALL of it.
//
// WHAT THIS APP IS FOR. It is the first app written for the `undefined` world,
// and its job is to answer "does this host actually work?" after the official
// apps are installed — before anyone debugs an app on a board that was never
// good.
//
// WHY AN APP RATHER THAN MORE FIRMWARE, which is the whole design argument:
// firmware can only report what firmware can see. A component exercises the
// capability seam FROM THE GUEST SIDE, which is the side that breaks — a host
// can believe it granted stdout while the guest's handle is unusable, and only
// the guest can tell you. It is also the honest end-to-end test of the loader:
// if a scaffolded app dragged onto a badge runs and reports, the whole path is
// proven.
//
// IT RUNS UNDER THE STRICTEST PROFILE, deliberately. `undefined` is the default
// world and resolves to the portable name profile, which is the right footing
// for a test whose value is being believable everywhere.
//
// Rules that keep it portable:
//   - Never call a platform API directly; use the injected capabilities.
//   - Stay TinyGo-safe and reflection-free — no encoding/json, no text/template.
//   - Touch the filesystem only through platform.SafeJoin / platform.WriteTree.
package engine

import (
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	platform "github.com/devalbo/devalbo-ilc/dlc-platform"

	badgeselftestv1 "github.com/you/badge-selftest/gen/go/badge_selftest/v1"
	"github.com/you/badge-selftest/gen/go/dlcconfig"
)

const MethodSelfTest = badgeselftestv1.MethodSelfTest

func init() {
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())
	platform.RegisterRaw(badgeselftestv1.AppServiceHandlers(handleSelfTest))

	// WHAT THE COMMANDS TAKE, so a host that cannot compile this app's
	// schema can still collect input for it — a badge running payloads it
	// was never built for, or a browser that would otherwise hand-write an
	// <input> per field. Without this the description is stripped from the
	// wasm as dead code, because only the native CLI referenced it.
	platform.RegisterCommandSpec(badgeselftestv1.AppServiceCLI)
}

// check is one question and its answer.
//
// A SLICE OF THESE RATHER THAN A RUNNING STRING, because the counts have to come
// out right and a report assembled by appending prose cannot be counted. The
// caller gets both: the text to show and the numbers to act on.
type check struct {
	name   string
	passed bool
	detail string
}

func handleSelfTest(req *badgeselftestv1.SelfTestRequest) (*badgeselftestv1.SelfTestResponse, error) {
	var checks []check

	checks = append(checks, checkAdvertisement()...)
	checks = append(checks, checkRandom())
	checks = append(checks, checkClock())
	if req.ReadOnly {
		checks = append(checks, check{
			name:   "filesystem",
			passed: true,
			detail: "skipped (read-only)",
		})
	} else {
		checks = append(checks, checkFilesystem())
	}
	checks = append(checks, checkEvents())

	var passed uint32
	var b strings.Builder
	for _, c := range checks {
		if c.passed {
			passed++
			b.WriteString("ok   ")
		} else {
			b.WriteString("FAIL ")
		}
		b.WriteString(c.name)
		if c.detail != "" {
			b.WriteString(": ")
			b.WriteString(c.detail)
		}
		b.WriteString("\n")
	}
	b.WriteString(strconv.FormatUint(uint64(passed), 10))
	b.WriteString("/")
	b.WriteString(strconv.Itoa(len(checks)))
	b.WriteString(" passed")

	// ALSO PRINTED, and this is not redundancy — it is the only channel a
	// GENERIC host can read.
	//
	// A typed response is protobuf on the wire, and rendering it needs this app's
	// schema. The CLI has that (its `Render` map is compiled in); the badge does
	// not and never will — one loader firmware runs apps it was never built for,
	// which is the entire point of the payload region. So the readable channel on
	// such a host is stdout, which is exactly what `ILC_STDOUT` advertises.
	//
	// Found by running this app under the badge's own host: `hello` appeared to
	// work only because its encoded response happened to be valid UTF-8 (the
	// leading field tag 0x0a reads as a newline). This one is not, and printed
	// nothing at all.
	// Println, not Print: the stream is one of two things a host may show, and
	// without the terminator it runs into whatever the host prints next.
	fmt.Println(b.String())

	// NOT an error when checks fail. A self-test that reports "3/5" has
	// SUCCEEDED at its job — the command worked, and the finding is the payload.
	// Returning an error would make a host show "the app failed", which is a
	// different and less useful claim.
	return &badgeselftestv1.SelfTestResponse{
		Text:   b.String(),
		Checks: uint32(len(checks)),
		Passed: passed,
	}, nil
}

// checkAdvertisement reads what the TIER SAYS IT IS and reports it back.
//
// THE MOST VALUABLE CHECK HERE, because it is the one a host cannot self-report
// honestly: firmware printing its own advertisement proves only that it can
// print. Reading it from inside the guest proves the environment actually
// crossed the boundary — and an app that adapts to `ILC_STDOUT=none` is relying
// on exactly this path working.
func checkAdvertisement() []check {
	tier := os.Getenv("ILC_TIER")
	world := os.Getenv("ILC_WORLD")

	// THE OUTLET COMES FROM THE MANIFEST, not from `ILC_STDOUT`.
	//
	// The wasi key is still set by embedded worlds and still shows up in a boot
	// log, but it is frozen at `_initialize` and nothing in the platform reads it
	// any more. An allocation moves — a world that takes screen back for a menu
	// corrects the manifest and cannot correct the key — so a self-test reading
	// the key would report a value that was true before the app ever ran.
	//
	// Reading it the way an ordinary app does is also the more honest check: this
	// exercises the path every other app depends on, rather than a debug surface
	// only this one touches.
	stdout := platform.Env().GetTextOut().GetOutlet().String()

	// ABSENCE IS NOT A FAILURE, and an earlier version of this got that wrong —
	// it reported FAIL on the native CLI, which advertises nothing because
	// `undefined` is a legitimate and common world. A check that fails on correct
	// behaviour trains people to ignore it.
	//
	// What IS checkable is CONSISTENCY: the advertisement is all-or-nothing, so a
	// host that sets some keys and not others has a bug the app can actually
	// detect. `ILC_TIER` without `ILC_WORLD` means somebody added a key and
	// forgot the table it belongs to.
	partial := (tier == "") != (world == "")
	out := []check{{
		name:   "advertisement",
		passed: !partial,
		detail: advertisementDetail(tier, world, partial),
	}}

	// Reported, never judged: no single command can prove a tier honours what it
	// claims, and pretending otherwise would be a check that cannot fail.
	out = append(out, check{
		name:   "stdout",
		passed: true,
		detail: describeStdout(stdout),
	})
	return out
}

func advertisementDetail(tier, world string, partial bool) string {
	if partial {
		return "partial - one of ILC_TIER/ILC_WORLD is missing"
	}
	if tier == "" && world == "" {
		return "none (undefined world)"
	}
	return tier + "/" + world
}

func describeStdout(stdout string) string {
	switch stdout {
	case "":
		return "unstated"
	case "none":
		// The signal to stop formatting text nobody will see.
		return "not shown - emit events instead"
	default:
		return stdout
	}
}

// checkRandom asks for two values and expects them to differ.
//
// Weak on purpose: the badge's generator is xorshift standing in for hardware,
// so this is checking that randomness is WIRED, not that it is good. A test that
// demanded entropy would fail on a host that is behaving exactly as documented.
func checkRandom() check {
	// `crypto/rand` rather than `math/rand`, because it is the one that reaches
	// the `wasi:random` IMPORT — which is the thing being tested. math/rand would
	// happily produce numbers from a seed and prove nothing about the host.
	var a, b [8]byte
	if _, err := rand.Read(a[:]); err != nil {
		return check{name: "random", detail: err.Error()}
	}
	if _, err := rand.Read(b[:]); err != nil {
		return check{name: "random", detail: err.Error()}
	}
	if a == b {
		return check{name: "random", detail: "two reads returned the same bytes"}
	}
	return check{name: "random", passed: true}
}

// checkClock asks whether the monotonic clock advances.
//
// The WALL clock is deliberately not asserted: the badge has no RTC wired and
// reports epoch zero, which is honest rather than broken. Failing on it would
// make this app report a fault where the host is telling the truth.
func checkClock() check {
	first := time.Now()
	// A little work, so a coarse clock still has something to see.
	sink := 0
	for i := 0; i < 10000; i++ {
		sink += i
	}
	_ = sink
	if !time.Now().After(first) && time.Now() == first {
		return check{name: "clock", detail: "monotonic did not advance"}
	}
	year := first.Year()
	if year < 2000 {
		// Reported, and PASSED: a tier with no RTC is a documented state.
		return check{name: "clock", passed: true, detail: "monotonic ok, no wall clock"}
	}
	return check{name: "clock", passed: true}
}

// checkFilesystem writes a file, reads it back, and removes it.
//
// AN ABSENT FILESYSTEM IS NOT A FAILURE — it is the capability model working.
// The badge grants no preopens today, so this reports "not granted" and passes.
// What would be a failure is a host that ACCEPTS a write and then cannot read it
// back, which is the one case a caller could not have predicted.
func checkFilesystem() check {
	// `FSRootGranted` FIRST — `FSRoot()` PANICS when no host granted one, and a
	// self-test that takes down the app it is testing is the worst possible bug
	// to ship in this particular program.
	if !platform.FSRootGranted() {
		return check{name: "filesystem", passed: true, detail: "not granted"}
	}
	root := platform.FSRoot()

	path, err := platform.SafeJoin(root, "selftest.tmp")
	if err != nil {
		return check{name: "filesystem", detail: "SafeJoin: " + err.Error()}
	}

	want := "ilc-selftest"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		// A refusal is a legitimate host answer, and it names the operation.
		return check{name: "filesystem", passed: true, detail: "read-only: " + err.Error()}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		return check{name: "filesystem", detail: "wrote but could not read: " + err.Error()}
	}
	if string(got) != want {
		return check{name: "filesystem", detail: "read back the wrong bytes"}
	}
	// Best-effort: a host that cannot remove is not broken, it is limited.
	_ = os.Remove(path)
	return check{name: "filesystem", passed: true, detail: "read/write ok"}
}

// checkEvents emits one, so a tier that renders events has something to render.
//
// **This is the check the minimal world exists for.** A badge with no text
// capability turns an event into a colour, so emitting one here is how this app
// says anything at all on a host that cannot show a string. Per Decision 33 a
// capability's absence is a no-op, never an error — so this cannot fail, and
// that is the point rather than a weakness.
func checkEvents() check {
	platform.Emit("selftest.complete", nil)
	return check{name: "events", passed: true, detail: "emitted"}
}
