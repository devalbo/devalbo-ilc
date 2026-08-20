package platform

import (
	dlcstdv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/dlc/std/v1"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// THREE BYTES AN APP CAN SET, AND A TIER RENDERS HOWEVER IT CAN.
//
// The problem this solves: a badge with one RGB LED, a CLI with a terminal, and
// a browser with a whole DOM all want to show "how is it going" — and an app
// that had to know which one it was talking to would stop being portable. So the
// app publishes three bytes and each tier maps them to what it has.
//
// **This is not a new capability, deliberately.** It rides `devalbo:ilc/events`,
// which already exists, is already imported by every component, and is already
// the channel for "say what happened, let the tier decide what that looks like"
// (Decision 33/34, D6). A new WIT import for three bytes would be exactly the
// mistake those decisions exist to prevent — and it would fork the artifact,
// which is the one thing the architecture cannot afford.
//
// **A tier that renders none of it is not broken.** A capability's absence is a
// no-op, never an error: this does nothing on a host with no indicator, and the
// app cannot tell. That is what makes it safe to call unconditionally.
//
// # The names are deliberately neutral, and the meanings are NOT settled
//
// They are `Status1`, `Status2`, `Status3` because naming them for a purpose
// would freeze a decision still being made, and a wrong name outlives the
// thinking that produced it.
//
// **CANDIDATE NAMES, kept as prompts for that decision** — they are the current
// intent, not a description of what the code enforces. The code enforces three
// bytes and an order; everything below is an argument to have:
//
//	Status1  "STATE" —   a PERSISTENT condition — true until changed. A tier with one
//	         indicator should render this one.
//	Status2  "ACTIVITY" — a SHORT-TERM value, where the CHANGE is the point. A tier can
//	         render movement as a blink, so an app that twiddles it produces
//	         visible activity without knowing blinking exists.
//	Status3  "DETAIL" —  app-defined. Progress, sub-mode, error class — whatever a richer
//	         tier can show and a poorer one ignores.
//
// A tier with three colour channels may equally map them straight to R/G/B. The
// contract is THREE BYTES AND AN ORDER; what they mean is convention, and
// convention is what is still under discussion.

// StatusTopic is the reserved event topic carrying the three status bytes.
//
// `ilc.` rather than `dlc.` because it matches the WIT interface this rides on
// (`devalbo:ilc/events`) and the `ILC_*` advertisement keys. That is a
// NAMESPACE, not branding — they travel together, and renaming one alone would
// leave a host matching a topic no app emits.
const StatusTopic = "ilc.status"

// StatusBytes is what a tier receives: three bytes, in a fixed order.
const StatusBytes = 3

// Slot indices into the payload. THE WIRE IS STILL THREE BYTES AND AN ORDER —
// that is the contract with every world, and the struct below is a shape for
// Go, not a change to it.
const (
	Status1 = 0
	Status2 = 1
	Status3 = 2
)

// StatusLevel is slot 1: how it is going.
//
// WHAT IT ACTUALLY DOES, which is the part a generic name cannot carry. On the
// badge (`world.rs: status_from_slot1`) each value becomes a panel colour and a
// backlight state:
//
//	StatusLevelIdle     ->  blue    alive, nothing to do
//	StatusLevelOK       ->  green   working, or finished and fine
//	StatusLevelWarning  ->  amber   look at this; NOT a failure
//	StatusLevelError    ->  red     it went wrong
//	StatusLevelOff      ->  (none)  leave the indicator alone
//	StatusLevelUnspecified -> (none) same, but because nobody spoke
//
// The last two land in the same place for OPPOSITE reasons, and a richer world
// may act on the difference: "go dark" is a decision, silence is not. A world
// that cannot show a colour discards all of it, which is a no-op and never an
// error — that is what makes this safe to call unconditionally.
type StatusLevel byte

// Status is what an app says about how it is going — the channel that survives
// when text does not.
//
// # What renders this TODAY, per field
//
// These names are generic because the idea is, and a generic name is exactly the
// one that needs its effect stated where it is declared:
//
//	field            | badge (rp2350)           | native CLI | browser
//	-----------------|--------------------------|------------|--------
//	IndicatorStatus  | panel colour + backlight | —          | —
//	Activity         | nothing yet              | —          | —
//	Detail           | nothing yet              | —          | —
//
// ONLY THE BADGE RENDERS ANY OF IT, and only `IndicatorStatus`: `main.rs` reads `body[0]`
// and ignores the rest. `Activity` and `Detail` have NO reader anywhere in this
// repo. They are written by `hello`'s countdown and go nowhere — kept because
// the wire is three bytes and an order, and a world that grows an indicator
// should not need an app change to drive it.
//
// Stating that plainly matters more than it sounds: an app author reading only
// the field names would reasonably assume something acts on them.
//
// # Typical use
//
//	// the common case — one line, at the end of a command
//	platform.SetStatus(platform.Status{IndicatorStatus: platform.StatusLevelOK})
//
//	// a warning the app handled, so the badge shows amber rather than red
//	platform.SetStatus(platform.Status{IndicatorStatus: platform.StatusLevelWarning})
//
//	// motion during long work: the CHANGE is the point, not the value
//	platform.SetStatus(platform.Status{
//		IndicatorStatus: platform.StatusLevelOK,
//		Activity:        byte(remaining),
//	})
//
// # Why a struct and not three bytes
//
// It was `SetStatus(status1, status2, status3 byte)`, and every call but one
// read `SetStatus(x, 0, 0)` — two trailing zeros of noise at each site, and a
// signature where passing a level into slot 2 compiles cleanly. Positional
// arguments are a schema written in argument ORDER, which is the one part of a
// signature the compiler cannot check when the parameters share a type. The
// control channel's own encoders were taken this way for the same reason.
//
// # Why not simply an enum
//
// Because `Activity` has a use even without a renderer: it is the only field
// whose CHANGE is meaningful, so a tier that grows a blink can drive it from
// apps that already exist.
//
// # The names
//
// The package note above kept these as `Status1/2/3` because naming them would
// freeze a decision still being made, and listed ACTIVITY and DETAIL as
// candidates. Slot 1 is now settled — it is `StatusLevel`, in the DLC standard
// set — so naming it is honest rather than premature. The other two adopt the
// candidate names; if that turns out wrong they are a rename in one file, not a
// wire change.
type Status struct {
	// THE PERSISTENT CONDITION — true until changed, and the one field anything
	// currently renders. See `StatusLevel` above for the colour each value
	// becomes on a badge.
	//
	// NAMED FOR WHAT IT DRIVES — the tier's indicator — rather than for the byte
	// it is. `Level` alone said nothing and left a reader to find the meaning
	// elsewhere, which is the cost of a generic name on a struct an app author
	// meets before any of the prose above.
	//
	// NOT A CLAIM THAT SOMETHING IS WRONG: `IDLE` and `OK` are the common
	// values. A tier renders the whole range however it can — a colour on the
	// badge, nothing at all where there is no indicator, which is a no-op and
	// never an error.
	IndicatorStatus StatusLevel

	// A SHORT-TERM value where the CHANGE is the point, not the number.
	//
	// NOTHING READS THIS YET. `hello`'s countdown writes the remaining ticks so
	// a world with one indicator could show motion; no world does. See
	// `SetActivity` in activity.go for the same idea as a STRING, which a tier
	// with a screen can show and which is rendered today.
	Activity byte

	// App-defined: progress, sub-mode, error class — whatever a richer tier can
	// show and a poorer one ignores.
	//
	// NOTHING READS THIS YET EITHER, and nothing writes it. It exists because
	// the wire carries three bytes; spending the third on "whatever the app
	// means" costs nothing and avoids a protocol change the day something wants
	// it.
	Detail byte
}

// THE VALUES, from the DLC standard enum rather than retyped.
//
// Re-exported here so an app does not have to know which generated package they
// came from to set a light. They were literals at every call site — `hello` had
// its own `Colour` enum and translated it into numbers here, the badge decoded
// the same numbers at the other end, and the two agreed for three values and
// disagreed at both ends: `COLOUR_OFF` went as 0, which the badge read as IDLE
// and rendered BLUE. Naming them is what makes that a compile error.
const (
	StatusLevelUnspecified = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_UNSPECIFIED)
	StatusLevelIdle        = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_IDLE)
	StatusLevelOK          = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_OK)
	StatusLevelWarning     = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_WARNING)
	StatusLevelError       = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_ERROR)
	StatusLevelOff         = StatusLevel(dlcstdv1.StatusLevel_STATUS_LEVEL_OFF)
)

// SetStatus publishes the three status bytes.
//
// Cheap and safe to call as often as the app likes. An app should NOT rate-limit
// on the assumption that something is watching: that is the tier's decision, and
// an app that guesses will guess differently on every tier.
func SetStatus(s Status) {
	Emit(StatusTopic, []byte{byte(s.IndicatorStatus), s.Activity, s.Detail})
}

// ParseStatus reads the three bytes back out of an event payload.
//
// NO CALLER YET, AND THAT IS EXPECTED. Rendering belongs to the WORLDS — a badge
// mapping the bytes to its screen or an RGB LED, a richer tier to whatever it
// has — and none has been written. This is the half a renderer needs, kept
// deliberately rather than left for a dead-code sweep to find: an emit path with
// no decoder would have every world re-derive the layout, and they would not all
// derive the same one.
//
// For HOSTS, not apps. Returns false if the payload is the wrong shape, because
// a tier must not render garbage from a topic that happens to collide — events
// are strings and any app may emit any topic it likes.
func ParseStatus(payload []byte) (Status, bool) {
	if len(payload) != StatusBytes {
		return Status{}, false
	}
	return Status{
		IndicatorStatus: StatusLevel(payload[Status1]),
		Activity:        payload[Status2],
		Detail:          payload[Status3],
	}, true
}

// ---------------------------------------------------------------------------
// What outlets does this tier actually have — and how much room, right now?
// ---------------------------------------------------------------------------
//
// An app should DEFER TO THE WORLD rather than guess. The division that falls
// out of it:
//
//	text is shown      -> print prose; the tier has an outlet for it
//	text is NOT shown  -> do not spend heap formatting it; use SetStatus, which
//	                      a tier with one LED can still render
//
// # The manifest is the only source
//
// CAPABILITY IS NOT ALLOCATION. "Can this tier show text at all" is settled when
// the host starts; "how much room has the app got" is an allocation a world can
// take back mid-session for an alert or a menu. They are the same statement at
// two resolutions and they can disagree, so they must not be two independent
// declarations — which is why there is exactly one, and it is the manifest.
//
// The manifest is also the only one of the candidates that can be CORRECTED. A
// `wasi:cli/environment` key is read once during `_initialize` and can never be
// re-read, so a budget announced there is frozen at the value it had before the
// app ever ran. Embedded worlds still set those keys as a pre-command bootstrap
// — they are what shows up in a boot log — but nothing here reads them, because
// a second source is a second thing that can be stale.
//
// So an app gets both halves without a new capability:
//
//	POLL    Env().GetTextOut() — a cached field read, no host round trip
//	NOTIFY  OnEnvironmentChange — the host re-sent a manifest and it differs
//
// There are deliberately NO convenience accessors over that. `TextOutlet()`,
// `TextCols()` and `TextRows()` existed for an afternoon and were deleted: two
// of them had no caller at all, and the third was one indirection over `Env()`
// that re-spelled an enum as a string — a second vocabulary the compiler could
// not exhaust. `CanShowText` stays because it is a DECISION rather than a field
// read, and every app makes it.
//
// An app wanting the budget reads `Env().GetTextOut()` directly. There is no
// availability check to remember, because there is no availability field: it
// was a second encoding of what `outlet` already says, and it had grown a rule
// (treat cols/rows as unknown unless PRESENT) that a host could silently fall
// foul of. Deleting the field deleted the rule.

// CanShowText reports whether printing is worth doing.
//
// FAILS OPEN: anything other than an explicit "none" counts as yes, including
// silence. A tier that has not said is more likely to be an ordinary terminal
// than a badge with no screen, and the cost of printing where nobody looks is a
// few discarded bytes — against the cost of staying silent where somebody is
// watching, which is an app that appears broken.
func CanShowText() bool {
	return Env().GetTextOut().GetOutlet() != ilcv1.TextOutlet_TEXT_OUTLET_NONE
}

// CurrentWorld reports which host slot the app is running in.
//
// # Diagnostic, not a switch
//
// This is IDENTITY, and the manifest groups it under `identity` to say so. Read
// it to REPORT what you are running in; do not branch on it. An app that writes
// `if CurrentWorld() == names.WorldBadgeNormal` has re-implemented a capability
// check against a name that will be wrong for the next world — and it will
// compile, pass, and quietly do the wrong thing there. Ask CanShowText or
// CanShowStatus instead; those adapt to worlds written after the app.
//
// # It returns names.World, not a second type
//
// A world already has a canonical list — names/WORLDS.tsv, generated into Go and
// Rust, checked by `make verify-names`. The first draft of this returned a new
// proto enum, and the Go compiler rejected it for colliding with the `World`
// that was already three feet away. That collision was the correct answer to the
// wrong idea.
func CurrentWorld() World {
	switch Env().GetIdentity().GetName() {
	case ilcv1.WorldName_WORLD_NAME_NATIVE:
		return WorldNative
	case ilcv1.WorldName_WORLD_NAME_BROWSER:
		return WorldBrowser
	case ilcv1.WorldName_WORLD_NAME_BADGE_NORMAL:
		return WorldBadgeNormal
	case ilcv1.WorldName_WORLD_NAME_BADGE_MINIMAL:
		return WorldBadgeMinimal
	case ilcv1.WorldName_WORLD_NAME_UNKNOWN:
		return WorldUnknown
	default:
		// NOBODY SAID, which is the common case: an app that runs everywhere has
		// no reason to name a world, and most hosts never send one.
		return WorldUndefined
	}
}

// CanShowStatus reports whether SetStatus reaches anybody.
//
// FAILS CLOSED, unlike CanShowText, and the asymmetry is deliberate. Text has a
// fallback a host can always provide — stdout exists everywhere, so printing
// where nobody looks costs a few discarded bytes. A status indicator has no
// fallback: a host either has one or does not, and there is nothing to degrade
// to. So silence here means "no", and an app that wants to be seen prints as
// well.
//
// It is never an ERROR either way: SetStatus is safe to call unconditionally on
// every tier, and this exists so an app can skip work it knows is invisible —
// not so it can decide whether calling is allowed.
func CanShowStatus() bool {
	return Env().GetStatus() == ilcv1.StatusOutlet_STATUS_OUTLET_COLOR
}
