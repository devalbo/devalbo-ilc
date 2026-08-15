package platform

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// TestNameVectors holds THIS implementation to the shared specification.
//
// The Rust host has a test that reads the same file and asserts the same rows
// (dlc-platform/embedded/src/names.rs). Neither implementation is the authority;
// the file is. That is what makes "consistent across all worlds" checkable rather
// than aspirational — two validators that agree today drift silently, and the
// drift surfaces as an app that writes a name one tier can read and another
// mangles.
func TestNameVectors(t *testing.T) {
	file, err := os.Open("names/VECTORS.tsv")
	if err != nil {
		t.Fatalf("the shared vectors are the spec; without them this proves nothing: %v", err)
	}
	defer file.Close()

	checked := 0
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		// A blank line is a separator; the empty-path case is a row whose path
		// field is empty, which still has a tab.
		if !strings.Contains(text, "\t") {
			continue
		}

		path, want, _ := strings.Cut(text, "\t")
		want = strings.TrimSpace(want)

		err := CheckNamePath(path)
		if want == "ok" {
			if err != nil {
				t.Errorf("line %d: %q should be portable, got %v", line, path, err)
			}
			checked++
			continue
		}

		if err == nil {
			t.Errorf("line %d: %q should be refused as %s, was accepted", line, path, want)
			checked++
			continue
		}
		got := string(err.(*NameError).Kind)
		if got != want {
			t.Errorf("line %d: %q refused as %s, want %s", line, path, got, want)
		}
		checked++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	// A vectors file that silently stopped being read would make this test pass
	// while checking nothing — the same failure the parity self-test guards
	// against by asserting its diff BY NAME.
	if checked < 30 {
		t.Fatalf("only %d vectors checked; the file should carry many more", checked)
	}
}

// TestNamePortabilityIsNotContainment records the boundary between the two, which
// is the thing most likely to be conflated: SafeJoin refuses an ATTACK, this
// refuses a PORTABILITY bug, and an app wants both.
func TestNamePortabilityIsNotContainment(t *testing.T) {
	// Legal to this profile, and still something SafeJoin must contain.
	if err := CheckNamePath("saves/game1.json"); err != nil {
		t.Fatalf("ordinary relative path refused: %v", err)
	}
	// Refused here for traversal, which SafeJoin would also refuse — the overlap
	// is real but partial.
	if CheckNamePath("a/../b") == nil {
		t.Fatal("traversal should be refused")
	}
}

// TestWorldProfilesAreTheSpecialCase pins what each profile gives up, so that
// "narrower profile" stays a documented trade rather than a loophole.
func TestWorldProfilesAreTheSpecialCase(t *testing.T) {
	// CASE IS THE MOTIVATING AXIS, and it does not split the way people expect:
	// Windows and FAT are case-insensitive, Linux is case-sensitive, macOS is
	// case-insensitive by default but can be either. So the portable profile
	// forbids uppercase outright; POSIX is what an app picks when it knows it is
	// only ever on one host.
	if CheckNamePath("Save.json") == nil {
		t.Fatal("portable profile must refuse uppercase")
	}
	if err := CheckNamePathForProfile(ProfilePosix, "Save.json"); err != nil {
		t.Fatalf("posix profile should allow uppercase: %v", err)
	}

	// FAT is the binding constraint, so it agrees with portable today. Named
	// separately so the two can diverge later without re-reading every caller.
	if CheckNamePathForProfile(ProfileFat, "Save.json") == nil {
		t.Fatal("fat profile must refuse uppercase")
	}

	// STRUCTURE IS NOT NEGOTIABLE. Traversal, absolute paths and empty components
	// are correctness rather than portability, and no profile relaxes them —
	// otherwise "pick a narrower world" would become a way to opt out of
	// containment.
	for _, path := range []string{"a/../b", "/etc/passwd", "a//b", ""} {
		if CheckNamePathForProfile(ProfilePosix, path) == nil {
			t.Errorf("posix profile must still refuse %q", path)
		}
	}

	// An unset profile is the portable one: the default must be the safe default.
	if CheckNamePathForProfile("", "Save.json") == nil {
		t.Fatal("empty profile must fall back to portable")
	}
}

// TestWorldRegistry holds the Go vocabulary to the shared file, and pins the
// distinction that is easiest to collapse: undefined is ABSENCE, unknown is
// INCOMPREHENSION.
func TestWorldRegistry(t *testing.T) {
	file, err := os.Open("names/WORLDS.tsv")
	if err != nil {
		t.Fatalf("the registry is the spec: %v", err)
	}
	defer file.Close()

	seen := map[World]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") || !strings.Contains(text, "\t") {
			continue
		}
		id, rest, _ := strings.Cut(text, "\t")
		profile, _, _ := strings.Cut(rest, "\t")
		world := World(id)
		seen[world] = true

		// Every id in the file must be understood — a row this build does not
		// know means the registry moved and this implementation did not.
		if !world.IsRealWorld() && world != WorldUndefined && world != WorldUnknown {
			t.Errorf("registry has %q, which this build does not know", id)
		}
		if got := string(world.NameProfile()); got != strings.TrimSpace(profile) {
			t.Errorf("%s: profile %s, registry says %s", id, got, profile)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	// ...and every world this build knows must be in the file, so the check
	// cannot pass by the registry being empty.
	for _, world := range knownWorlds {
		if !seen[world] {
			t.Errorf("%s is not in the registry", world)
		}
	}
	if !seen[WorldUndefined] || !seen[WorldUnknown] {
		t.Error("the registry must carry both non-worlds")
	}
}

func TestUndefinedAndUnknownAreDifferent(t *testing.T) {
	// Absence: nobody said anything. The common case, not an error.
	if got := ParseWorld(""); got != WorldUndefined {
		t.Errorf("empty id should be undefined, got %s", got)
	}
	// Incomprehension: something was said and could not be understood. This is
	// what an older host sees from a payload built against a newer registry.
	if got := ParseWorld("badge-holographic"); got != WorldUnknown {
		t.Errorf("unrecognised id should be unknown, got %s", got)
	}
	if WorldUndefined == WorldUnknown {
		t.Fatal("the two non-worlds must not be the same value")
	}
	// Neither is a real world, and both FAIL CLOSED onto the strictest profile.
	for _, world := range []World{WorldUndefined, WorldUnknown} {
		if world.IsRealWorld() {
			t.Errorf("%s should not be a real world", world)
		}
		if world.NameProfile() != ProfilePortable {
			t.Errorf("%s must fail closed onto the portable profile", world)
		}
	}
}

// TestCollisionVectors holds this implementation to names/COLLISIONS.tsv, which
// the Rust twin also reads.
func TestCollisionVectors(t *testing.T) {
	file, err := os.Open("names/COLLISIONS.tsv")
	if err != nil {
		t.Fatalf("the shared vectors are the spec: %v", err)
	}
	defer file.Close()

	checked := 0
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") || !strings.Contains(text, "\t") {
			continue
		}
		names, want, _ := strings.Cut(text, "\t")
		want = strings.TrimSpace(want)
		set := strings.Fields(names)

		err := CheckNameSet(set)
		if want == "ok" {
			if err != nil {
				t.Errorf("line %d: %v should not collide, got %v", line, set, err)
			}
			checked++
			continue
		}
		if err == nil {
			t.Errorf("line %d: %v should collide as %s, was accepted", line, set, want)
			checked++
			continue
		}
		if got := string(err.(*CollisionError).Kind); got != want {
			t.Errorf("line %d: %v collided as %s, want %s", line, set, got, want)
		}
		checked++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("only %d collision vectors checked", checked)
	}
}

// TestCollisionNamesBothEntries pins that the error identifies the pair, because
// a caller printing "invalid" gives a user nothing to act on.
func TestCollisionNamesBothEntries(t *testing.T) {
	err := CheckNameSet([]string{"alpha", "board-state-1", "board-state-2"})
	if err == nil {
		t.Fatal("expected a collision")
	}
	c := err.(*CollisionError)
	if c.A != "board-state-1" || c.B != "board-state-2" {
		t.Errorf("wrong pair named: %s and %s", c.A, c.B)
	}
	if !strings.Contains(err.Error(), "board-state-1") || !strings.Contains(err.Error(), "board-state-2") {
		t.Errorf("message must name both: %q", err.Error())
	}
}
