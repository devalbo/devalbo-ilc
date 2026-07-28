package main

// The slot gate (Decision 34). Until now nothing in dlc.toml was load-bearing —
// `capabilities` had one writer and zero readers, `root` was parsed and never
// read — so this is the first field whose absence or staleness stops a build,
// and the first test that can watch it happen.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inProject chdirs into a temp project with the given dlc.toml and slots, and
// returns. os.Chdir rather than t.Chdir: the module is on go 1.23, where t.Chdir
// does not exist yet.
func inProject(t *testing.T, manifest string, slots ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, s := range slots {
		if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

const bothTiers = `
[project]
name = "app"

[tiers.native]
capabilities = ["console"]
root = "hosts/native"

[tiers.web]
capabilities = ["console"]
root = "hosts/web"
assets = "hosts/web/src/wasm"
`

func TestSlotsPresent(t *testing.T) {
	inProject(t, bothTiers, "hosts/native", "hosts/web")

	m, err := loadManifest()
	if err != nil {
		t.Fatalf("a manifest whose slots all exist must load: %v", err)
	}
	if got := m.Tiers["web"].Root; got != "hosts/web" {
		t.Errorf("web slot: got %q, want hosts/web", got)
	}
	if got := m.Tiers["native"].Root; got != "hosts/native" {
		t.Errorf("native slot: got %q, want hosts/native", got)
	}
}

// The falsification this gate exists for: a slot that MOVED. A stale `root` is
// what a rename looks like from here, and it used to cost nothing at all — the
// field was parsed and dropped.
func TestSlotMissingIsRefused(t *testing.T) {
	inProject(t, bothTiers, "hosts/native") // web slot deliberately absent

	_, err := loadManifest()
	if err == nil {
		t.Fatal("a tier whose slot does not exist must not load")
	}
	// The error has to name the tier AND the path, because the two plausible
	// causes — wrong path, or a directory nobody created — need different fixes.
	if !strings.Contains(err.Error(), "tiers.web") || !strings.Contains(err.Error(), "hosts/web") {
		t.Errorf("error should name the tier and its root: %v", err)
	}
}

func TestSlotUndeclaredIsRefused(t *testing.T) {
	inProject(t, `
[project]
name = "app"

[tiers.native]
capabilities = ["console"]
`, "hosts/native")

	_, err := loadManifest()
	if err == nil {
		t.Fatal("a tier with no root at all must not load")
	}
	// The message suggests the conventional path, because "no root" is nearly
	// always a file someone wrote by hand rather than a deliberate omission.
	if !strings.Contains(err.Error(), `hosts/native`) {
		t.Errorf("error should suggest the conventional slot: %v", err)
	}
}

func TestSlotMustBeADirectory(t *testing.T) {
	inProject(t, bothTiers, "hosts/native")
	if err := os.WriteFile(filepath.Join("hosts", "web"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadManifest()
	if err == nil {
		t.Fatal("a slot that is a file must not load")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should say what is wrong: %v", err)
	}
}

// A project with no tiers is not a slot problem — it is a different (and legal)
// shape, and the gate must not invent an error for it. This pins the bug the
// first draft had: tierNames() substitutes a "(none)" placeholder for an empty
// map, and iterating THAT reported a missing slot for a tier nobody declared.
func TestNoTiersIsNotASlotError(t *testing.T) {
	inProject(t, "[project]\nname = \"app\"\n")

	if _, err := loadManifest(); err != nil {
		t.Errorf("a project declaring no tiers must still load: %v", err)
	}
}
