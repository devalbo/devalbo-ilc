package platform

// The environment manifest (§6.4a, Decision 32) — what THIS host can actually
// do, as data on the one command boundary.
//
// WHY THIS EXISTS. §6.5 promises graceful degradation when a capability is
// missing, and until now that promise was false: an app called WriteTree and
// either it worked or it returned an error the app had no way to anticipate.
// There was no way to ASK. The manifest is the asking half.
//
// WHY THE HOST PUSHES IT rather than the engine querying. A query is an import,
// and imports are the expensive direction — WIT has no optional ones, so every
// tier including future embedded targets would have to supply it or the
// component would not link. Worse, a query returns a value and is therefore
// synchronous, while probing OPFS is inherently async and jco only supports
// async imports under --async-imports (refused, Decision 22). A synchronous
// describe() in the worker could only answer from a pre-computed cache, which
// IS this manifest: the pull collapses into a push plus a cache, having paid
// for an import to get there. A push is also bytes on a boundary — recordable,
// diffable, and pinnable in a parity vector — where a query would make engine
// behaviour depend on host code running mid-command.

import (
	"errors"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// env is the manifest in force, or nil when no host has sent one.
//
// Nil is a real state, not an oversight: an app may ask before the host has
// spoken, and the answer then is "assume nothing" — see Env.
var env *ilcv1.Environment

// Env returns the environment in force.
//
// NEVER NIL. An unset manifest reads as a zero-valued one, in which every
// availability is UNSPECIFIED and therefore not PRESENT, so an app that asks
// before the host has spoken takes its fallback path rather than crashing on a
// nil deref or believing in a capability nobody promised.
//
// That default is not the silent defaulting this codebase keeps deleting, and
// the difference is worth stating because it looks identical from a distance. A
// defaulted tier list is a GUESS about intent — either answer might have been
// wanted, and choosing one scaffolds a layout nobody picked. Assuming a
// capability is absent is the conservative direction: the app does what works
// everywhere, and being wrong costs performance rather than data. A default is
// dangerous when the safe option and the convenient one differ. Here they are
// the same.
func Env() *ilcv1.Environment {
	if env == nil {
		return &ilcv1.Environment{}
	}
	return env
}

// HasFilesystem reports whether this host granted a filesystem.
//
// Only AVAILABILITY_PRESENT counts. UNSPECIFIED — nobody said — is treated as
// absent for the reason above, though the two are kept distinct on the wire so
// that "the host forgot" stays diagnosable.
func HasFilesystem() bool {
	return Env().GetFilesystem().GetAvailability() == ilcv1.Availability_AVAILABILITY_PRESENT
}

// Isolated reports whether the host granted a root belonging to ONE person
// (§3·5), rather than one others may also see.
//
// FOR APPS THAT HOLD PRIVATE DATA, and only those. It answers "is privacy my
// problem?": a per-user root means everything visible belongs to one person; a
// shared one means access control is the app's own responsibility. An app that
// assumes the first while running in the second leaks, and leaks quietly.
//
// Only PER_USER counts. UNSPECIFIED — nobody said — reads as NOT isolated, the
// same conservative direction HasFilesystem takes, and here the conservatism is
// the point: an app that requires privacy refuses to run rather than assuming
// it. A host that forgot to declare fails loudly instead of exposing data.
//
// THIS IS THE HOST'S WORD, NOT A GUARANTEE. A host that grants a shared root
// and reports PER_USER is lying, and no engine-side check can catch it. Use it
// to decide what an honest host is offering, never as a security boundary.
func Isolated() bool {
	return Env().GetFilesystem().GetIsolation() == ilcv1.Isolation_ISOLATION_PER_USER
}

// There is deliberately no HasIndex here (docs/INDEX-PLAN.md D8). The derived
// index is a projection the ENGINE owns, so it exists wherever a filesystem
// does, and an app has nothing to branch on — which is the whole reason the
// index stopped being a host capability.

// applyEnvironment installs a manifest, reporting whether anything changed.
//
// Returns false for a repeat of the revision already in force. That is not an
// optimisation: SetEnvironment TRIGGERS capability registration (see
// commands.go), so re-applying an unchanged manifest would tear down and
// rebuild the command surface underneath a host that only meant to say hello
// twice. The revision is what makes the difference observable.
func applyEnvironment(next *ilcv1.Environment) (bool, error) {
	if next == nil {
		return false, errors.New("set-environment: no environment")
	}
	// Zero is refused rather than read as "the first one". A host that forgot to
	// set the revision would otherwise be indistinguishable from one that is up
	// to date — and every later re-send would look unchanged and be skipped,
	// which is the silent-staleness failure this field exists to make visible.
	if next.Revision == 0 {
		return false, errors.New("set-environment: revision must be non-zero")
	}
	if env != nil && env.Revision == next.Revision {
		return false, nil
	}
	env = next
	// AFTER the swap, so a watcher reading Env() sees the new manifest rather
	// than the one being replaced. A watcher that saw the old value would be
	// worse than no watcher: it would act confidently on stale data.
	notifyEnvironmentChanged()
	return true, nil
}

// ---------------------------------------------------------------------------
// Being told, rather than having to ask again
// ---------------------------------------------------------------------------
//
// The manifest was always dynamic — `revision` exists precisely so a host can
// re-send one — but until now the only thing that reacted was capability
// registration, inside the platform. An APP could not find out. It could poll
// Env() and would get a fresh answer, but nothing told it WHEN to look, so a
// long-running command that laid out its output for 12 rows kept drawing to 12
// rows after the world took four of them back.
//
// So: an app registers, the engine calls back on change. This is the push half
// of what Env() already provides as pull, and between them an app never has to
// guess.
//
// WHY BOTH, rather than picking one. They answer different questions. Polling
// suits code that is about to draw and wants the number it will draw with — it
// is a cached field read, so asking every frame costs nothing. Notification
// suits code that must DO something when the answer changes: recompute a
// layout, drop a cache, redraw. An app with only polling busy-checks or goes
// stale; an app with only callbacks has to mirror the state itself, and that
// mirror is a second copy that can disagree.

// envWatchers are called after a manifest actually changes.
var envWatchers []func()

// OnEnvironmentChange registers fn to run whenever a NEW manifest takes effect.
//
// Fires only on actual change — a host re-sending the revision already in force
// calls nothing, the same rule applyEnvironment applies to registration. Does
// NOT fire for the manifest already in force at the time of registration: an app
// that has just started should read Env() directly, which it can do
// synchronously and cheaply, rather than depending on a callback that may never
// come because nothing has changed yet.
//
// Callbacks run in registration order, synchronously, inside the SetEnvironment
// command. Keep them short and do not call back into the platform's command
// dispatch from one — the surface is being rebuilt underneath you.
//
// There is no deregistration, deliberately: registration happens in init(), the
// app lives as long as the engine, and an Unregister would need handles that
// exist only to support a case nobody has.
func OnEnvironmentChange(fn func()) {
	if fn == nil {
		return
	}
	envWatchers = append(envWatchers, fn)
}

// notifyEnvironmentChanged runs the watchers. Called only when something changed.
func notifyEnvironmentChanged() {
	for _, fn := range envWatchers {
		fn()
	}
}

// resetEnvironment returns to the unset state. Tests only — a host has no reason
// to un-know what it can do.
func resetEnvironment() { env = nil; envWatchers = nil }
