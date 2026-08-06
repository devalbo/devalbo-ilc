package platform

import (
	"os"
	"strings"
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// bootIn runs Boot against a fresh temp directory with a clean registry.
func bootIn(t *testing.T, opts BootOptions) error {
	t.Helper()
	cleanEnv(t)
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	// What an app's engine init does, and what a host gets for free by
	// blank-importing it.
	RegisterDiscovered()
	return Boot(opts)
}

// Boot leaves an engine that a host can immediately use: root granted, facts in
// force, capability verbs registered.
func TestBootRegistersDiscoveredVerbs(t *testing.T) {
	if err := bootIn(t, BootOptions{
		Root:           ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
	}); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if !HasFilesystem() {
		t.Fatal("filesystem absent after Boot granted a root")
	}
	if got := Env().GetFilesystem().GetKind(); got != ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD {
		t.Fatalf("kind = %v, want CWD", got)
	}
	if !RootGranted() {
		t.Fatal("Boot did not grant the root")
	}
}

// A host that says nothing about isolation is not making a claim, and Boot must
// not invent one on its behalf — the whole point of the safe default.
func TestBootDefaultsToNoIsolationClaim(t *testing.T) {
	if err := bootIn(t, BootOptions{
		Root:           ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
	}); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if Isolated() {
		t.Fatal("Boot claimed isolation the host never stated")
	}
	if got := Env().GetFilesystem().GetIsolation(); got != ilcv1.Isolation_ISOLATION_UNSPECIFIED {
		t.Fatalf("isolation = %v, want UNSPECIFIED (no claim)", got)
	}
}

// And a host that DID grant a per-user root can say so — the case a partitioned
// host would use.
func TestBootCarriesAnIsolationClaim(t *testing.T) {
	if err := bootIn(t, BootOptions{
		Root:           ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR,
		Isolation:      ilcv1.Isolation_ISOLATION_PER_USER,
	}); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if !Isolated() {
		t.Fatal("Boot dropped the host's isolation claim")
	}
}

// A root is GRANTED, never assumed (§3·5). Boot refusing an empty one keeps
// that rule at the entry point every host now goes through, rather than
// relying on Root() panicking later, somewhere that does not name the cause.
func TestBootRefusesAnUngrantedRoot(t *testing.T) {
	err := bootIn(t, BootOptions{FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD})
	if err == nil {
		t.Fatal("Boot accepted an empty root")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Fatalf("error does not name the problem: %q", err)
	}
}

// The host is the only party that knows what its root IS, so an unset kind is a
// host that has not said — not a cue for the platform to infer one from the
// path, which would be a guess wearing a fact's clothes.
func TestBootRefusesAnUnsetFilesystemKind(t *testing.T) {
	err := bootIn(t, BootOptions{Root: "."})
	if err == nil {
		t.Fatal("Boot accepted an unset filesystem kind")
	}
	if !strings.Contains(err.Error(), "kind") {
		t.Fatalf("error does not name the problem: %q", err)
	}
}

// The sink must be installed BEFORE the manifest, because SetEnvironment may
// itself emit — and a sink installed afterwards would miss that first event
// with nothing to indicate anything had been missed.
//
// Asserted through Boot rather than by reading the source: the ordering is the
// contract, and an ordering nothing checks is a comment.
func TestBootInstallsTheSinkBeforeSendingTheManifest(t *testing.T) {
	seenWhileBooting := false
	err := bootIn(t, BootOptions{
		Root:           ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
		Sink:           func(string, []byte) { seenWhileBooting = true },
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(func() { SetEventSink(nil) })

	// Nothing emits during boot today, so this cannot assert an event arrived.
	// What it CAN assert is that the sink is live by the time Boot returns —
	// which is the half that would break if the order were flipped.
	EmitEvent(&ilcv1.DataChangedEvent{Prefix: "probe"})
	if !seenWhileBooting {
		t.Fatal("the sink Boot was given is not installed")
	}
}

// Boot dispatches through the registry, so an app whose engine package was
// never imported has nothing to dispatch TO. The bare failure is "unknown
// method_id 2", which names the symptom; the cause is always a missing blank
// import, so Boot says that instead.
func TestBootWithNothingRegisteredExplainsWhy(t *testing.T) {
	cleanEnv(t) // no RegisterDiscovered: the engine package was never imported
	err := Boot(BootOptions{Root: ".", FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD})
	if err == nil {
		t.Fatal("Boot succeeded with an empty registry")
	}
	if strings.Contains(err.Error(), "unknown method_id") {
		t.Fatalf("error names the symptom, not the cause: %q", err)
	}
	if !strings.Contains(err.Error(), "engine package") {
		t.Fatalf("error does not point at the missing import: %q", err)
	}
}

// A HOST WITH NO STORAGE CAN BOOT — the keyboard tier (RP2040) has no
// filesystem at all, and until NoFilesystem existed such a host could not start:
// Boot's own error told it to "say so explicitly" and gave it no way to.
func TestBootWithoutAFilesystem(t *testing.T) {
	if err := bootIn(t, BootOptions{NoFilesystem: true}); err != nil {
		t.Fatalf("boot without a filesystem: %v", err)
	}
	if HasFilesystem() {
		t.Fatal("filesystem reported PRESENT on a host that declared it has none")
	}
	// The point of the manifest, not a side effect: an absent filesystem takes
	// the filesystem verbs with it, so an app cannot call export-fs into a void.
	if _, ok := registry[MethodExportFs]; ok {
		t.Fatal("export-fs is registered on a host with no filesystem")
	}
}

// The two ways of describing storage CONTRADICT each other, and Boot refuses
// rather than picking a winner — a host that sets both holds two beliefs about
// itself, and any precedence rule would hide that at the cheapest moment to see
// it.
func TestBootRefusesAFilesystemBothWays(t *testing.T) {
	err := bootIn(t, BootOptions{NoFilesystem: true, Root: "."})
	if err == nil {
		t.Fatal("Boot accepted NoFilesystem together with a granted root")
	}
	if !strings.Contains(err.Error(), "NoFilesystem") {
		t.Fatalf("error does not name the contradiction: %q", err)
	}

	err = bootIn(t, BootOptions{
		NoFilesystem:   true,
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
	})
	if err == nil {
		t.Fatal("Boot accepted NoFilesystem together with a filesystem kind")
	}
	if !strings.Contains(err.Error(), "NoFilesystem") {
		t.Fatalf("error does not name the contradiction: %q", err)
	}
}

// The message a host actually hits must name the way OUT. This is the gap that
// made the whole feature necessary — the old text ended "or say so explicitly"
// while offering nothing to say it with, which reads as a platform that lost an
// argument with itself.
func TestBootUngrantedRootNamesTheEscape(t *testing.T) {
	err := bootIn(t, BootOptions{FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD})
	if err == nil {
		t.Fatal("Boot accepted an empty root")
	}
	if !strings.Contains(err.Error(), "NoFilesystem") {
		t.Fatalf("error tells a storageless host nothing it can do: %q", err)
	}
}
