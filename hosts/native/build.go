package main

// `dlc build <tier>` — a TOOLCHAIN verb (Decision 30), so it lives host-side and
// never crosses into the engine. It spawns processes and inspects the machine;
// both are things the engine must never do, because the engine also runs in a
// browser tab.
//
// What it does for the `web` tier is supply the WIT world. An app does not own
// the world — `dlc` does (see wit/wit.go) — so this materializes the embedded
// copy into a temp directory, points TinyGo at it, and transpiles the result
// with jco. A generated project therefore carries no `wit/` and cannot be
// stranded on a stale world.

import (
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"

	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devalbo/devalbo-ilc/dlc-platform/wit"
	"github.com/devalbo/devalbo-ilc/engine"
)

// runBuild implements `dlc build <tier> [--out dir] [--web-out dir] [--entry pkg]`.
//
// Precedence: built-in defaults, then dlc.toml's [tiers.<tier>], then flags.
// The manifest is what the project DECLARES; flags are one-off overrides. Either
// alone should work, and neither should require editing the other.
func runBuild(request []byte) error {
	// Parsed by the generated surface from toolchain.proto — no hand-rolled flag
	// loop, no usage string, and `--help` comes from the field comments. The
	// positional default ("web") and `--entry`'s default are declared there too.
	var req dlcv1.BuildRequest
	if err := req.UnmarshalVT(request); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	tier := req.GetTier()

	// STILL SEPARATE VARIABLES, because the precedence below depends on knowing
	// whether the user said anything: out/web_out carry no schema default, so an
	// empty string means "not given" and the manifest gets its turn.
	outFlag, webOutFlag := req.GetOut(), req.GetWebOut()
	entry := req.GetEntry()

	// The manifest decides which tiers exist. Building one the project does not
	// declare is worth naming — otherwise `dlc build web` in a CLI-only project
	// silently produces a web tier nobody asked for.
	m, err := loadManifest()
	if err != nil {
		return err
	}
	declared, ok := m.Tiers[tier]
	if !ok {
		return fmt.Errorf("build: this project declares no [tiers.%s] in %s (has: %s)",
			tier, manifestFile, strings.Join(tierNames(m), ", "))
	}

	// TWO destinations, because the two artifacts have different audiences:
	//
	//   component  a build artifact — also the parity and interchange artifact;
	//              nothing serves it
	//   web assets jco's loader FETCHES the core .wasm at run time, so these must
	//              sit inside the web root or a dev server will not serve them
	// The assets default is derived from the tier's SLOT, not hard-coded: the
	// slot is where this tier's host code lives, and jco's output has to be
	// servable from inside it. A literal here would silently disagree with a
	// project that put its slot somewhere else.
	out := firstNonEmpty(outFlag, declared.Component, "build/engine.component.wasm")
	webOut := firstNonEmpty(webOutFlag, declared.Assets, filepath.Join(declared.Root, "src", "wasm"))

	// ROUTE ON THE TARGET, NOT THE TIER NAME. A tier is a host slot and there
	// will be one per chip; a target is an artifact and there are four. Switching
	// on the name would mean editing this function for every board, and the
	// second board would produce the same bytes as the first.
	target, known := engine.TierTarget(tier)
	if !known {
		// Declared in dlc.toml but not in the engine's landscape. The manifest is
		// the project's, the landscape is the platform's, and a name in one and
		// not the other is worth saying out loud rather than defaulting.
		return fmt.Errorf("build: tier %q is in %s but not in the platform's tier landscape", tier, manifestFile)
	}

	switch target {
	case engine.TargetWasip2:
		return buildWeb(out, webOut, entry)
	case engine.TargetPulley32, engine.TargetPulley64:
		return buildEmbedded(out, firstNonEmpty(declared.Cwasm, defaultCwasm(target)), entry, target)
	case engine.TargetNative:
		// Deliberately not implemented: a native build is `go build`, and
		// wrapping it would add a layer that hides a perfectly good error
		// message. Named here so the refusal is specific.
		return fmt.Errorf("build: tier %q needs no dlc — use `go build ./hosts/native`", tier)
	default:
		return fmt.Errorf("build: tier %q is declared but dlc cannot build it yet", tier)
	}
}

func defaultCwasm(target string) string {
	return filepath.Join("build", "engine."+target+".cwasm")
}

// buildEmbedded: the component FIRST, then AOT. Two steps, one verb.
//
// THE COMPONENT IS NOT REBUILT PER BOARD. This calls exactly the same TinyGo
// invocation the web tier uses and then compiles that artifact ahead of time —
// which is what makes "the badge runs the same component the browser runs" a
// property of the build rather than a claim in a document. If this function ever
// grows a second `tinygo build`, the embedded plan has failed its own constraint.
func buildEmbedded(component, cwasm, entry, target string) error {
	if err := buildComponent(component, entry); err != nil {
		return err
	}

	// `dlc-precompile`, NOT `wasmtime compile`. A .cwasm records the FEATURE SET
	// OF THE COMPILER THAT PRODUCED IT — not the flags it was given — so a stock
	// wasmtime CLI emits artifacts the no_std runtime rejects with "compilation
	// settings are not compatible". The precompile crate is built with the same
	// feature set as the firmware, which makes that mismatch impossible rather
	// than merely fixed.
	crate, err := precompileCrate()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		return fmt.Errorf("build %s: cargo not found on PATH — run inside `devbox shell`", target)
	}
	if err := os.MkdirAll(filepath.Dir(cwasm), 0o755); err != nil {
		return err
	}
	abs := func(p string) string {
		a, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return a
	}

	fmt.Fprintln(os.Stderr, "build "+target+": precompile -> "+cwasm)
	if err := run("cargo", "run", "--quiet",
		"--manifest-path", filepath.Join(crate, "Cargo.toml"),
		"--", abs(component), abs(cwasm), target); err != nil {
		return fmt.Errorf("build %s: precompile: %w", target, err)
	}
	fmt.Fprintln(os.Stderr, "build "+target+": ok")
	return nil
}

// precompileCrate locates the AOT compiler, which is Rust and therefore cannot
// be embedded in this binary the way the WIT world is.
//
// A SCAFFOLDED PROJECT DOES NOT HAVE IT, and that is the honest state today: the
// crate lives in this repository, so `dlc build <embedded tier>` works here and
// needs an explicit path elsewhere. Saying so beats a "no such file" naming a
// path the user never chose.
func precompileCrate() (string, error) {
	if env := os.Getenv("DLC_PRECOMPILE"); env != "" {
		return env, nil
	}
	const inRepo = "dlc-platform/embedded/precompile"
	if _, err := os.Stat(filepath.Join(inRepo, "Cargo.toml")); err == nil {
		return inRepo, nil
	}
	return "", fmt.Errorf("build: the AOT compiler is not here — set DLC_PRECOMPILE to a checkout of %s.\n"+
		"It is a Rust crate, so unlike the WIT world it cannot ship inside this binary", inRepo)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// tierNames lists what the project actually declares, for the error above.
func tierNames(m *Manifest) []string {
	names := make([]string, 0, len(m.Tiers))
	for name := range m.Tiers {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return []string{"(none)"}
	}
	return names
}

// buildWeb: `component` is the wasm FILE path (not a directory) — the manifest
// names a file, so the flag and the default do too. Treating it as a directory
// produced `build/engine.component.wasm/engine.component.wasm`, which is the
// kind of thing that looks fine in a log until someone reads it.
func buildWeb(component, webOut, entry string) error {
	if _, err := exec.LookPath("jco"); err != nil {
		return fmt.Errorf("build web: jco not found on PATH — run inside `devbox shell`")
	}
	if err := buildComponent(component, entry); err != nil {
		return err
	}

	// Transpile straight into the web root — no post-build copy step, and no
	// chance of serving a stale copy someone forgot to refresh.
	if err := os.MkdirAll(filepath.Dir(webOut), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(webOut); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "build web: jco transpile -> "+webOut)
	// MAP the custom capability imports to real modules. jco turns a WIT import
	// into a bare specifier — `import { emit } from 'devalbo:ilc/events'` — which
	// no bundler can resolve on its own; the browser reports only "Failed to fetch
	// dynamically imported module", naming the component rather than the import.
	//
	// Mapped to the PACKAGE path, not a relative file: the sink belongs to the ILC
	// web host, so apps get fixes on a version bump instead of carrying a copy.
	// Every capability added later needs a line here.
	if err := run("jco", "transpile", component, "-o", webOut,
		"--map", "devalbo:ilc/events=@devalbo/dlc-web/events"); err != nil {
		return fmt.Errorf("build web: jco: %w", err)
	}
	fmt.Fprintln(os.Stderr, "build web: ok")
	return nil
}

// buildComponent produces `engine.component.wasm` — THE artifact, shared by
// every tier that runs wasm. Factored out of buildWeb when the embedded tier
// landed, because both need it and neither may have its own copy: two call sites
// with two TinyGo invocations is exactly how "one artifact everywhere" would
// quietly stop being true.
func buildComponent(component, entry string) error {
	if _, err := exec.LookPath("tinygo"); err != nil {
		return fmt.Errorf("build: tinygo not found on PATH — run inside `devbox shell`")
	}
	witDir, err := materializeWIT()
	if err != nil {
		return err
	}
	defer os.RemoveAll(witDir)

	if err := os.MkdirAll(filepath.Dir(component), 0o755); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "build: tinygo -> "+component)
	if err := run("tinygo", "build", "-target=wasip2",
		"--wit-package", witDir, "--wit-world", "engine",
		"-o", component, entry); err != nil {
		return fmt.Errorf("build: tinygo: %w", err)
	}
	return nil
}

// materializeWIT writes the embedded world to a temp directory for TinyGo, which
// wants a real path. The caller removes it.
func materializeWIT() (string, error) {
	dir, err := os.MkdirTemp("", "dlc-wit-")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(wit.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		content, err := wit.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, content, 0o644)
	})
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("materialize wit: %w", err)
	}
	return dir, nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr // build chatter is not program output
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
