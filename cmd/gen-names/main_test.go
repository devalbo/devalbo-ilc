package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T) (rules, []world, string) {
	t.Helper()
	root := findRoot()
	return loadRules(filepath.Join(root, "dlc-platform/names/RULES.json")),
		loadWorlds(filepath.Join(root, "dlc-platform/names/WORLDS.tsv")),
		root
}

// TestCheckedInFilesAreCurrent is the staleness guard AS A TEST, so `go test
// ./...` catches it and not only `make verify-names`.
//
// THIS IS THE WHOLE POINT OF THE CODEGEN. Without it, someone edits a generated
// file by hand, the Go and Rust validators diverge again, and the single source
// has quietly become a third copy — which is the exact failure the generator was
// built to make impossible.
func TestCheckedInFilesAreCurrent(t *testing.T) {
	spec, worlds, root := load(t)

	for path, want := range map[string][]byte{
		filepath.Join(root, "dlc-platform/names_gen.go"):              renderGo(spec, worlds),
		filepath.Join(root, "dlc-platform/embedded/src/names_gen.rs"): renderRust(spec, worlds),
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale — run `make gen-names`", mustRel(root, path))
		}
	}
}

// TestOutputIsDeterministic pins what would otherwise be a maddening false
// alarm: Go map iteration is randomised, so a generator that ranged over a map
// directly would emit different bytes each run and the staleness check would
// fail at random — reading as "somebody edited a generated file" when nobody
// did. `sortedKeys` exists for this, and this test is why it must stay.
func TestOutputIsDeterministic(t *testing.T) {
	spec, worlds, _ := load(t)
	for i := 0; i < 20; i++ {
		if !bytes.Equal(renderGo(spec, worlds), renderGo(spec, worlds)) {
			t.Fatal("Go output differs between renders")
		}
		if !bytes.Equal(renderRust(spec, worlds), renderRust(spec, worlds)) {
			t.Fatal("Rust output differs between renders")
		}
	}
}

// TestSpecChangeReachesBothLanguages proves the SINGLE SOURCING rather than
// assuming it. A generator that silently emitted only one language would pass
// every other test here.
func TestSpecChangeReachesBothLanguages(t *testing.T) {
	spec, worlds, _ := load(t)

	// A reserved name nobody has: if it appears in both outputs, the spec really
	// is driving both.
	spec.ReservedDevices = append(spec.ReservedDevices, "zzdevice")
	spec.Limits["portable"] = limits{Component: 41, Path: 43}

	for name, out := range map[string]string{
		"Go":   string(renderGo(spec, worlds)),
		"Rust": string(renderRust(spec, worlds)),
	} {
		if !strings.Contains(out, "zzdevice") {
			t.Errorf("%s output did not pick up the new reserved device", name)
		}
		if !strings.Contains(out, "41") || !strings.Contains(out, "43") {
			t.Errorf("%s output did not pick up the new limits", name)
		}
	}
}

// TestNonWorldsAreNotEmittedAsKnownWorlds guards a distinction the rest of the
// system leans on: `undefined` and `unknown` are what a lookup RETURNS, not
// registry entries. Emitting them would make `IsRealWorld` true for both, and
// "no world was declared" would start reading as a real host slot.
func TestNonWorldsAreNotEmittedAsKnownWorlds(t *testing.T) {
	_, worlds, _ := load(t)
	for _, w := range worlds {
		if w.ID == "undefined" || w.ID == "unknown" {
			t.Errorf("%q is a non-world and must not be emitted as a known world", w.ID)
		}
	}
	if len(worlds) < 4 {
		t.Fatalf("expected the real worlds to survive the filter, got %d", len(worlds))
	}
}

// TestGeneratedCodeIsSyntacticallyBalanced would have caught a bug this
// generator actually shipped: `world_name_profile` opened an `if` per world and
// closed only one, so the Rust file had an unclosed delimiter and the whole
// crate failed to build. Cheap structural check, real bug.
func TestGeneratedCodeIsSyntacticallyBalanced(t *testing.T) {
	spec, worlds, _ := load(t)
	for name, out := range map[string]string{
		"Go":   string(renderGo(spec, worlds)),
		"Rust": string(renderRust(spec, worlds)),
	} {
		for _, pair := range []struct{ open, close rune }{{'{', '}'}, {'(', ')'}, {'[', ']'}} {
			depth := 0
			inString := false
			for i, c := range out {
				// Skip string literals: a brace inside `"        "` is not code.
				if c == '"' && (i == 0 || out[i-1] != '\\') {
					inString = !inString
				}
				if inString {
					continue
				}
				switch c {
				case pair.open:
					depth++
				case pair.close:
					depth--
				}
				if depth < 0 {
					t.Fatalf("%s: unbalanced %c%c — closed before opening", name, pair.open, pair.close)
				}
			}
			if depth != 0 {
				t.Errorf("%s: unbalanced %c%c — %d unclosed", name, pair.open, pair.close, depth)
			}
		}
	}
}

// TestGeneratedFilesCarryTheDoNotEditBanner — the only thing standing between a
// generated file and someone editing it in an editor that gave no warning.
func TestGeneratedFilesCarryTheDoNotEditBanner(t *testing.T) {
	spec, worlds, _ := load(t)
	for name, out := range map[string]string{
		"Go":   string(renderGo(spec, worlds)),
		"Rust": string(renderRust(spec, worlds)),
	} {
		if !strings.Contains(out, "DO NOT EDIT") {
			t.Errorf("%s output is missing the generated-file banner", name)
		}
	}
}

// TestEveryProfileInTheSpecIsEmitted stops a profile being added to RULES.json
// and silently never reaching the generated limit tables — where it would fall
// through to the portable default and look like it worked.
func TestEveryProfileInTheSpecIsEmitted(t *testing.T) {
	spec, worlds, _ := load(t)
	goOut, rustOut := string(renderGo(spec, worlds)), string(renderRust(spec, worlds))
	for profile := range spec.Limits {
		if !strings.Contains(goOut, `"`+profile+`"`) {
			t.Errorf("Go output never mentions profile %q", profile)
		}
		if !strings.Contains(rustOut, `"`+profile+`"`) {
			t.Errorf("Rust output never mentions profile %q", profile)
		}
	}
}
