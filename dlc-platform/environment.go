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

	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"
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
	return true, nil
}

// resetEnvironment returns to the unset state. Tests only — a host has no reason
// to un-know what it can do.
func resetEnvironment() { env = nil }
