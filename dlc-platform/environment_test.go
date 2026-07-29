// The environment manifest (§6.4a, Decision 32) — the host stating what it can
// do, and the command surface following from it.
package platform

import (
	"testing"

	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"
)

// cleanEnv isolates a test from the package-level manifest and registry. Both
// are process-global by design — an engine has exactly one environment — so
// tests that change them have to put them back.
func cleanEnv(t *testing.T) {
	t.Helper()
	prevEnv, prevDiscovered := env, discovered
	prevRegistry := map[uint32]Handler{}
	for k, v := range registry {
		prevRegistry[k] = v
	}
	t.Cleanup(func() {
		env, discovered = prevEnv, prevDiscovered
		registry = prevRegistry
	})
	resetEnvironment()
	discovered = nil
	registry = map[uint32]Handler{}
}

func manifest(revision uint32, fs ilcv1.Availability) *ilcv1.SetEnvironmentRequest {
	return &ilcv1.SetEnvironmentRequest{
		Environment: &ilcv1.Environment{
			Revision:   revision,
			Filesystem: &ilcv1.Filesystem{Availability: fs, Kind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR},
		},
	}
}

// withIndex is the same manifest plus an index availability. A separate helper
// rather than another parameter on `manifest`, because every existing test is
// about the filesystem and would gain an argument it does not care about.
func withIndex(req *ilcv1.SetEnvironmentRequest, index ilcv1.Availability) *ilcv1.SetEnvironmentRequest {
	req.Environment.Index = &ilcv1.Index{Availability: index}
	return req
}

// send dispatches SetEnvironment the way a host does — through Execute, not by
// calling the handler. Natively the engine is linked in-process and a direct
// call would work, but then the native tier would never exercise the command
// path that the wasm tier has no choice about, and parity would be comparing
// two different sequences.
func send(t *testing.T, req *ilcv1.SetEnvironmentRequest) Result {
	t.Helper()
	body, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	return Execute(MethodSetEnvironment, body)
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
	if HasIndex() {
		t.Fatal("unset manifest reports an index")
	}
	if got := Env().GetRevision(); got != 0 {
		t.Fatalf("unset revision = %d, want 0", got)
	}
}

// --- the index (INDEX-PLAN phase 1 — SLATED FOR REVERT, see that plan's D8) --

// The accessor reports what the host said, in both directions. An index can go
// away mid-session for the same reasons a filesystem can — a browser tab losing
// its storage takes both with it.
func TestIndexFollowsTheManifest(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, withIndex(manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT), ilcv1.Availability_AVAILABILITY_PRESENT))
	if !HasIndex() {
		t.Fatal("index reported absent after the host said PRESENT")
	}

	send(t, withIndex(manifest(2, ilcv1.Availability_AVAILABILITY_PRESENT), ilcv1.Availability_AVAILABILITY_ABSENT))
	if HasIndex() {
		t.Fatal("index still reported present after the host said ABSENT")
	}
}

// A manifest that mentions a filesystem and says nothing about an index reports
// no index — the same conservative default as an unset manifest, and the reason
// a host adding the index later breaks nothing that shipped before it.
func TestUnmentionedIndexReadsAsAbsent(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if HasIndex() {
		t.Fatal("a manifest with no index field reports an index")
	}
	if !HasFilesystem() {
		t.Fatal("the filesystem went missing from a manifest that granted it")
	}
}

// THE D4 RULE, asserted before there is anything to break it: an index appearing
// or vanishing must not change which commands exist. Absence of a filesystem
// removes an ability, so its verbs unregister; absence of an index removes only
// speed, so nothing may.
//
// Phase 2 adds `rebuild-index` — the one verb that IS index-dependent, because
// it has nothing to rebuild without one — and this test is what will force that
// exception to be deliberate rather than incidental.
func TestIndexDoesNotChangeTheCommandSurface(t *testing.T) {
	cleanEnv(t)
	RegisterDiscovered()

	send(t, withIndex(manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT), ilcv1.Availability_AVAILABILITY_ABSENT))
	without := surfaceIDs()

	send(t, withIndex(manifest(2, ilcv1.Availability_AVAILABILITY_PRESENT), ilcv1.Availability_AVAILABILITY_PRESENT))
	with := surfaceIDs()

	if len(with) != len(without) {
		t.Fatalf("the surface changed when an index appeared: %v -> %v", without, with)
	}
	for id := range without {
		if _, ok := with[id]; !ok {
			t.Fatalf("method %d disappeared when an index appeared", id)
		}
	}
}

func surfaceIDs() map[uint32]bool {
	ids := map[uint32]bool{}
	for id := range registry {
		ids[id] = true
	}
	return ids
}

func TestSetEnvironmentRoundTrips(t *testing.T) {
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
// wasted assignment: SetEnvironment re-runs capability registration, so
// re-applying would rebuild the command surface underneath a host that only
// repeated itself.
func TestUnchangedRevisionDoesNotReapply(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	first := send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	var firstResp ilcv1.SetEnvironmentResponse
	if err := firstResp.UnmarshalVT(first.Output); err != nil {
		t.Fatal(err)
	}
	if !firstResp.Applied {
		t.Fatal("the first manifest reported applied=false")
	}

	// Same revision, contradictory facts: proof that the revision alone decides,
	// so a host cannot smuggle a change past the check by keeping the number.
	second := send(t, manifest(1, ilcv1.Availability_AVAILABILITY_ABSENT))
	var secondResp ilcv1.SetEnvironmentResponse
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
// and nothing else. SetEnvironment being in it is the whole reason the core
// block exists: it is the command that decides what else gets registered, so it
// cannot itself be waiting on a decision.
func TestRegisterCoreRegistersOnlyLifecycleVerbs(t *testing.T) {
	cleanEnv(t)
	RegisterCore()

	for _, method := range []uint32{MethodVersion, MethodSetEnvironment} {
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
	if len(*seen) != 1 || (*seen)[0] != "ilc.environment-changed" {
		t.Fatalf("topics = %v, want one ilc.environment-changed", *seen)
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
		if topic == "ilc.environment-changed" {
			_, registeredWhenHeard = registry[MethodExportFs]
		}
	})
	t.Cleanup(func() { SetEventSink(nil) })

	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_ABSENT))
	if registeredWhenHeard {
		t.Fatal("export-fs was still registered when the change was announced")
	}
}
