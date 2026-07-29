// Package engine holds dlc's portable business logic. It compiles two ways
// (Decision 26): linked natively in-process by hosts/native, and as a wasip2
// component via cmd/engine-component. Keep it free of WIT / cm types and build
// tags so it builds under plain `go` and TinyGo alike.
//
// ONE entry point (Decisions 28/29/31): a scalar method_id plus proto-encoded
// request bytes, dispatched by a registry map lookup. The engine never sees
// argv — command parsing is the host's job, because a browser tab has no argv
// and an embedded REPL has a different one. hosts/native/commands.go is this
// tier's parser.
//
// Everything here stays reflection-free: `new` renders templates with plain
// string substitution rather than text/template (reflection-heavy under TinyGo).
package engine

import "github.com/devalbo/dlc-platform"

// Result is the dispatch envelope. Aliased from the platform so hosts keep
// importing one package (this one) while the type is owned where the registry
// lives.
type Result = platform.Result

// ExecuteMethod dispatches one command through the platform registry. Importing
// this package is what registers dlc's commands (see commands.go's init), so a
// host that links the app gets the app's verbs plus the platform's.
func ExecuteMethod(method uint32, request []byte) Result {
	return platform.Execute(method, request)
}
