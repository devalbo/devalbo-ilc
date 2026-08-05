// Spike 5 — Rich/CM async ecosystem probe (T-B1.5).
//
// Guest calls host-delay.delay synchronously from execute-cli. The JS host may
// implement delay with setTimeout/Promise. We use only stock TinyGo + jco
// (--async-mode jspi is jco's ecosystem feature, not an ILC shim).
//
// World: devalbo:ilc/async-engine (see wit/ilc.wit).
package main

import (
	"strconv"

	asyncengine "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/async-engine"
	hostdelay "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/host-delay"
	"github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() { asyncengine.Exports.ExecuteCli = executeCli }

func executeCli(args cm.List[string]) asyncengine.CommandResult {
	ms := uint32(50)
	if s := args.Slice(); len(s) >= 2 && s[0] == "wait" {
		if v, err := strconv.ParseUint(s[1], 10, 32); err == nil {
			ms = uint32(v)
		}
	}
	// Blocking call from the guest's POV — host may be async in JS.
	out := hostdelay.Delay(ms)
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList([]byte(out)),
		Error:   cm.None[string](),
	}
}

func main() {}
