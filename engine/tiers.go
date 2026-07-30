package engine

// THE TIER LANDSCAPE — every tier this project knows about, named in one place.
//
// A tier is a COMPOSITION RECIPE (Decision 27): the shared engine × a host
// binding × an ABI mode × a capability set. Not a fork of the logic, and not a
// board — two runtimes on one board are two tiers, and two boards running the
// same binding are one.
//
// WHY A DECLARED LIST, when `supportedTiers` derives the buildable ones from the
// template tree. Derivation alone left no place that NAMES a tier: a typo'd
// directory (`hosts/webb/`) would silently become a tier, nothing tied the
// scaffolder's vocabulary to `dlc.toml`'s or the docs', and there was no constant
// to reference from Go. Derivation alone also cannot express a tier that is
// designed but unbuilt, which is most of them.
//
// So both, and they check each other (see the tests): this table names the
// landscape, and the template decides which entries can actually be emitted
// today. That is the same shape as `reserved_method_id` — declare it so the name
// is claimed and cannot be quietly reused, without pretending it works.
//
// NOT A CONSTRAINT ON PROJECTS. `dlc.toml` accepts any `[tiers.*]` name with a
// slot directory that exists (Decision 27 puts the per-project registry there,
// and `hosts/native/manifest.go` enforces the slot). This table is what **`dlc`
// can scaffold**, which is a smaller claim than what a project may declare.
//
// EXPECT THIS TO BE REFACTORED. It exists first to populate the landscape; a
// proto enum is the likelier long-term home (it would generate constants for Go
// and TypeScript, and Decision 29 turns an enum into menu choices host-side),
// but that changes `NewRequest.tiers` from `repeated string` and is a wire change
// worth making deliberately rather than in passing.

// Tier names. Referenced rather than spelled, so a rename is one edit and a typo
// is a compile error.
const (
	TierNative = "native" // terminal / CLI: engine linked in-process (Decision 26)
	TierWeb    = "web"    // browser: wasip2 component under jco

	TierDesktop = "desktop" // Wails webview over the native host binding

	TierBadgeNative = "badge-native" // RP2350 (Tufty): TinyGo linked directly, Go host
	TierBadgeWAMR   = "badge-wamr"   // RP2350 (Tufty): wasip1 core wasm under WAMR, C++ host
	TierKeebNative  = "keeb-native"  // RP2040 (KB2040): TinyGo linked directly, no display
	TierESP32WAMR   = "esp32-wamr"   // ESP32-S3: wasip1 core wasm under WAMR, ESP-IDF host
)

// TierStatus separates "you can scaffold this" from "we have named it".
type TierStatus int

const (
	// TierAvailable means the template has a slot and `dlc new` will emit it.
	TierAvailable TierStatus = iota
	// TierPlanned means designed and named, with no skeleton yet. Requesting one
	// is refused — but refused by NAME, which is a better error than "unknown".
	TierPlanned
)

// TierSpec is one row of the landscape. Lean on purpose: the fields that exist
// are the ones something reads.
type TierSpec struct {
	Name   string
	Status TierStatus
	// What this tier is, in one line — the source for docs and help text.
	What string
	// Comment written above the tier's `dlc.toml` section. A generated manifest
	// explains itself; that is why these are prose and not a format string.
	Comment string
	// Root is the slot directory. Empty means the default, `hosts/<name>`.
	Root string
	// Assets and Component are the web tier's extra manifest keys. Empty for
	// every other tier, which is why `tierSections` defaults rather than switches.
	Assets    string
	Component string
}

// TierLandscape is the whole list, in the canonical order tiers are written in
// `dlc.toml` and offered in help.
//
// Adding a tier: add a row here, and a `templates/component-model/hosts/<name>/`
// directory when its shape is known. Until the directory exists it stays
// TierPlanned and nothing can scaffold it.
var TierLandscape = []TierSpec{
	{
		Name:    TierNative,
		Status:  TierAvailable,
		What:    "terminal CLI; the engine is linked in-process (Decision 26)",
		Comment: "This tier's HOST code — native input in, a proto request out.",
	},
	{
		Name:      TierWeb,
		Status:    TierAvailable,
		What:      "browser; the engine is a wasip2 component under jco",
		Comment:   "This tier's HOST code, and where `dlc build web` writes into it.\n# The assets MUST sit inside the slot: jco's loader fetches the core\n# .wasm at run time, and a dev server will not serve a path outside\n# its root.",
		Assets:    "hosts/web/src/wasm",
		Component: "build/engine.component.wasm",
	},
	{
		Name:   TierDesktop,
		Status: TierPlanned,
		What:   "Wails webview over the native binding (§5.4, §10 of the Go plan)",
	},
	{
		Name:   TierBadgeNative,
		Status: TierPlanned,
		What:   "RP2350 Tufty badge; TinyGo linked directly, Go host, 320x240 TFT",
	},
	{
		Name:   TierBadgeWAMR,
		Status: TierPlanned,
		What:   "RP2350 Tufty badge; wasip1 core wasm under WAMR, C++ host",
	},
	{
		Name:   TierKeebNative,
		Status: TierPlanned,
		What:   "RP2040 KB2040; TinyGo linked directly, no display, serial or key matrix",
	},
	{
		Name:   TierESP32WAMR,
		Status: TierPlanned,
		What:   "ESP32-S3; wasip1 core wasm under WAMR, ESP-IDF host",
	},
}

// tierSpec finds a declared tier by name.
func tierSpec(name string) (TierSpec, bool) {
	for _, t := range TierLandscape {
		if t.Name == name {
			return t, true
		}
	}
	return TierSpec{}, false
}

// slotRoot is where a tier's host code lives — `hosts/<tier>` unless the row
// overrides it. Decision 34: `hosts/<tier>/` IS the tier slot.
func (t TierSpec) slotRoot() string {
	if t.Root != "" {
		return t.Root
	}
	return "hosts/" + t.Name
}
