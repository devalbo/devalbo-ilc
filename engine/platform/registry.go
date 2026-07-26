// Package platform is the ILC runtime an app INHERITS rather than writes
// (§16.3): command dispatch, the filesystem root seam, path containment, BFT
// bundles, and the verbs every app needs (version, export-fs, import-fs,
// reset-fs).
//
// It is deliberately a separate package from the app's `engine/` so the boundary
// is visible and mechanical to extract: this directory becomes the
// `ilc-platform` module (§16.4), which templates **depend on and never inline**
// (§16.6). That rule is not tidiness — code copied into a scaffold is frozen
// there forever, so a path-containment fix inlined into a template could never
// reach an app that had already been generated.
//
// Everything here must stay TinyGo-safe and reflection-free: it compiles into
// every tier, including wasm and (eventually) embedded.
package platform

import "strconv"

// Result is the host-neutral outcome of a command. The wasip2 entrypoint maps it
// to the WIT command-result; the native host consumes it directly. An empty Err
// means success-with-no-error (maps to option<string> = none).
type Result struct {
	Success bool
	Output  []byte
	Err     string
}

// Handler is the byte-level command shape the registry stores: proto-encoded
// request in, Result out. Command functions are written against typed messages
// and adapted by TypedHandler.
type Handler func(request []byte) Result

// Method ids reserved for the platform, in per-capability blocks. Permanent:
//
//	    1 –   99   core lifecycle
//	  100 –  199   filesystem
//	  200 –  299   index      (SQLite)
//	  300 –  399   events
//	  400 –  499   display
//	  500 –  599   network
//	  600 –  999   reserved
//	 1000 +        the app's own commands
//
// One flat map serves all of it, so the ranges are what stop an app colliding
// with a platform verb added later. The platform range is far larger than it
// needs to be on purpose: ids are u32, so reserving 999 costs nothing, while
// widening the range once apps exist would break every app inside it.
// See proto/devalbo/ilc/v1/platform.proto.
const (
	// core lifecycle (1–99)
	MethodVersion uint32 = 1

	// filesystem (100–199)
	MethodExportFs uint32 = 100
	MethodImportFs uint32 = 101
	MethodResetFs  uint32 = 102

	// AppMethodBase is the first id an app may claim.
	AppMethodBase uint32 = 1000
)

var registry = map[uint32]Handler{}

// Register binds a handler to a method_id. Registering the same id twice is a
// programming error (two commands claiming one wire number), so it panics at
// init rather than letting one silently shadow the other.
//
// An app MAY deliberately take over a platform id to change its semantics — but
// it has to Unregister first, so an override is never accidental.
func Register(method uint32, h Handler) {
	if _, dup := registry[method]; dup {
		panic("platform: duplicate method_id " + strconv.FormatUint(uint64(method), 10))
	}
	registry[method] = h
}

// Unregister drops a handler so an app can replace a platform verb with its own.
// Reports whether anything was registered.
func Unregister(method uint32) bool {
	_, ok := registry[method]
	delete(registry, method)
	return ok
}

// Execute dispatches one command: method_id → registry → the handler decodes
// `request` as its own message type and returns response bytes in the
// command-result envelope (Decision 28). An unknown id is an error result, not a
// panic — hosts and engines version independently.
func Execute(method uint32, request []byte) Result {
	h, ok := registry[method]
	if !ok {
		return Result{Err: "unknown method_id " + strconv.FormatUint(uint64(method), 10)}
	}
	return h(request)
}

// protoRequest is the go-lite request contract: a pointer to T that can decode
// itself. The `*T` constraint lets TypedHandler allocate a T and use it as PReq
// without reflection — the TinyGo-safe stand-in for a generic factory.
type protoRequest[T any] interface {
	*T
	UnmarshalVT([]byte) error
}

// protoResponse is the go-lite response contract.
type protoResponse interface {
	MarshalVT() ([]byte, error)
}

// TypedHandler adapts a typed command function to a Handler: decode the request
// as its own message type (flat, single-encode — Spike 2; there is no envelope),
// run it, encode the response. Errors ride the command-result envelope, which is
// why no response message carries an error field.
func TypedHandler[Req any, PReq protoRequest[Req], Resp protoResponse](fn func(PReq) (Resp, error)) Handler {
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
