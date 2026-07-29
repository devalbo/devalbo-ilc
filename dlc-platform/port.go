package platform

// EnginePort — the whole surface a native tier slot uses to reach the engine.
//
// The Go counterpart of `hosts/web/port.ts`, and deliberately the same shape: a
// slot (Decision 34) renders what the engine tells it and decides nothing, so
// everything it needs from the engine is "run a command". The web tier's port
// carries `subscribe` as well; this one does not, because no native host
// delivers events yet — when one does, it gains the method here rather than a
// second interface.
//
// Why an interface at all, when `Execute` is a package function: so a slot can
// take the engine as an ARGUMENT instead of reaching for a global. That is what
// makes a slot testable with no engine at all — `platformtest.NewFake` satisfies
// this with scripted replies, and the slot cannot tell the difference. If a slot
// behaves differently against a fake, it was reading something it should not
// have been.
type EnginePort interface {
	// Execute runs one command. Errors ride the Result envelope, never a panic.
	Execute(method uint32, request []byte) Result
}

// livePort dispatches into the in-process registry — the real engine, linked
// natively (Decision 26).
type livePort struct{}

func (livePort) Execute(method uint32, request []byte) Result {
	return Execute(method, request)
}

// Live is the real engine. A slot's main() passes this; its tests pass a fake,
// and nothing else about the slot changes between the two.
var Live EnginePort = livePort{}
