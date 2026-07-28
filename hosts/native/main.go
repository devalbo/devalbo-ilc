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

	"github.com/devalbo/devalbo-ilc/engine/platform"

	// Importing the engine is what REGISTERS dlc's commands; nothing else here
	// touches it. The blank-ish import is load-bearing.
	_ "github.com/devalbo/devalbo-ilc/engine"
)

// toolchainVerbs never cross into the engine (Decision 30).
var toolchainVerbs = map[string]func([]string) error{
	"build": runBuild,
	"gen":   runGen,
}

func main() {
	// dlc GRANTS ITSELF the working directory, overriding the `./.<app>/`
	// convention every other app follows.
	//
	// That is not an exception so much as the rule applied honestly: an app's
	// root is where its data lives, and dlc's data IS the user's project —
	// `dlc new myapp` must scaffold where you are standing, and `export-fs`
	// must bundle the tree in front of you. An app whose output belongs to the
	// user rather than to itself should do the same; one that keeps a private
	// store (notes, tictactoe) should not.
	if err := platform.SetRoot("."); err != nil {
		os.Stderr.WriteString("dlc: " + err.Error() + "\n")
		os.Exit(2)
	}

	args := os.Args[1:]

	if len(args) > 0 {
		if handler, ok := toolchainVerbs[args[0]]; ok {
			if err := handler(args[1:]); err != nil {
				os.Stderr.WriteString("dlc: " + err.Error() + "\n")
				os.Exit(1)
			}
			return
		}
	}

	// In-engine verbs go through the GENERATED command surface (Decision 29):
	// no switch, no usage string, just the renderers in commands.go.
	os.Exit(runCommand(args))
}
