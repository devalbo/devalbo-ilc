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

// `dlc`'s OWN method-id block, carved out of the framework range (AGENTS.md §1).
//
// Declared here because it is dlc's band, not the platform's — `dlc-platform`
// has no business knowing this tool exists. The upper bound is
// `platform.AppMethodBase` and is never retyped: the boundary lives in one place,
// which is the lesson ids_test.go already records about a message that disagreed
// with the constant beside it.
//
// Moved out of the app band on 2026-07-29 (see commands.proto for why). 10000+ is
// the app's alone now.
const (
	// DlcMethodBase is the first id `dlc`'s engine-served verbs may claim.
	DlcMethodBase uint32 = 9000
	// DlcHostLocalBase is the first id `dlc`'s host-local verbs may claim
	// (Decision 30) — the ones the engine never dispatches.
	DlcHostLocalBase uint32 = 9100
)

// Tier names. Referenced rather than spelled, so a rename is one edit and a typo
// is a compile error.
const (
	TierNative = "native" // terminal / CLI: engine linked in-process (Decision 26)
	TierWeb    = "web"    // browser: wasip2 component under jco

	TierDesktop = "desktop" // Wails webview over the native host binding

	// Embedded tiers are named for the CHIP, not the board: the HAL, the boot
	// block and the memory map are chip-level, and only a handful of values
	// (crystal, pins, flash size) are the board's.
	TierRP2350       = "rp2350"        // RP2350 (Tufty 2350): pulley32 under Wasmtime no_std, Rust host
	TierRP2350TinyGo = "rp2350-tinygo" // RP2350: TinyGo linked directly — no wasm, the measured fallback
	TierRP2040TinyGo = "rp2040-tinygo" // RP2040: TinyGo only; 2 MB flash cannot hold a wasm runtime + payload
	TierESP32P4      = "esp32p4"       // ESP32-P4 (RISC-V + PSRAM): pulley32, the same artifact as rp2350
)

// Build targets — what an ARTIFACT is compiled for.
//
// NOT the same axis as a tier, and conflating them is the mistake `dlc build
// web` almost taught us. A tier is a slot: host code, a display, buttons. A
// target is an artifact. Web and native happen to be 1:1, so the difference
// never showed — but embedded breaks it, because **Pulley bytecode is
// ISA-independent**. One `pulley32` artifact runs on the RP2350's Cortex-M33,
// on its Hazard3 RISC-V cores, and on an ESP32-P4. Many tiers, one artifact.
//
// So there are exactly two embedded targets and there always will be: pointer
// width is the only thing the artifact can differ on.
const (
	TargetNative   = "native"   // linked in-process; there is no artifact (Decision 26)
	TargetWasip2   = "wasip2"   // the component the browser runs, via jco
	TargetPulley32 = "pulley32" // every 32-bit embedded chip, and the QEMU harness
	TargetPulley64 = "pulley64" // 64-bit dev machines: the laptop harness and the parity column
)

// TargetSpec is what `dlc build` needs to produce an artifact.
//
// The PROFILE is part of the target, not a flag someone remembers. A `.cwasm`
// records the compiler's settings, so "pulley32" alone does not determine
// whether a runtime can load it — `pulley32 + no-CoW + no-signals` does. That
// distinction cost an afternoon of "compilation settings are not compatible
// with the native host" before it was written down here.
type TargetSpec struct {
	Name string
	What string
	// NoStdProfile marks targets whose runtime has no virtual memory and no host
	// signal handlers, so the compiler must be told the same.
	NoStdProfile bool
}

// TargetLandscape is every artifact shape this project produces.
var TargetLandscape = []TargetSpec{
	{Name: TargetNative, What: "no artifact — the engine is a linked Go package"},
	{Name: TargetWasip2, What: "wasip2 component; the web tier's artifact"},
	{Name: TargetPulley32, What: "Pulley bytecode for 32-bit runtimes (every embedded chip so far)", NoStdProfile: true},
	{Name: TargetPulley64, What: "Pulley bytecode for 64-bit runtimes (dev harness, parity column)", NoStdProfile: true},
}

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
	// Target is the ARTIFACT this tier consumes. Several tiers share one — every
	// 32-bit embedded chip runs the same `pulley32` bytecode — which is why this
	// is a field rather than a rename of Name.
	Target string
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
		Target:  TargetNative,
		What:    "terminal CLI; the engine is linked in-process (Decision 26)",
		Comment: "This tier's HOST code — native input in, a proto request out.",
	},
	{
		Name:      TierWeb,
		Status:    TierAvailable,
		Target:    TargetWasip2,
		What:      "browser; the engine is a wasip2 component under jco",
		Comment:   "This tier's HOST code, and where `dlc build web` writes into it.\n# The assets MUST sit inside the slot: jco's loader fetches the core\n# .wasm at run time, and a dev server will not serve a path outside\n# its root.",
		Assets:    "hosts/web/src/wasm",
		Component: "build/engine.component.wasm",
	},
	{
		Name:   TierDesktop,
		Status: TierPlanned,
		Target: TargetNative,
		What:   "Wails webview over the native binding (§5.4, §10 of the Go plan)",
	},
	{
		Name:   TierRP2350,
		Status: TierPlanned,
		Target: TargetPulley32,
		What:   "RP2350 (Tufty 2350); the SAME component the browser runs, AOT-compiled to Pulley (docs/EMBEDDED-PLAN.md)",
	},
	{
		Name:   TierESP32P4,
		Status: TierPlanned,
		Target: TargetPulley32,
		What:   "ESP32-P4 (RISC-V, PSRAM); the same pulley32 artifact as rp2350 — Xtensa parts are out of scope (D8)",
	},
	{
		Name:   TierRP2350TinyGo,
		Status: TierPlanned,
		Target: TargetNative,
		What:   "RP2350 without a wasm runtime; TinyGo compiles the engine natively — measured at a 263 KB .uf2, the fallback if Pulley will not fit",
	},
	{
		Name:   TierRP2040TinyGo,
		Status: TierPlanned,
		Target: TargetNative,
		What:   "RP2040; TinyGo only — 2 MB of flash cannot hold a wasm runtime plus a ~1 MB payload",
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

// TierTarget reports which artifact a tier consumes — the mapping `dlc build`
// routes on. Exported because the builder is HOST-side (Decision 30) and the
// tier landscape is engine-side; without this the host would keep its own copy
// of the table and the two would drift.
//
// MANY TIERS SHARE ONE TARGET, which is the whole point: `rp2350` and `esp32p4`
// both answer `pulley32`, because Pulley bytecode is ISA-independent and one
// artifact serves both boards.
func TierTarget(tier string) (string, bool) {
	spec, ok := tierSpec(tier)
	if !ok {
		return "", false
	}
	return spec.Target, true
}

// slotRoot is where a tier's host code lives — `hosts/<tier>` unless the row
// overrides it. Decision 34: `hosts/<tier>/` IS the tier slot.
func (t TierSpec) slotRoot() string {
	if t.Root != "" {
		return t.Root
	}
	return "hosts/" + t.Name
}
