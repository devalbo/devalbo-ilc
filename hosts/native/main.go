// Native dlc host (Decision 26): links the engine in-process — no wasm runtime in
// the run path, sidestepping the wasmtime-go Component-Model gap. This is the
// reference `dlc` binary for terminal use.
//
// TWO KINDS OF VERB pass through here, and the split is Decision 30:
//
//   - TOOLCHAIN verbs (`build`) spawn processes and inspect the machine, so they
//     are handled HOST-SIDE and never reach the engine — the engine also runs in
//     a browser tab, where neither is possible.
//   - IN-ENGINE verbs (`new`, `version`, `export-fs`, …) are parsed here into a
//     proto request and dispatched by method id (Decision 28). The engine never
//     sees argv; commands.go is that parser.
//
// Build:
//
//	go build -o dlc ./hosts/native
package main

import (
	"os"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"

	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"

	// Importing the engine is what REGISTERS dlc's commands; nothing else here
	// touches it. The blank-ish import is load-bearing.
	_ "github.com/devalbo/devalbo-ilc/engine"
)

// toolchainVerbs never cross into the engine (Decision 30) — they spawn the dev
// toolchain, which cannot happen inside wasm.
//
// Keyed by the method id DECLARED in proto/devalbo/dlc/v1/toolchain.proto, not by
// name. Before that file this map was keyed by string and consulted BEFORE the
// CLI ran, which meant `gen` and `build` never appeared in `dlc --help`: the two
// commands every tutorial tells you to run were missing from the tool's own
// command list.
//
// Now the name, summary, flags and defaults all come from the schema like every
// other command's, and this map supplies only the behaviour — which is why each
// handler takes an ENCODED REQUEST rather than argv. There is no hand-rolled flag
// loop left in any of them.
var toolchainVerbs = map[uint32]func(request []byte) error{
	dlcv1.MethodBuild: runBuild,
	dlcv1.MethodGen:   runGen,
	dlcv1.MethodRun:   runRun,
}

func main() {
	// The whole startup sequence, in the one order that works (§2.5). Owned by
	// the platform so a fix to the order reaches every app rather than only the
	// ones scaffolded after it.
	//
	// dlc GRANTS ITSELF the working directory, overriding the `./.<app>/`
	// convention every other app follows.
	//
	// That is not an exception so much as the rule applied honestly: an app's
	// root is where its data lives, and dlc's data IS the user's project —
	// `dlc new myapp` must scaffold where you are standing, and `export-fs`
	// must bundle the tree in front of you. An app whose output belongs to the
	// user rather than to itself should do the same; one that keeps a private
	// store (notes, tictactoe) should not.
	if err := platform.Boot(platform.BootOptions{
		FSRoot:         ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
		// WHERE TEXT GOES, declared rather than assumed. An app cannot work this
		// out for itself: every tier provides `wasi:cli/stdout`, so its presence
		// proves nothing and a badge with no screen looks identical from inside.
		//
		// Cols and Rows stay UNMEASURED (zero). Go has no portable way to ask a
		// terminal its size, and a guessed 80 would be worse than no answer — an
		// app reads zero as "wrap however you like" and a wrong number as a
		// budget to format against. A host that measures sets them here and
		// re-sends the manifest with a bumped revision on SIGWINCH.
		TextOutlet: ilcv1.TextOutlet_TEXT_OUTLET_TERMINAL,
	}); err != nil {
		os.Stderr.WriteString("dlc: " + err.Error() + "\n")
		os.Exit(2)
	}

	// EVERY verb goes through the generated command surface (Decision 29), engine
	// and host-local alike. The runner routes on `Local` — declared in the
	// .proto — so there is no pre-CLI interception and nothing is invisible to
	// `--help`.
	os.Exit(runCommand(os.Args[1:]))
}
