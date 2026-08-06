package platform

// Boot — the startup sequence every in-process host performs, owned by the
// platform instead of copied into each one.
//
// WHY THIS IS NOT FIVE LINES IN A TEMPLATE. The steps are ordered, and every
// misordering fails somewhere that does not look like an ordering bug:
//
//	grant the root      before the manifest, which DESCRIBES the root
//	install the sink    before the manifest, which may itself emit
//	send the manifest   before any other command, because capability verbs
//	                    register from it (see RegisterDiscovered) — send it
//	                    late and `export-fs` does not exist yet
//	build the CLI       after registration settles, or the surface is a
//	                    snapshot of a half-registered engine
//
// A template that copied those lines would freeze them into every app
// scaffolded that day (§16.6): a fix to the order would reach apps generated
// afterwards and no others. Here, it reaches all of them.
//
// The web tier necessarily performs the same sequence in TypeScript
// (hosts/web/worker.ts) — two host runtimes, so two implementations — but each
// is written once and INHERITED, not once per app. docs/ENVIRONMENT-PLAN.md
// §2.5 is the sequence both must follow.

import (
	"errors"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// BootOptions is what a host knows and the engine does not.
//
// A struct rather than positional arguments because this list will grow: an
// added capability is then a new field that existing hosts ignore, not a
// signature change that breaks every one of them at once.
// Deliberately NOT here: the app's version string. Every app calls
// SetVersion from its ENGINE's init, which runs on every tier — a host-supplied
// version would exist natively and be missing in a browser tab, where no Go host
// runs at all.
type BootOptions struct {
	// Root is the filesystem root being GRANTED (§3·5). Required — there is no
	// "wherever you happen to be standing" default, because reset-fs is
	// inherited and a wrong root makes it delete a user's directory.
	//
	// Use platform.AppRoot(name) for the ./.<app>/ convention, or "." for an app
	// whose data IS the user's project (dlc).
	Root string

	// FilesystemKind describes what Root actually is, for an app deciding what
	// to cache. Required when a root is granted: the host is the only party that
	// knows, and guessing from the path would be inference dressed as fact.
	FilesystemKind ilcv1.FilesystemKind

	// Ephemeral marks a store that does not survive the process. An app may
	// still write; it should not promise the user durability.
	Ephemeral bool

	// Isolation says whether this root belongs to ONE person (§3·5).
	//
	// The ENUM rather than a bool, deliberately, so all three states survive to
	// the wire: a host that has not thought about it (UNSPECIFIED) is genuinely
	// different from one that checked and found the store shared (SHARED). A
	// bool would have to collapse them, and it would collapse them into a claim
	// nobody made.
	//
	// NOT required, unlike FilesystemKind, because the zero value is SAFE here:
	// unset reads as "not isolated", so an app needing privacy refuses rather
	// than assumes. FilesystemKind has no safe default — guessing there points
	// reset-fs at the wrong directory — which is why that one is refused and
	// this one is not.
	Isolation ilcv1.Isolation

	// Sink receives emitted events, or nil on a tier where nothing listens —
	// which an app must not be able to detect (Decision 33).
	Sink EventSink

	// NoFilesystem declares that this host has NOTHING TO GRANT.
	//
	// Not a convenience. Root's error message has always ended "…or say so
	// explicitly", and until now there was no way to say so — so a host with no
	// storage could not boot at all, and one that lied with a fake root got a
	// manifest claiming a filesystem was PRESENT. The keyboard tier (RP2040, no
	// flash filesystem) is the first host for which that is simply the truth.
	//
	// A SEPARATE FIELD RATHER THAN AN EMPTY ROOT, because those are different
	// claims and only one of them is safe to infer. An empty Root is far more
	// often a host that forgot to set it than a host that has no storage, and
	// reading the first as the second would turn a bug into a silently
	// degraded app — every filesystem verb quietly missing, which looks like a
	// platform fault from inside the app.
	//
	// It contradicts Root and FilesystemKind, and contradictions are refused
	// below rather than resolved by precedence: whichever way a precedence rule
	// fell, half the callers who hit it would be surprised.
	NoFilesystem bool
}

// Boot runs the startup sequence in the one order that works.
//
// Sends the manifest as revision 1: this is launch, so there is nothing to be
// newer than. Later re-sends are the host's own business — a resize, an index
// going away — and go through Execute with an incremented revision.
//
// The manifest goes through Execute rather than calling applyEnvironment
// directly, even though a native host is linked in-process and a direct call
// would work. If native took the shortcut, the native tier would never dispatch
// id 2 at all, and parity would be comparing two different startup sequences —
// which is precisely the divergence it exists to catch.
func Boot(opts BootOptions) error {
	if opts.NoFilesystem {
		// Refuse the contradiction instead of picking a winner. A host that sets
		// both has two beliefs about itself and needs to lose one; guessing which
		// would hide the confusion at exactly the moment it is cheapest to see.
		if opts.Root != "" {
			return errors.New("boot: NoFilesystem is set but a root was granted — a host has storage or it does not")
		}
		if opts.FilesystemKind != ilcv1.FilesystemKind_FILESYSTEM_KIND_UNSPECIFIED {
			return errors.New("boot: NoFilesystem is set but a filesystem kind was given — there is nothing for it to describe")
		}
	} else {
		if opts.Root == "" {
			return errors.New("boot: no filesystem root — grant one (see platform.AppRoot), or set NoFilesystem if this host has no storage")
		}
		if opts.FilesystemKind == ilcv1.FilesystemKind_FILESYSTEM_KIND_UNSPECIFIED {
			return errors.New("boot: filesystem kind unset — the host is the only party that knows what its root is")
		}

		if err := SetRoot(opts.Root); err != nil {
			return err
		}
	}
	// Before the manifest: SetEnvironment may emit, and a sink installed
	// afterwards would miss the first event with nothing to indicate it had.
	if opts.Sink != nil {
		SetEventSink(opts.Sink)
	}

	// ABSENT carries no kind, no ephemerality and no isolation — they describe a
	// root, and there is no root to describe. Sending the zero values alongside
	// PRESENT would be a claim; sending them alongside ABSENT would be noise a
	// reader could mistake for one.
	fs := &ilcv1.Filesystem{Availability: ilcv1.Availability_AVAILABILITY_ABSENT}
	if !opts.NoFilesystem {
		fs = &ilcv1.Filesystem{
			Availability: ilcv1.Availability_AVAILABILITY_PRESENT,
			Kind:         opts.FilesystemKind,
			Ephemeral:    opts.Ephemeral,
			Isolation:    opts.Isolation,
		}
	}
	body, err := (&ilcv1.SetEnvironmentRequest{
		Environment: &ilcv1.Environment{
			Revision:   1,
			Filesystem: fs,
		},
	}).MarshalVT()
	if err != nil {
		return errors.New("boot: encode environment: " + err.Error())
	}
	if _, ok := registry[MethodSetEnvironment]; !ok {
		// Otherwise this surfaces as "unknown method_id 2", which names the
		// symptom and not the cause. The cause is always the same: the app's
		// engine package never registered, because the host forgot the
		// blank import that runs its init.
		return errors.New("boot: no commands registered — import the app's engine package so its init runs (RegisterAll / RegisterDiscovered)")
	}
	if res := Execute(MethodSetEnvironment, body); !res.Success {
		return errors.New("boot: " + res.Err)
	}
	return nil
}
