package platform

import (
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

// Slot indices into the payload. See the package note above: the names are
// placeholders on purpose.
const (
	Status1 = 0
	Status2 = 1
	Status3 = 2
)

// SetStatus publishes the three status bytes.
//
// Cheap and safe to call as often as the app likes. An app should NOT rate-limit
// on the assumption that something is watching: that is the tier's decision, and
// an app that guesses will guess differently on every tier.
func SetStatus(status1, status2, status3 byte) {
	Emit(StatusTopic, []byte{status1, status2, status3})
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
func ParseStatus(payload []byte) (status1, status2, status3 byte, ok bool) {
	if len(payload) != StatusBytes {
		return 0, 0, 0, false
	}
	return payload[Status1], payload[Status2], payload[Status3], true
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
