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

	"github.com/devalbo/dlc-platform/wit"
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

	switch tier {
	case "web":
		return buildWeb(out, webOut, entry)
	case "native":
		// Deliberately not implemented: a native build is `go build`, and
		// wrapping it would add a layer that hides a perfectly good error
		// message. Named here so the refusal is specific.
		return fmt.Errorf("build: tier %q needs no dlc — use `go build ./hosts/native`", tier)
	default:
		return fmt.Errorf("build: tier %q is declared but dlc cannot build it yet", tier)
	}
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
	for _, tool := range []string{"tinygo", "jco"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("build web: %s not found on PATH — run inside `devbox shell`", tool)
		}
	}

	witDir, err := materializeWIT()
	if err != nil {
		return err
	}
	defer os.RemoveAll(witDir)

	if err := os.MkdirAll(filepath.Dir(component), 0o755); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "build web: tinygo -> "+component)
	if err := run("tinygo", "build", "-target=wasip2",
		"--wit-package", witDir, "--wit-world", "engine",
		"-o", component, entry); err != nil {
		return fmt.Errorf("build web: tinygo: %w", err)
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
