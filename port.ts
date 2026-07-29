// EnginePort — the whole surface a tier slot uses to reach the engine.
//
// A slot (Decision 34) is this app's presentation on one tier. It renders what
// the engine tells it and decides nothing, which means everything it needs from
// the engine fits in two functions: run a command, and hear that something
// changed.
//
// Naming that as a TYPE, and having slots take it as an ARGUMENT rather than
// importing `./api` directly, is what makes a slot testable with no engine — the
// harness in `./testing` satisfies this interface with scripted responses and a
// hand-driven event stream, and the slot cannot tell the difference. That is the
// point: if a slot behaves differently against a fake, it was reading something
// it should not have been.
//
// It is also the shape a non-web slot needs. An embedded renderer has no
// Comlink, no worker and no OPFS, but it has exactly these two operations.
import { execute, subscribe } from "./api";
import type { CommandResult } from "./worker";

export type { CommandResult };

export type EnginePort = {
	/** Run one command. Errors ride the result envelope, not exceptions. */
	execute(method: number, request: Uint8Array): Promise<CommandResult>;
	/** Listen for engine events (§6.3). Resolves to an unsubscribe. */
	subscribe(fn: (topic: string, payload: Uint8Array) => void): Promise<() => void>;
};

/**
 * The real engine: a live jco component in a worker, over Comlink.
 *
 * A slot's `main.ts` passes this; its tests pass a fake. Nothing else about the
 * slot changes between the two, which is the property being bought.
 */
export const enginePort: EnginePort = { execute, subscribe };
