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
//		_ "github.com/devalbo/dlc-platform/wasm" // wires the exports
//	)
//
//	func main() {}
//
// Built by `dlc build web` (never by hand): the WIT world lives in dlc, not in
// the app, so a WIT change does not strand every previously generated project.
package wasm

import (
	"github.com/devalbo/dlc-platform"
	wit "github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/dlc-platform/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() { wit.Exports.Execute = execute }

// execute is the real boundary: method_id + proto-encoded request bytes.
func execute(method uint32, request cm.List[uint8]) wit.CommandResult {
	return ToCommandResult(platform.Execute(method, request.Slice()))
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
