// Package wit carries the ILC capability world, compiled into the `dlc` binary.
//
// Embedded because APPS DO NOT OWN THE WIT. A scaffolded project needs the world
// (and its vendored WASI deps) to build a component, but shipping a copy into
// every scaffold would strand each generated project on whatever WIT existed the
// day it was created — and the world is framework surface, not app surface.
// Instead `dlc build web` materializes these bytes into a temp directory and
// points TinyGo at them, so a WIT change reaches every app the next time it
// builds, with no per-project migration.
//
// This is the reason `dlc` is a required build tool rather than just `go` + `buf`.
package wit

import "embed"

//go:embed all:deps ilc.wit
var FS embed.FS
