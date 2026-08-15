package platform

// The PORTABLE NAME PROFILE, for apps and for hosts alike.
//
// WHY IT IS HERE. An app writes a name on one tier and a person reads it on
// another: `saves/game1.json` persisted on a badge, opened over USB on Windows.
// Every layer in that path has its own rules, and the failure mode is not an
// error — it is a file that quietly has a different name than the one asked for.
// That happened: a payload called `hello.pulley32` mounted as `HELLO.PU.CWA`,
// because an 8.3 directory entry has no dot. Nothing failed; the name was just
// wrong.
//
// THE RULES LIVE IN names/VECTORS.tsv, not in this file. Apps are Go, the badge
// host is Rust, the browser host is JS — no library spans those, so the shared
// thing has to be a specification with implementations checked against it. This
// is one implementation; `dlc-platform/embedded/src/names.rs` is another, and
// both have tests that read every row of that file. Add a rule there first.
//
// WHAT THIS IS NOT. It is not path containment — that is SafeJoin, which stops a
// path escaping its root. This is about whether a name MEANS THE SAME THING on
// every tier. An app wants both, and they fail differently: SafeJoin refuses an
// attack, this refuses a portability bug.
//
// TinyGo-safe by construction: no regexp, no reflection, no allocations beyond
// the error string.

// NameErrorKind names which rule was broken. Carried rather than collapsed into
// a bool because the rule is what makes the profile learnable — an app author
// should see which constraint and why, not "invalid path".
type NameErrorKind string

const (
	NameEmpty             NameErrorKind = "empty"
	NameTooLong           NameErrorKind = "too-long"
	NameIllegalCharacter  NameErrorKind = "illegal-character"
	NameReservedTraversal NameErrorKind = "reserved-traversal"
	NameReservedDevice    NameErrorKind = "reserved-device"
	NameBadEdge           NameErrorKind = "bad-edge"
	NameBadStructure      NameErrorKind = "bad-structure"
)

// THE RULES ARE GENERATED, the reasons are here. `cmd/gen-names` emits
// `names_gen.go` and its Rust twin from `names/RULES.json`, so an app's validator
// and the badge host's cannot disagree about a character class, a limit, or a
// reserved name. Before codegen they were written twice and VECTORS.tsv existed
// to DETECT the drift; now the drift cannot occur, and the vectors check the
// remaining question — whether the rules mean what was intended.
//
// NameComponentMax, NamePathMax, ShortStemLength, portableNameChar,
// shortNameChar, badEdgeFirst, badEdgeLast and reservedDevices all live in
// names_gen.go. Edit names/RULES.json, then `make gen-names`.

// NameError reports a name that would not survive every tier.
type NameError struct {
	Path string
	Kind NameErrorKind
}

func (e *NameError) Error() string {
	return "name " + e.Path + " is not portable: " + nameReason(e.Kind)
}

func nameReason(kind NameErrorKind) string {
	switch kind {
	case NameEmpty:
		return "empty"
	case NameTooLong:
		return "too long"
	case NameIllegalCharacter:
		return "illegal character (use lowercase a-z, 0-9, '-', '_', '.')"
	case NameReservedTraversal:
		return "'.' and '..' are reserved"
	case NameReservedDevice:
		return "reserved device name on Windows"
	case NameBadEdge:
		return "must not start with '.' or '-', or end with '.' or '-'"
	case NameBadStructure:
		return "must be relative, '/'-separated, with no empty components"
	}
	return string(kind)
}

// CheckNameComponent validates one path component — a file or directory name,
// with no separators.
func CheckNameComponent(name string) error {
	if name == "" {
		return &NameError{Path: name, Kind: NameEmpty}
	}
	if len(name) > NameComponentMax {
		return &NameError{Path: name, Kind: NameTooLong}
	}
	if name == "." || name == ".." {
		return &NameError{Path: name, Kind: NameReservedTraversal}
	}

	for i := 0; i < len(name); i++ {
		c := name[i]
		// UPPERCASE IS ILLEGAL ON PURPOSE. FAT and Windows are case-insensitive
		// and POSIX is not, so allowing both cases lets an app create two files
		// that become one on the badge. "Use lowercase" is a rule an app can
		// follow; "never create two names differing only in case" is one it
		// cannot check locally.
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.'
		if !ok {
			return &NameError{Path: name, Kind: NameIllegalCharacter}
		}
	}

	// Edges. A leading dot is hidden on POSIX and has no 8.3 form; a trailing dot
	// is silently STRIPPED by Windows, which is the same class of bug as the 8.3
	// mangling this profile exists to prevent.
	if badEdgeFirst(name[0]) || badEdgeLast(name[len(name)-1]) {
		return &NameError{Path: name, Kind: NameBadEdge}
	}

	// Windows matches a device name against the stem, so `con.txt` is refused too.
	stem := name
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			stem = name[:i]
			break
		}
	}
	for _, device := range reservedDevices {
		if stem == device {
			return &NameError{Path: name, Kind: NameReservedDevice}
		}
	}

	return nil
}

// CheckNamePath validates a whole relative path: '/'-separated, no leading slash.
//
// RELATIVE ONLY. A tier decides where an app's files live — a directory on a
// laptop, a FAT subdirectory on the badge — so an absolute path is a claim the
// app is not entitled to make. Same reasoning as SafeJoin, which enforces the
// containment this only describes.
func CheckNamePath(path string) error {
	if path == "" {
		return &NameError{Path: path, Kind: NameEmpty}
	}
	if len(path) > NamePathMax {
		return &NameError{Path: path, Kind: NameTooLong}
	}
	// A leading '/' is absolute. A '\' is a separator on Windows and a legal
	// filename character on POSIX, so the same string names different things on
	// different tiers — which is the whole problem.
	if path[0] == '/' {
		return &NameError{Path: path, Kind: NameBadStructure}
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' {
			return &NameError{Path: path, Kind: NameBadStructure}
		}
	}

	start := 0
	for i := 0; i <= len(path); i++ {
		if i < len(path) && path[i] != '/' {
			continue
		}
		component := path[start:i]
		// '//' and a trailing '/' both produce an empty component.
		if component == "" {
			return &NameError{Path: path, Kind: NameBadStructure}
		}
		if err := CheckNameComponent(component); err != nil {
			// Report against the whole path: that is what the caller passed.
			return &NameError{Path: path, Kind: err.(*NameError).Kind}
		}
		start = i + 1
	}
	return nil
}

// IsPortableName reports whether a path is one every tier can carry unchanged.
func IsPortableName(path string) bool {
	return CheckNamePath(path) == nil
}

// ---------------------------------------------------------------------------
// World-specific profiles — THE SPECIAL CASE, and it costs something.
// ---------------------------------------------------------------------------
//
// Everything above is the PORTABLE profile: the intersection of what every tier
// accepts, and what an app gets by default. This section exists because an app
// may legitimately declare it targets fewer worlds — a browser-only tool has no
// reason to obey FAT's rules — and refusing to model that would just push apps
// into writing their own validators, which is how the drift starts.
//
// **But it is deliberately not the easy path.** Choosing a profile other than
// Portable is a statement that the app will not run somewhere, and the project's
// core constraint is that business logic runs everywhere. So the narrower
// profiles are opt-in, named for what they give up, and an app that reaches for
// one should be able to say why.
//
// CASE IS THE AXIS THAT MOTIVATES THIS, and it does not split the way people
// expect. Windows and FAT are case-INSENSITIVE; Linux is case-SENSITIVE; macOS
// is case-insensitive BY DEFAULT on APFS but can be formatted either way. So
// "works on my Mac" proves nothing about Linux, and the portable profile forbids
// uppercase outright rather than trying to reason about which host it is on.
// ProfilePosix is what an app picks when it knows it is only ever on one.

// NameProfile selects which world's rules to check against.
type NameProfile string

const (
	// ProfilePortable is the default and the intersection: a name that survives
	// every tier unchanged. Use this unless there is a reason not to.
	ProfilePortable NameProfile = "portable"

	// ProfilePosix relaxes case and allows a wider character set. Valid for
	// CLI-only and browser-only apps; a name accepted here may be MANGLED or
	// COLLIDE on a badge, which is the specific thing being given up.
	ProfilePosix NameProfile = "posix"

	// ProfileFat is the badge's own rules, and is currently identical to
	// ProfilePortable — because the badge IS the binding constraint. It exists as
	// a separate name so that the day FAT gains long-filename support here, the
	// two can diverge without every caller having to be re-read.
	ProfileFat NameProfile = "fat"
)

// ComponentMax is the longest single component this profile allows.
//
// THE LIMIT IS PER-WORLD because the worlds genuinely differ: NTFS and ext4 both
// allow 255-byte components, and FAT with long filenames allows 255 too but the
// badge's synthetic view is 8.3. The PORTABLE limit is deliberately far below
// all of them — 64 is what stays readable in a 320x240 file list, and a name an
// app can rely on everywhere is worth more than a long one.
//
// An app that asks for a specific world gets that world's limit, which is the
// point of asking.
func (p NameProfile) ComponentMax() int {
	return profileComponentMax(p)
}

// PathMax is the longest whole path this profile allows.
func (p NameProfile) PathMax() int {
	return profilePathMax(p)
}

// CheckNamePathForProfile validates against a specific world's rules.
//
// Prefer CheckNamePath. Reaching for this means accepting that the name may not
// survive every tier — see the section comment above for what each profile gives
// up.
func CheckNamePathForProfile(profile NameProfile, path string) error {
	switch profile {
	case ProfilePosix:
		return checkPosixPath(path)
	case ProfileFat, ProfilePortable, "":
		return CheckNamePath(path)
	}
	return &NameError{Path: path, Kind: NameBadStructure}
}

// checkPosixPath allows what POSIX allows and this profile still considers sane:
// mixed case, and that is the whole difference. Structure, traversal and the
// empty-component rules are NOT relaxed — those are correctness, not portability,
// and an app does not get to opt out of them.
func checkPosixPath(path string) error {
	if path == "" {
		return &NameError{Path: path, Kind: NameEmpty}
	}
	if len(path) > ProfilePosix.PathMax() {
		return &NameError{Path: path, Kind: NameTooLong}
	}
	if path[0] == '/' {
		return &NameError{Path: path, Kind: NameBadStructure}
	}

	start := 0
	for i := 0; i <= len(path); i++ {
		if i < len(path) && path[i] != '/' {
			continue
		}
		component := path[start:i]
		if component == "" {
			return &NameError{Path: path, Kind: NameBadStructure}
		}
		if component == "." || component == ".." {
			return &NameError{Path: path, Kind: NameReservedTraversal}
		}
		if len(component) > ProfilePosix.ComponentMax() {
			return &NameError{Path: path, Kind: NameTooLong}
		}
		// NUL is the one byte POSIX itself forbids in a filename.
		for j := 0; j < len(component); j++ {
			if component[j] == 0 {
				return &NameError{Path: path, Kind: NameIllegalCharacter}
			}
		}
		start = i + 1
	}
	return nil
}

// ---------------------------------------------------------------------------
// The world registry — a shared vocabulary, including the two non-worlds.
// ---------------------------------------------------------------------------
//
// A WORLD is a host slot an app can run in; a PROFILE is a filesystem rule set.
// Not the same axis: several worlds share the portable profile, which is exactly
// why one profile is worth having. The canonical list is names/WORLDS.tsv, and
// Go, Rust and JS are each tested against it.

// World names a host slot.
type World string

const (
	// WorldUndefined is the DEFAULT: nobody declared a world. An app that runs
	// everywhere has no reason to name one, so this is the common case rather
	// than an error, and it resolves to the strictest profile.
	WorldUndefined World = "undefined"

	// WorldUnknown is a world that WAS declared and this build does not
	// recognise — an app or payload built against a newer registry meeting an
	// older host. The badge makes that routine: payloads outlive the firmware
	// that reads them.
	//
	// Distinct from WorldUndefined on purpose. Absence and incomprehension are
	// different facts, and only one of them means somebody meant something.
	WorldUnknown World = "unknown"

	WorldNative       World = "native"
	WorldBrowser      World = "browser"
	WorldBadgeNormal  World = "badge-normal"
	WorldBadgeMinimal World = "badge-minimal"
)

// ParseWorld maps a declared id onto the registry.
//
// An unrecognised id becomes WorldUnknown rather than an error, because the
// caller has to decide what to do about it and that decision differs: a host
// merely REPORTING which world it is can carry on, while a host ASKED for a
// world it does not know must refuse. Collapsing both into an error would take
// that choice away.
func ParseWorld(id string) World {
	if id == "" {
		return WorldUndefined
	}
	for _, world := range knownWorlds {
		if World(id) == world {
			return world
		}
	}
	if World(id) == WorldUndefined {
		return WorldUndefined
	}
	return WorldUnknown
}

// IsRealWorld reports whether this names an actual host slot, as opposed to the
// absence of one (WorldUndefined) or an unrecognised one (WorldUnknown).
func (w World) IsRealWorld() bool {
	for _, world := range knownWorlds {
		if w == world {
			return true
		}
	}
	return false
}

// NameProfile returns the filesystem rules this world imposes.
//
// FAILS CLOSED: both non-worlds get the portable profile, which is the strictest
// one available. An unrecognised world must never be quietly given a laxer rule
// set than the app was written against — that is how an app loses output on a
// tier nobody tested.
func (w World) NameProfile() NameProfile {
	// WorldUndefined, WorldUnknown and anything not in the registry fall through
	// to the portable profile inside the generated table.
	return worldNameProfile(w)
}

// CheckNamePathForWorld validates a path against a world's rules.
func CheckNamePathForWorld(world World, path string) error {
	return CheckNamePathForProfile(world.NameProfile(), path)
}

// ---------------------------------------------------------------------------
// SET-LEVEL validation: collisions no per-name check can see
// ---------------------------------------------------------------------------
//
// Every rule above judges ONE name. A whole class of bug is invisible from
// there: two names that are each perfectly legal and still cannot coexist. The
// host does not report it — one file becomes the other, or shadows it in a
// listing, and somebody eventually wonders where their save went.
//
// Rules live in names/COLLISIONS.tsv; both implementations are tested against it.

// CollisionKind says how two names collide.
type CollisionKind string

const (
	// CollisionCaseFold: identical once case is folded. Two files on Linux, one
	// on FAT, Windows, and a default-formatted macOS. Cannot arise under the
	// portable profile, which forbids uppercase — it is the trade an app makes by
	// declaring the narrower posix world.
	CollisionCaseFold CollisionKind = "case-fold"

	// CollisionShortName: identical once truncated to a FAT 8.3 short name.
	// LIVE rather than hypothetical — the badge's USB volume renders a name as
	// its first eight characters.
	CollisionShortName CollisionKind = "short-name"
)

// CollisionError names BOTH offending entries. "Invalid" is not actionable;
// "these two collide" is.
type CollisionError struct {
	A, B string
	Kind CollisionKind
}

func (e *CollisionError) Error() string {
	why := "collide once truncated to a FAT 8.3 short name"
	if e.Kind == CollisionCaseFold {
		why = "differ only in case, and are one file on a case-insensitive host"
	}
	return "names " + e.A + " and " + e.B + " " + why
}

// ShortStem83 reduces a name to the 8-character stem a FAT short name carries.
//
// THE SHARED TRUTH FOR TRUNCATION. The badge's fatview renders directory entries
// with the Rust twin of this function, and CheckNameSet predicts collisions with
// it, so a warning here describes what the badge actually does. A validator that
// reimplemented the rule would check something nothing does, which is worse than
// not checking.
func ShortStem83(name string) string {
	stem := []byte("        ")
	for i := 0; i < len(name) && i < ShortStemLength; i++ {
		stem[i] = shortNameChar(name[i])
	}
	return string(stem)
}

// foldsTogether reports whether two names are the same file on a
// case-insensitive host.
func foldsTogether(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x = x - 'A' + 'a'
		}
		if y >= 'A' && y <= 'Z' {
			y = y - 'A' + 'a'
		}
		if x != y {
			return false
		}
	}
	return true
}

// CheckNameSet checks a set of names for pairs that cannot coexist.
//
// O(n^2) deliberately: a catalog holds 16 entries and an app's directory holds
// tens, so a map would cost an allocation on a target that has none, for a
// saving nobody can measure.
func CheckNameSet(names []string) error {
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			// Case first: it is the stronger statement about the same pair.
			if foldsTogether(names[i], names[j]) {
				return &CollisionError{A: names[i], B: names[j], Kind: CollisionCaseFold}
			}
			if ShortStem83(names[i]) == ShortStem83(names[j]) {
				return &CollisionError{A: names[i], B: names[j], Kind: CollisionShortName}
			}
		}
	}
	return nil
}
