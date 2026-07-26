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
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/devalbo/devalbo-ilc/wit"
)

// runBuild implements `dlc build <tier> [--out dir] [--web-out dir] [--entry pkg]`.
func runBuild(args []string) error {
	tier := "web"
	// TWO destinations, because the two artifacts have different audiences:
	//
	//   component  build/engine.component.wasm   a build artifact — also the
	//              parity and interchange artifact; nothing serves it
	//   web assets frontend/src/wasm/            jco's loader FETCHES the core
	//              .wasm at runtime, so these must sit inside the web root or a
	//              dev server will not serve them
	//
	// One --out for both would force every project to copy files after building
	// (which is what the template used to do). The defaults match the layout
	// `dlc new` emits; a project manifest (§16.8) is where these become
	// declared rather than assumed.
	out := "build"
	webOut := filepath.Join("frontend", "src", "wasm")
	entry := "./cmd/engine-component"

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		tier, args = args[0], args[1:]
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("build: --out needs a directory")
			}
			out, i = args[i+1], i+1
		case "--web-out":
			if i+1 >= len(args) {
				return fmt.Errorf("build: --web-out needs a directory")
			}
			webOut, i = args[i+1], i+1
		case "--entry":
			if i+1 >= len(args) {
				return fmt.Errorf("build: --entry needs a package path")
			}
			entry, i = args[i+1], i+1
		default:
			return fmt.Errorf("build: unknown flag %q", args[i])
		}
	}

	switch tier {
	case "web":
		return buildWeb(out, webOut, entry)
	case "native":
		// Deliberately not implemented: a native build is `go build`, and
		// wrapping it would add a layer that hides a perfectly good error
		// message. Named here so the refusal is specific.
		return fmt.Errorf("build: tier %q needs no dlc — use `go build ./hosts/native`", tier)
	default:
		return fmt.Errorf("build: unknown tier %q (have: web)", tier)
	}
}

func buildWeb(out, webOut, entry string) error {
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

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	component := filepath.Join(out, "engine.component.wasm")

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
	if err := run("jco", "transpile", component, "-o", webOut); err != nil {
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
