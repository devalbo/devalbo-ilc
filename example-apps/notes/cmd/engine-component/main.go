//go:build tinygo

// Wasip2 component entrypoint for notes — the artifact the browser loads.
//
// It is this short because the WIT glue is app-agnostic and belongs to the ILC
// platform: dispatch is by method_id into one registry, so nothing here needs to
// know which commands exist. Importing the two packages is the whole job —
// `engine` registers your commands, `platform/wasm` wires the exports.
//
// Build it with `dlc build web` (never tinygo directly): the WIT world lives in
// dlc, not in this project, so a world change reaches you on the next build
// instead of requiring a migration.
package main

import (
	_ "github.com/devalbo/devalbo-ilc/example-apps/notes/engine" // registers this app's commands

	_ "github.com/devalbo/devalbo-ilc/engine/platform/wasm" // wires the WIT exports
)

func main() {}
