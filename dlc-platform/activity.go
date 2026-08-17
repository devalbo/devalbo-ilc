package platform

// WHAT THE APP IS DOING RIGHT NOW (BADGE-CONTROL-PLAN D3a).
//
// # The gap this fills
//
// A world can report that a guest is EXECUTING. It cannot report what the guest
// is executing at, because only the app knows: a countdown on tick 3 of 5 and a
// bundle import on file 400 of 1000 look identical from outside. That is exactly
// the resolution a "stuck or slow?" question needs.
//
// # The default is DERIVED, not written down
//
// The obvious design is a line at the top of every handler and a matching one in
// the scaffold template. This repo has already paid for that shape twice: the
// generated `AppServiceCLI` reached nobody because it needed a hand-written
// registration, and a hand-mirrored fact is one an app can forget, get wrong, or
// leave stale after a rename.
//
// The DISPATCHER already knows. `Execute` routes on a method id and the
// registered command spec holds that command's name, so "running: greet" is
// derivable for every app with NO APP CODE AT ALL. An app that wants better says
// so; an app that says nothing still reports something true.
//
// # Why it travels as an event
//
// `emit` is an import, so host code runs while the guest is suspended inside it
// (SESSION-AND-SURFACE-PLAN D4a). An `ilc.activity` event therefore arrives as
// the app changes it, rather than when the command returns — and progress
// reported after the thing it described has finished is not progress.
//
// # Its relationship to the status bytes
//
// `status.go` sketched `Status2` as ACTIVITY: "a short-term value where the
// CHANGE is the point". This is the same idea at a different resolution. A byte
// can drive a blink on a tier with one LED; a string can say what is happening on
// a tier with a screen. Both stay, because neither tier should have to render the
// other's form.

// ActivityTopic is the reserved event carrying what the app is doing.
//
// `ilc.` for the same reason `ilc.status` uses it: the namespace matches the WIT
// interface these ride on, and renaming one alone would leave a world matching a
// topic no app emits.
const ActivityTopic = "ilc.activity"

// SetActivity says what the app is doing now.
//
// Free to call as often as the app likes, and MEANT to be called during long
// work — that is the case it exists for. A tier with nowhere to show it discards
// the event, which an app cannot detect (Decision 33).
//
//	platform.SetActivity("tick 3 of 5")
//
// Leaving it unset is fine: the dispatcher publishes the command's name, so the
// answer is the command rather than nothing.
func SetActivity(what string) {
	Emit(ActivityTopic, []byte(what))
}

// publishDefaultActivity announces the command about to run.
//
// Called by Execute before dispatch, so a world knows which command is in flight
// even for an app that never mentions activity. Silent when no spec is
// registered — there is no name to publish, and inventing "method 10000" would
// be worse than saying nothing.
func publishDefaultActivity(method uint32) {
	for _, command := range commandSpec {
		if command.Method == method {
			SetActivity(command.Name)
			return
		}
	}
}
