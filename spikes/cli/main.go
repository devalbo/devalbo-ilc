// Spike 4 — in-engine CLI interpreter (T-B1.4).
//
// The engine parses subcommands + flags ITSELF and dispatches; the host just
// forwards argv. Bake-off variants behind build tags:
//
//	(default)       stdlib flag          — parse_flag.go
//	-tags cliffcli  peterbourgon/ffcli   — parse_ffcli.go
//	-tags clihand   hand-rolled          — parse_hand.go
//	-tags clisub    google/subcommands   — parse_sub.go
//	-tags clicobra  spf13/cobra          — parse_cobra.go
//	-tags clikong   alecthomas/kong      — parse_kong.go
//	-tags cligoarg  alexflint/go-arg     — parse_goarg.go
//
// Shared surface (cmds.go): greet / count / host add — see README matrix.
package main

import (
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() { engine.Exports.ExecuteCli = executeCli }

func executeCli(args cm.List[string]) engine.CommandResult {
	out, err := dispatch(args.Slice())
	if err != nil {
		return types.CommandResult{
			Success: false,
			Output:  cm.ToList([]byte{}),
			Error:   cm.Some(err.Error()),
		}
	}
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList([]byte(out)),
		Error:   cm.None[string](),
	}
}

func main() {}
