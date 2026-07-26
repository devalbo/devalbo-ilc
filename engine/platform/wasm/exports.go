//go:build tinygo

// Package wasm adapts the WIT exports to the platform registry — the wasip2
// component entrypoint that EVERY ILC app shares.
//
// It is app-agnostic by construction: dispatch is by `method_id` into one
// registry, so this glue never needs to know which commands exist. That is why
// it lives in the platform instead of being copied into each scaffold — a
// generated project should not carry 40 lines of `cm.List` marshalling it will
// never edit, and a fix here should reach apps on a version bump.
//
// An app's component main is then only:
//
//	//go:build tinygo
//	package main
//
//	import (
//		_ "myapp/engine"                                        // registers commands
//		_ "github.com/devalbo/devalbo-ilc/engine/platform/wasm" // wires the exports
//	)
//
//	func main() {}
//
// Built by `dlc build web` (never by hand): the WIT world lives in dlc, not in
// the app, so a WIT change does not strand every previously generated project.
package wasm

import (
	"github.com/devalbo/devalbo-ilc/engine/platform"
	wit "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() {
	wit.Exports.Execute = execute
	// The world declares execute-cli, so SOMETHING must satisfy it or the
	// component will not build. Apps do not have an argv shim — their hosts
	// parse (Decision 28) — so the default refuses, and `dlc` installs its own
	// via SetExecuteCli until the shim retires.
	wit.Exports.ExecuteCli = executeCliUnsupported
}

// execute is the real boundary: method_id + proto-encoded request bytes.
func execute(method uint32, request cm.List[uint8]) wit.CommandResult {
	return ToCommandResult(platform.Execute(method, request.Slice()))
}

var executeCli func(args []string) platform.Result

// SetExecuteCli installs an argv shim for the transitional `execute-cli` export.
// Only `dlc` itself uses this; a scaffolded app leaves it unset, and calling
// execute-cli on that app returns a clear error rather than a mystery.
func SetExecuteCli(fn func(args []string) platform.Result) { executeCli = fn }

func executeCliUnsupported(args cm.List[string]) wit.CommandResult {
	if executeCli != nil {
		return ToCommandResult(executeCli(args.Slice()))
	}
	return ToCommandResult(platform.Result{
		Err: "execute-cli is not implemented by this engine; the host builds a request and calls execute (Decision 28)",
	})
}

// ToCommandResult maps the host-neutral Result onto the WIT record. Exported
// because a host embedding the engine differently may need the same mapping.
func ToCommandResult(r platform.Result) wit.CommandResult {
	res := types.CommandResult{
		Success: r.Success,
		Output:  cm.ToList(r.Output),
	}
	if r.Err != "" {
		res.Error = cm.Some(r.Err)
	} else {
		res.Error = cm.None[string]()
	}
	return res
}
