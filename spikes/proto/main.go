// Spike 2 — protobuf-go-lite under TinyGo (T-B1.2).
//
// Encodes a fixed SpikeMessage to binary (MarshalVT) and canonical JSON
// (MarshalJSON) under -target=wasip2. Modes via execute-cli args:
//
//	["binary"] → protobuf wire bytes
//	["json"]   → canonical-JSON bytes
//
// The Node harness compares both to checked-in goldens and cross-decodes with
// protobuf-es-lite. See spikes/README.md (Spike 2).
package main

import (
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	spikev1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/spike/v1"
	"go.bytecodealliance.org/cm"
)

// Fixed fixture — must match goldens and the Node harness expectations.
func fixture() *spikev1.SpikeMessage {
	return &spikev1.SpikeMessage{
		Name:  "spike",
		Count: 42,
		Ok:    true,
	}
}

func init() {
	engine.Exports.ExecuteCli = executeCli
}

func executeCli(args cm.List[string]) engine.CommandResult {
	mode := ""
	if s := args.Slice(); len(s) > 0 {
		mode = s[0]
	}

	msg := fixture()
	var out []byte
	var err error
	switch mode {
	case "binary":
		out, err = msg.MarshalVT()
	case "json":
		out, err = msg.MarshalJSON()
	default:
		return fail("usage: execute-cli [binary|json]")
	}
	if err != nil {
		return fail(err.Error())
	}
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList(out),
		Error:   cm.None[string](),
	}
}

func fail(msg string) engine.CommandResult {
	return types.CommandResult{
		Success: false,
		Output:  cm.ToList([]byte(nil)),
		Error:   cm.Some(msg),
	}
}

func main() {}
