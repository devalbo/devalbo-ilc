// Spike — go-lite oneof under TinyGo (registry de-risk for Decision 29).
//
// Proves the load-bearing assumption behind the command registry: a protobuf
// `oneof` command envelope (protobuf-go-lite) encodes/decodes AND dispatches via
// a **map keyed on the oneof discriminator (no switch)** under
// `tinygo -target=wasip2`. Spike 2 only proved a *flat* message.
//
// Flow per call: build a Command oneof → MarshalVT/UnmarshalVT (binary wire
// round-trip) → MarshalJSON/UnmarshalJSON (canonical JSON round-trip) → dispatch
// the decoded command through the tag→handler registry.
package main

import (
	"strconv"

	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	pb "github.com/devalbo/devalbo-ilc/gen/go/devalbo/oneofspike/v1"
	"go.bytecodealliance.org/cm"
)

// --- the registry: a map keyed on the oneof discriminator (the "no switch" win) ---

type handler func(*pb.Command) *pb.CommandResult

const (
	tagGreet int32 = 1
	tagAdd   int32 = 2
)

var handlers = map[int32]handler{}

func init() {
	engine.Exports.ExecuteCli = executeCli
	// App registers each command once — adding one never edits a central switch.
	handlers[tagGreet] = handleGreet
	handlers[tagAdd] = handleAdd
}

// tagOf is the ONE type-switch — exactly the 1-line-per-arm helper
// protoc-gen-dlc-registry will emit; the app never writes it. Dispatch itself
// (below) is switch-free. go-lite has no WhichX() getter, so the discriminator
// comes from a type-switch over the exported wrapper types.
func tagOf(c *pb.Command) int32 {
	switch c.GetCommand().(type) {
	case *pb.Command_Greet:
		return tagGreet
	case *pb.Command_Add:
		return tagAdd
	default:
		return 0
	}
}

func dispatch(c *pb.Command) *pb.CommandResult {
	h, ok := handlers[tagOf(c)] // O(1) map lookup, no switch
	if !ok {
		return &pb.CommandResult{Text: "unhandled"}
	}
	return h(c)
}

func handleGreet(c *pb.Command) *pb.CommandResult {
	return &pb.CommandResult{Text: "hello " + c.GetGreet().GetName()}
}

func handleAdd(c *pb.Command) *pb.CommandResult {
	a := c.GetAdd()
	return &pb.CommandResult{Text: strconv.Itoa(int(a.GetA() + a.GetB()))}
}

// --- engine export ---

func executeCli(args cm.List[string]) engine.CommandResult {
	cmd, errMsg := buildCommand(args.Slice())
	if errMsg != "" {
		return fail(errMsg)
	}

	// Binary wire round-trip — the load-bearing test (go-lite oneof codec, TinyGo).
	raw, err := cmd.MarshalVT()
	if err != nil {
		return fail("marshalVT: " + err.Error())
	}
	var fromBin pb.Command
	if err := fromBin.UnmarshalVT(raw); err != nil {
		return fail("unmarshalVT: " + err.Error())
	}

	// Canonical JSON round-trip — mirrors Spike 2's dual binary+JSON coverage.
	jb, err := cmd.MarshalJSON()
	if err != nil {
		return fail("marshalJSON: " + err.Error())
	}
	var fromJSON pb.Command
	if err := fromJSON.UnmarshalJSON(jb); err != nil {
		return fail("unmarshalJSON: " + err.Error())
	}

	// Dispatch the binary-decoded command through the switch-free registry.
	res := dispatch(&fromBin)
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList([]byte(res.GetText())),
		Error:   cm.None[string](),
	}
}

// buildCommand stands in for the host front-end constructing the request (which
// arm to set is inherently a branch — in the real system the host does this from
// argv/form). The registry dispatch above is what must be switch-free.
func buildCommand(args []string) (*pb.Command, string) {
	if len(args) == 0 {
		return nil, "no command"
	}
	switch args[0] {
	case "greet":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return &pb.Command{Command: &pb.Command_Greet{Greet: &pb.GreetRequest{Name: name}}}, ""
	case "add":
		if len(args) < 3 {
			return nil, "add needs two numbers"
		}
		a, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, "add: bad number " + args[1]
		}
		b, err := strconv.Atoi(args[2])
		if err != nil {
			return nil, "add: bad number " + args[2]
		}
		return &pb.Command{Command: &pb.Command_Add{Add: &pb.AddRequest{A: int32(a), B: int32(b)}}}, ""
	default:
		return nil, "unknown verb: " + args[0]
	}
}

func fail(msg string) engine.CommandResult {
	return types.CommandResult{
		Success: false,
		Output:  cm.ToList([]byte{}),
		Error:   cm.Some(msg),
	}
}

func main() {}
