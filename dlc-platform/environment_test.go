// The environment manifest (§6.4a, Decision 32) — the host stating what it can
// do, and the command surface following from it.
package platform

import (
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// cleanEnv isolates a test from the package-level manifest and registry. Both
// are process-global by design — an engine has exactly one environment — so
// tests that change them have to put them back.
func cleanEnv(t *testing.T) {
	t.Helper()
	prevEnv, prevDiscovered := env, discovered
	// The rebuilder is global too, and it DECIDES REGISTRATION — a test that set
	// one and did not put it back would hand the next test an index block it
	// never asked for, in a package whose tests are all about what is registered.
	prevRebuilder := indexRebuilder
	prevRegistry := map[uint32]Handler{}
	for k, v := range registry {
		prevRegistry[k] = v
	}
	t.Cleanup(func() {
		env, discovered, indexRebuilder = prevEnv, prevDiscovered, prevRebuilder
		registry = prevRegistry
	})
	resetEnvironment()
	discovered = nil
	indexRebuilder = nil
	registry = map[uint32]Handler{}
}

func manifest(revision uint32, fs ilcv1.Availability) *ilcv1.SetWorldManifestRequest {
	return &ilcv1.SetWorldManifestRequest{
		WorldManifest: &ilcv1.WorldManifest{
			Revision:   revision,
			Filesystem: &ilcv1.Filesystem{Availability: fs, Kind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR},
		},
	}
}

// send dispatches SetWorldManifest the way a host does — through Execute, not by
// calling the handler. Natively the engine is linked in-process and a direct
// call would work, but then the native tier would never exercise the command
// path that the wasm tier has no choice about, and parity would be comparing
// two different sequences.
func send(t *testing.T, req *ilcv1.SetWorldManifestRequest) Result {
	t.Helper()
	body, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	return Execute(MethodSetWorldManifest, body)
}

// An app that asks before any host has spoken must get the conservative answer,
// not a nil deref and not a capability nobody promised.
func TestUnsetEnvironmentReadsAsAbsent(t *testing.T) {
	cleanEnv(t)
	if Env() == nil {
		t.Fatal("Env() returned nil; it must never be nil")
	}
	if HasFilesystem() {
		t.Fatal("unset manifest reports a filesystem")
	}
	if got := Env().GetRevision(); got != 0 {
		t.Fatalf("unset revision = %d, want 0", got)
	}
}

// --- isolation (§3·5) -------------------------------------------------------

// isolated builds a manifest whose filesystem carries an isolation claim.
func isolated(revision uint32, iso ilcv1.Isolation) *ilcv1.SetWorldManifestRequest {
	req := manifest(revision, ilcv1.Availability_AVAILABILITY_PRESENT)
	req.WorldManifest.Filesystem.Isolation = iso
	return req
}

// The conservative default, and the reason this field could be added without
// touching a single existing host: silence is not a promise of privacy.
func TestUnstatedIsolationReadsAsShared(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	if Isolated() {
		t.Fatal("an unset manifest claims isolation")
	}
	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if Isolated() {
		t.Fatal("a manifest that says nothing about isolation claims it")
	}
}

// An explicit SHARED is a claim a host CAN make, and it must read the same as
// silence — the difference is diagnostic ("did the host think about this?"),
// never behavioural.
func TestExplicitSharedIsNotIsolated(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, isolated(1, ilcv1.Isolation_ISOLATION_SHARED))
	if Isolated() {
		t.Fatal("SHARED reported as isolated")
	}
	if got := Env().GetFilesystem().GetIsolation(); got != ilcv1.Isolation_ISOLATION_SHARED {
		t.Fatalf("isolation = %v, want SHARED preserved on the wire", got)
	}
}

func TestPerUserIsIsolated(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, isolated(1, ilcv1.Isolation_ISOLATION_PER_USER))
	if !Isolated() {
		t.Fatal("PER_USER reported as not isolated")
	}
}

// Isolation can change mid-session for the same reason a filesystem can: a host
// may re-grant a narrower root, and an app holding private data has to notice.
func TestIsolationCanBeWithdrawn(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, isolated(1, ilcv1.Isolation_ISOLATION_PER_USER))
	send(t, isolated(2, ilcv1.Isolation_ISOLATION_SHARED))
	if Isolated() {
		t.Fatal("isolation survived a manifest that withdrew it")
	}
}

func TestSetWorldManifestRoundTrips(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	res := send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if !res.Success {
		t.Fatalf("set-environment failed: %s", res.Err)
	}
	if !HasFilesystem() {
		t.Fatal("filesystem reported absent after the host said PRESENT")
	}
	if got := Env().GetFilesystem().GetKind(); got != ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR {
		t.Fatalf("kind = %v, want APP_DIR", got)
	}
	if got := Env().GetRevision(); got != 1 {
		t.Fatalf("revision = %d, want 1", got)
	}
}

// A revision of zero is a host that forgot, and it must not be readable as "the
// first one" — every later re-send would then look unchanged and be skipped,
// which is precisely the silent staleness this field exists to prevent.
func TestZeroRevisionIsRejected(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	res := send(t, manifest(0, ilcv1.Availability_AVAILABILITY_PRESENT))
	if res.Success {
		t.Fatal("revision 0 was accepted")
	}
	if env != nil {
		t.Fatal("a rejected manifest was installed anyway")
	}
}

// A manifest REPLACES rather than merges: a host that stops reporting a
// capability is saying it is gone, not declining to mention it.
func TestSecondManifestReplaces(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_ABSENT))

	if HasFilesystem() {
		t.Fatal("filesystem still reported present after the host said ABSENT")
	}
}

// Re-sending the revision already in force must do NOTHING. The stake is not a
// wasted assignment: SetWorldManifest re-runs capability registration, so
// re-applying would rebuild the command surface underneath a host that only
// repeated itself.
func TestUnchangedRevisionDoesNotReapply(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	first := send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	var firstResp ilcv1.SetWorldManifestResponse
	if err := firstResp.UnmarshalVT(first.Output); err != nil {
		t.Fatal(err)
	}
	if !firstResp.Applied {
		t.Fatal("the first manifest reported applied=false")
	}

	// Same revision, contradictory facts: proof that the revision alone decides,
	// so a host cannot smuggle a change past the check by keeping the number.
	second := send(t, manifest(1, ilcv1.Availability_AVAILABILITY_ABSENT))
	var secondResp ilcv1.SetWorldManifestResponse
	if err := secondResp.UnmarshalVT(second.Output); err != nil {
		t.Fatal(err)
	}
	if secondResp.Applied {
		t.Fatal("an unchanged revision reported applied=true")
	}
	if !HasFilesystem() {
		t.Fatal("an unchanged revision changed the facts in force")
	}
}

// RegisterCore registers the block that must exist before the host has spoken —
// and nothing else. SetWorldManifest being in it is the whole reason the core
// block exists: it is the command that decides what else gets registered, so it
// cannot itself be waiting on a decision.
func TestRegisterCoreRegistersOnlyLifecycleVerbs(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	for _, method := range []uint32{MethodVersion, MethodSetWorldManifest} {
		if _, ok := registry[method]; !ok {
			t.Fatalf("core method %d not registered", method)
		}
	}
	for _, method := range []uint32{MethodExportFs, MethodImportFs, MethodResetFs} {
		if _, ok := registry[method]; ok {
			t.Fatalf("filesystem method %d registered by RegisterCore", method)
		}
	}
}

// The two-phase registry (plan D7): an app that discovers gets a filesystem
// surface only once a host says there is a filesystem.
func TestDiscoveredRegistrationFollowsTheManifest(t *testing.T) {
	cleanEnv(t)
	RegisterDiscovered()

	if _, ok := registry[MethodExportFs]; ok {
		t.Fatal("export-fs registered before any manifest arrived")
	}
	// Until the host speaks, the verb is not merely unregistered — it is
	// undispatchable, which is what makes the ordering rule in §2.5 a
	// correctness requirement rather than a style note.
	if res := Execute(MethodExportFs, nil); res.Success {
		t.Fatal("export-fs dispatched before any manifest arrived")
	}

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if _, ok := registry[MethodExportFs]; !ok {
		t.Fatal("export-fs not registered after the host reported a filesystem")
	}

	// And back off again: a browser can lose an OPFS handle mid-session as
	// easily as it can fail to get one at startup.
	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_ABSENT))
	if _, ok := registry[MethodExportFs]; ok {
		t.Fatal("export-fs still registered after the filesystem went away")
	}
}

// Re-syncing must be idempotent. Registering an id twice panics by design
// (Register guards against two commands claiming one wire number), so a
// capability block that is re-synced on every manifest has to skip what is
// already there rather than re-add it.
func TestResyncingACapabilityDoesNotPanic(t *testing.T) {
	cleanEnv(t)
	RegisterDiscovered()

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_PRESENT))

	if _, ok := registry[MethodExportFs]; !ok {
		t.Fatal("export-fs lost across a re-sync")
	}
}

// RegisterAll stays eager — the right choice for an app whose hosts always have
// a filesystem, and the reason adding discovery did not change what every
// existing app and host does.
func TestRegisterAllIsUnaffectedByTheManifest(t *testing.T) {
	cleanEnv(t)
	RegisterAll()

	if _, ok := registry[MethodExportFs]; !ok {
		t.Fatal("RegisterAll did not register export-fs")
	}
	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_ABSENT))
	if _, ok := registry[MethodExportFs]; !ok {
		t.Fatal("RegisterAll's surface shrank when a manifest reported no filesystem")
	}
}

// --- volatile facts (ENVIRONMENT-PLAN phase 4) ------------------------------

// recordEvents installs a sink and returns the topics it saw.
func recordEvents(t *testing.T) *[]string {
	t.Helper()
	var seen []string
	SetEventSink(func(topic string, _ []byte) { seen = append(seen, topic) })
	t.Cleanup(func() { SetEventSink(nil) })
	return &seen
}

// The host that sent the manifest already knows it changed. This is for
// everyone else — an inspector, a capability-dependent control — who did not
// make the call and has no other way to find out.
func TestAppliedManifestAnnouncesItself(t *testing.T) {
	cleanEnv(t)
	RegisterCore()
	seen := recordEvents(t)

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if len(*seen) != 1 || (*seen)[0] != "ilc.world-manifest-changed" {
		t.Fatalf("topics = %v, want one ilc.world-manifest-changed", *seen)
	}
}

// A re-send of the revision already in force changes nothing, so announcing it
// would have every listener re-read for no reason — and on a host that re-sends
// defensively, do so constantly.
func TestUnchangedManifestIsSilent(t *testing.T) {
	cleanEnv(t)
	RegisterCore()
	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	seen := recordEvents(t)

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_ABSENT))
	if len(*seen) != 0 {
		t.Fatalf("an unchanged manifest emitted %v", *seen)
	}
}

// A rejected manifest must not announce a change it did not make.
func TestRejectedManifestIsSilent(t *testing.T) {
	cleanEnv(t)
	RegisterCore()
	seen := recordEvents(t)

	send(t, manifest(0, ilcv1.Availability_AVAILABILITY_PRESENT))
	if len(*seen) != 0 {
		t.Fatalf("a rejected manifest emitted %v", *seen)
	}
}

// The event arrives AFTER the surface is in line with the facts. A listener
// that hears it and immediately asks what is registered must get the NEW
// answer; emitting first would race every listener against the registration
// being announced.
func TestSurfaceIsSettledBeforeTheAnnouncement(t *testing.T) {
	cleanEnv(t)
	RegisterDiscovered()
	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))

	var registeredWhenHeard bool
	SetEventSink(func(topic string, _ []byte) {
		if topic == "ilc.world-manifest-changed" {
			_, registeredWhenHeard = registry[MethodExportFs]
		}
	})
	t.Cleanup(func() { SetEventSink(nil) })

	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_ABSENT))
	if registeredWhenHeard {
		t.Fatal("export-fs was still registered when the change was announced")
	}
}
