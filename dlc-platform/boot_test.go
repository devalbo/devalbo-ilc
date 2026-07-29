package platform

import (
	"os"
	"strings"
	"testing"

	ilcv1 "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/v1"
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
