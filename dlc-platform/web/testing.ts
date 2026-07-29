// A fake EnginePort, for testing a tier slot with NO engine (Decision 34).
//
// Why this exists, in one sentence: a slot's rendering is the one part of an ILC
// app that parity cannot check — it compares command results, the written
// filesystem, and the event stream, all engine-side — so a slot needs its own
// way to be wrong out loud.
//
// What it buys beyond speed:
//
//   * Events can be driven. A real engine emits only what a real command causes,
//     so "what does the UI do when a write arrives from somewhere else" is
//     awkward to stage and trivial here.
//   * Failure paths are reachable. `success: false` from the engine takes real
//     effort to produce on purpose and one line to script.
//   * It is how a renderer gets developed for a tier that cannot run yet — an
//     ESP32 slot has no host to run against until the firmware exists.
//
// It deliberately does NOT simulate the engine. Scripted responses are stated
// by the test, not computed: a fake that grew logic would be a second
// implementation of the engine, disagreeing with the real one in its own way,
// and the tiers-must-not-diverge argument applies to test doubles too.
import type { CommandResult, EnginePort } from "./port";

/** One scripted answer. A function may vary by request; a value is constant. */
export type Reply =
	| CommandResult
	| ((method: number, request: Uint8Array) => CommandResult);

export type Recorded = { method: number; request: Uint8Array };

export type FakePort = {
	port: EnginePort;
	/** Deliver one event to every current subscriber, as the worker would. */
	emit(topic: string, payload: Uint8Array): void;
	/** Every command the slot ran, in order. */
	calls: Recorded[];
	/** Replace the scripted answer for a method mid-test. */
	reply(method: number, reply: Reply): void;
	/** How many subscribers the slot currently holds (0 after it unsubscribes). */
	subscriberCount(): number;
};

/**
 * Build a fake port from a method → reply script.
 *
 * An unscripted method is an ERROR result naming the method, not a throw and not
 * an empty success: a slot that hits an unscripted command has done something
 * the test did not describe, and the failure should say which command rather
 * than surfacing as an undefined field three lines later.
 */
export function createFakePort(script: Record<number, Reply> = {}): FakePort {
	const replies = new Map<number, Reply>(
		Object.entries(script).map(([m, r]) => [Number(m), r]),
	);
	const calls: Recorded[] = [];
	const listeners = new Set<(topic: string, payload: Uint8Array) => void>();

	const port: EnginePort = {
		async execute(method, request) {
			calls.push({ method, request });
			const reply = replies.get(method);
			if (reply === undefined) {
				return {
					success: false,
					output: new Uint8Array(),
					error: `fake port: no scripted reply for method ${method}`,
				};
			}
			return typeof reply === "function" ? reply(method, request) : reply;
		},
		async subscribe(fn) {
			listeners.add(fn);
			return () => {
				listeners.delete(fn);
			};
		},
	};

	return {
		port,
		calls,
		emit(topic, payload) {
			// Snapshot, and swallow nothing: this mirrors api.ts's fan-out, where a
			// listener may unsubscribe from inside its own callback. A throwing
			// listener propagates here on purpose — in a test that is the finding.
			for (const listener of [...listeners]) listener(topic, payload);
		},
		reply(method, r) {
			replies.set(method, r);
		},
		subscriberCount: () => listeners.size,
	};
}

/** A successful result carrying `output`. */
export function ok(output: Uint8Array = new Uint8Array()): CommandResult {
	return { success: true, output };
}

/** A failed result, as the engine's envelope reports one. */
export function err(message: string): CommandResult {
	return { success: false, output: new Uint8Array(), error: message };
}
