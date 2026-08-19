package platform

import (
	"strings"
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// The proto's WorldName enum and names/WORLDS.tsv name the same worlds.
//
// # Why this is derived and not a list
//
// It WAS a list — a hand-written map from Go constant to proto value — and it
// was the third place a world had to be added. Adding the Badger needed an edit
// in the registry, an edit in the .proto, and an edit here, which is precisely
// the arrangement that let the first draft of the enum ship with no name for
// `browser` at all.
//
// Both sides are now generated from the registry, so this asserts the RULE that
// relates them rather than restating their contents: a world id in kebab-case is
// the enum value in SCREAMING_SNAKE with the enum's prefix. A world added to the
// registry appears on both sides and needs nothing here.
func TestProtoWorldsMatchNames(t *testing.T) {
	for _, world := range knownWorlds {
		want := "WORLD_NAME_" + strings.ToUpper(strings.ReplaceAll(string(world), "-", "_"))
		value, ok := ilcv1.WorldName_value[want]
		if !ok {
			t.Errorf("%s is in the registry with no %s in the proto", world, want)
			continue
		}
		if value == 0 {
			t.Errorf("%s maps to the zero value, which means 'nobody said'", world)
		}
	}

	// And the reverse: a proto value with no world is a name only one half of
	// the system knows.
	for name := range ilcv1.WorldName_value {
		switch name {
		// The two that describe the ABSENCE of a world rather than a world, and
		// so are not rows in the registry's list of real slots.
		case "WORLD_NAME_UNSPECIFIED", "WORLD_NAME_UNKNOWN":
			continue
		}
		id := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(name, "WORLD_NAME_"), "_", "-"))
		if !World(id).IsRealWorld() {
			t.Errorf("proto has %s with no world %q in names/WORLDS.tsv", name, id)
		}
	}
}

// The two spellings of "nobody declared one" mean the same thing.
//
// proto's linter requires the zero value to be `_UNSPECIFIED`; the registry was
// written with `undefined`. This is what stops them drifting into two ideas.
func TestUnspecifiedWorldIsUndefined(t *testing.T) {
	if got := CurrentWorld(); got != WorldUndefined {
		t.Errorf("with no manifest sent, CurrentWorld() = %q, want %q", got, WorldUndefined)
	}
}
