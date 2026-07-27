//go:build !tinygo

package platform

// The NATIVE half of the capability seam (§5.3).
//
// Here a capability is a direct Go call: the host linked the engine in-process
// (Decision 26), so there is no boundary to cross and no serialization to pay.
// The wasip2 half of this seam is caps_wasip2.go, which calls a WIT import
// instead. Business logic above the seam is byte-identical, and the parity check
// exists to keep that claim honest.

// emit delivers to the in-process sink, if a host installed one.
//
// A sink that panics must not take the engine down with it. A host's UI callback
// is not the engine's business, and a command that succeeded should not report
// failure because a listener was buggy — the event has already happened, and the
// filesystem write it describes is already committed.
func emit(topic string, payload []byte) {
	fn := sink
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(topic, payload)
}
