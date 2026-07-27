package platform

// Events — the reactivity capability (§6.3).
//
// An app calls Emit; a HOST decides what that means. On the web tier the event
// crosses into the UI thread and invalidates a view; on a CLI it is usually
// nothing at all. The engine never learns which.
//
// This file is the portable half. The half that actually delivers is behind a
// build tag — caps_native.go for in-process hosts, caps_wasip2.go for the
// component boundary — which is the §5.3 seam. Everything above the seam is
// identical on every tier, and that is the whole point: `notes` emits the same
// event from a terminal and from a browser tab, without knowing the difference.
//
// TWO RULES, both of which fail far from their cause if broken:
//
//  1. Emitting is FIRE-AND-FORGET. There is no return value and no error. An
//     app must never be able to tell whether anyone is listening, because on
//     some tier nobody is, and code that branches on it would behave
//     differently per tier — exactly what this architecture exists to prevent.
//
//  2. A host must NOT call back into Execute from a sink. The engine is on the
//     stack; re-entering it mid-command is how you get corrupt state or a
//     deadlock. Hosts defer instead (the web host gets this for free — Comlink
//     is a message boundary).

// TopicDataChanged is the platform's own topic: the filesystem changed under
// some prefix. Apps namespace their own as "<app>.<what-happened>".
const TopicDataChanged = "ilc.data-changed"

// EventSink receives an emitted event. Hosts install one with SetEventSink.
type EventSink func(topic string, payload []byte)

// sink is the installed handler, or nil when nothing is listening.
//
// Package-level rather than passed through handlers: an app's business logic
// should not thread a sink through every call site any more than it threads a
// filesystem. Capabilities are ambient to the engine and injected by the host.
var sink EventSink

// SetEventSink installs the host's handler. Passing nil disables delivery.
//
// Called by the HOST during startup, never by an app's engine code. The wasip2
// build ignores it entirely — there the sink is the WIT import, wired by the
// component boundary rather than by a Go call.
func SetEventSink(fn EventSink) { sink = fn }

// Emit publishes an event. Safe on every tier, including ones where no host is
// listening, where it does nothing.
//
// Topic convention is a namespaced string: "ilc.data-changed" for platform
// events, "<app>.<what-happened>" for an app's own. Payload is a proto-encoded
// message (§7.2) — never JSON, never a bare string, so subscribers decode it the
// same way they decode a command response.
func Emit(topic string, payload []byte) {
	if topic == "" {
		return // a topic nobody can subscribe to is not an event
	}
	emit(topic, payload)
}
