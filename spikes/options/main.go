// Spike — go-lite tolerates custom options (Decision 29 registry gate).
//
// Proves: a .proto that imports descriptor.proto, defines MethodOptions /
// FieldOptions extensions (method_id, help/required/default/short), and applies
// them on a service + request fields still generates under go-lite and round-trips
// under TinyGo wasip2. The engine never reads the options at runtime — only the
// request/response messages. Host-side FileDescriptorSet introspection is a
// separate native Go check (cmd/host-introspect).
package main

import (
	"strconv"
	"strings"

	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	pb "github.com/devalbo/devalbo-ilc/gen/go/devalbo/optionsspike/v1"
	"go.bytecodealliance.org/cm"
)

func init() { engine.Exports.ExecuteCli = executeCli }

func executeCli(args cm.List[string]) engine.CommandResult {
	s := args.Slice()
	if len(s) == 0 {
		return fail("no command")
	}
	switch s[0] {
	case "greet":
		return runGreet(s[1:])
	case "add":
		return runAdd(s[1:])
	default:
		return fail("unknown: " + s[0])
	}
}

func runGreet(args []string) engine.CommandResult {
	req := &pb.GreetRequest{Times: 1}
	if len(args) >= 1 {
		req.Name = args[0]
	}
	if len(args) >= 2 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fail("times: " + err.Error())
		}
		req.Times = int32(n)
	}

	// Binary + JSON round-trip — same load-bearing check as Spike 2, on a
	// message whose .proto *carries* custom field options.
	raw, err := req.MarshalVT()
	if err != nil {
		return fail("marshalVT: " + err.Error())
	}
	var fromBin pb.GreetRequest
	if err := fromBin.UnmarshalVT(raw); err != nil {
		return fail("unmarshalVT: " + err.Error())
	}
	jb, err := req.MarshalJSON()
	if err != nil {
		return fail("marshalJSON: " + err.Error())
	}
	var fromJSON pb.GreetRequest
	if err := fromJSON.UnmarshalJSON(jb); err != nil {
		return fail("unmarshalJSON: " + err.Error())
	}

	times := int(fromBin.GetTimes())
	if times < 1 {
		times = 1
	}
	parts := make([]string, times)
	for i := 0; i < times; i++ {
		parts[i] = "hello " + fromBin.GetName()
	}
	return ok(strings.Join(parts, " "))
}

func runAdd(args []string) engine.CommandResult {
	if len(args) < 2 {
		return fail("add needs two numbers")
	}
	a, err := strconv.Atoi(args[0])
	if err != nil {
		return fail("a: " + err.Error())
	}
	b, err := strconv.Atoi(args[1])
	if err != nil {
		return fail("b: " + err.Error())
	}
	req := &pb.AddRequest{A: int32(a), B: int32(b)}
	raw, err := req.MarshalVT()
	if err != nil {
		return fail("marshalVT: " + err.Error())
	}
	var fromBin pb.AddRequest
	if err := fromBin.UnmarshalVT(raw); err != nil {
		return fail("unmarshalVT: " + err.Error())
	}
	sum := fromBin.GetA() + fromBin.GetB()
	resp := &pb.AddResponse{Sum: sum}
	out, err := resp.MarshalVT()
	if err != nil {
		return fail("resp marshal: " + err.Error())
	}
	var round pb.AddResponse
	if err := round.UnmarshalVT(out); err != nil {
		return fail("resp unmarshal: " + err.Error())
	}
	return ok(strconv.Itoa(int(round.GetSum())))
}

func ok(text string) engine.CommandResult {
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList([]byte(text)),
		Error:   cm.None[string](),
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
