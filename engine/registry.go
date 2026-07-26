package engine

// The command registry (Decision 29). Every command is one entry in a
// `map[uint32]Handler` keyed by the permanent `method_id` its rpc carries in
// commands.proto — the rpc *name* is cosmetic, so renaming a verb is wire-safe.
// Dispatch is a map lookup, never reflection and never a `switch` that has to be
// edited for each new command.
//
// Registration is explicit and happens once, in commands.go. That hand-written
// registration is what `protoc-gen-dlc-registry` will emit later; keeping it
// hand-written first proves the shape the plugin has to produce.

import "strconv"

// Handler is the byte-level command shape the registry stores: proto-encoded
// request in, Result (proto-encoded response, or an error) out. Command
// functions are written against typed messages and adapted by typedHandler.
type Handler func(request []byte) Result

// registry maps method_id → handler. Populated by Register at init.
var registry = map[uint32]Handler{}

// Register binds a handler to a method_id. Registering the same id twice is a
// programming error (two commands claiming one wire number), so it panics at
// init rather than silently letting one shadow the other.
func Register(method uint32, h Handler) {
	if _, dup := registry[method]; dup {
		panic("engine: duplicate method_id " + strconv.FormatUint(uint64(method), 10))
	}
	registry[method] = h
}

// lookup returns the handler for a method_id.
func lookup(method uint32) (Handler, bool) {
	h, ok := registry[method]
	return h, ok
}

// protoRequest is the go-lite request contract: a pointer to T that can decode
// itself. The `*T` constraint lets typedHandler allocate a T and use it as PReq
// without reflection — the TinyGo-safe stand-in for a generic factory.
type protoRequest[T any] interface {
	*T
	UnmarshalVT([]byte) error
}

// protoResponse is the go-lite response contract.
type protoResponse interface {
	MarshalVT() ([]byte, error)
}

// typedHandler adapts a typed command function to a Handler: decode the request
// as its own message type (flat, single-encode — Spike 2; there is no envelope),
// run it, encode the response. Errors ride the command-result envelope, which is
// why no response message carries an error field.
func typedHandler[Req any, PReq protoRequest[Req], Resp protoResponse](fn func(PReq) (Resp, error)) Handler {
	return func(request []byte) Result {
		var req Req
		msg := PReq(&req)
		if err := msg.UnmarshalVT(request); err != nil {
			return Result{Err: "decode request: " + err.Error()}
		}
		resp, err := fn(msg)
		if err != nil {
			return Result{Err: err.Error()}
		}
		out, err := resp.MarshalVT()
		if err != nil {
			return Result{Err: "encode response: " + err.Error()}
		}
		return Result{Success: true, Output: out}
	}
}
