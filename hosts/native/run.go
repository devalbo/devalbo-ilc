package main

// `dlc run <tier>` — launch a tier on a developer machine (Decision 30: host-side,
// because it spawns processes).
//
// WHY THIS IS NOT ONE THING. A tier is a host binding, and launching one means
// something different per binding: the native tier is a binary to exec, the web
// tier is a dev server plus a browser. So this file is a switch over tiers, and
// **the default arm refuses**.
//
// REFUSING IS THE POINT, not a gap. `dlc` can exec a binary and it can serve a
// directory; it cannot flash a board, push firmware, or attach a debugger to a
// device. A tier it cannot launch gets a message naming itself and a non-zero
// exit, because the alternative — appearing to start something — is worse than
// saying no. Same stance `build` takes on a tier it cannot build.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

func runRun(request []byte) error {
	var req dlcv1.RunRequest
	if err := req.UnmarshalVT(request); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	tier := req.GetTier()

	// The manifest decides which tiers exist. Launching one the project does not
	// declare is worth naming — the same check `build` makes, and for the same
	// reason: a typo should not silently start the wrong thing.
	m, err := loadManifest()
	if err != nil {
		return err
	}
	declared, ok := m.Tiers[tier]
	if !ok {
		return fmt.Errorf("run: this project declares no [tiers.%s] in %s (has: %s)",
			tier, manifestFile, joinNames(tierNames(m)))
	}

	switch tier {
	case "native":
		return runNative(m, req.GetArgs())
	case "web":
		if len(req.GetArgs()) > 0 {
			// Not ignored. A dropped argument is only noticed when the output is
			// wrong, and by then nobody suspects the launcher.
			return fmt.Errorf("run: the web tier takes no program arguments (got %s) — they have nowhere to go",
				joinNames(req.GetArgs()))
		}
		return runWeb(declared, req.GetNoOpen())
	default:
		// The default answer for a tier `dlc` cannot start.
		return fmt.Errorf("run: tier %q is declared but `dlc run` does not know how to launch it — "+
			"only native and web can be started from here", tier)
	}
}

// runNative builds the tier's binary and execs it, forwarding arguments.
//
// Built first, deliberately: `dlc run native state` after editing a handler
// should run the edit, not yesterday's binary. That is the whole difference
// between this and typing `./myapp` yourself.
func runNative(m *Manifest, args []string) error {
	bin := filepath.Join(os.TempDir(), "dlc-run-"+m.Name)
	if err := run("go", append([]string{"build", "-o", bin}, "./"+filepath.ToSlash(m.Tiers["native"].Root))...); err != nil {
		return fmt.Errorf("run: building the native tier: %w", err)
	}

	// The launched program owns the terminal from here: its stdout is program
	// output, not build chatter, so unlike `run()` this wires stdout through.
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// runWeb builds the component, starts the dev server, and opens a browser.
func runWeb(tier Tier, noOpen bool) error {
	// Same defaults `dlc build web` resolves, from the same declaration — so
	// `run` cannot serve a component built to a different path than the one the
	// page will fetch. That divergence is exactly what one code path prevents.
	out := firstNonEmpty(tier.Component, "build/engine.component.wasm")
	webOut := firstNonEmpty(tier.Assets, filepath.Join(tier.Root, "src", "wasm"))
	if err := buildWeb(out, webOut, "./cmd/engine-component"); err != nil {
		return err
	}

	slot := tier.Root
	if err := run("npm", "--prefix", slot, "install"); err != nil {
		return fmt.Errorf("run: npm install in %s: %w", slot, err)
	}

	// Vite prints its own URL, and it is the authority on which port it got —
	// so the browser is opened against the CONVENTIONAL address rather than a
	// scraped one, and a port clash shows up as a browser that lands nowhere
	// while the server's own output says where it actually is. Guessing quietly
	// would be worse: the URL would look right and be wrong.
	const url = "http://127.0.0.1:5173"
	if !noOpen {
		if err := openBrowser(url); err != nil {
			// Not fatal. The server is the point; the browser is a convenience,
			// and a headless machine has no business failing the command.
			fmt.Fprintf(os.Stderr, "run: could not open a browser (%v) — visit %s\n", err, url)
		}
	}

	fmt.Fprintf(os.Stderr, "run: serving %s (ctrl-c to stop)\n", slot)
	cmd := exec.Command("npm", "--prefix", slot, "run", "dev")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// openBrowser is the one genuinely platform-specific thing in `dlc`, and it is
// confined to this function on purpose.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return run("open", url)
	case "windows":
		return run("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return run("xdg-open", url)
	}
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
