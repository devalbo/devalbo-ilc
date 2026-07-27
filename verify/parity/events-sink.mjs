// The `devalbo:ilc/events` import, for the parity harness.
//
// jco turns a WIT import into a bare module specifier — the transpiled component
// does `import { emit } from 'devalbo:ilc/events'` — so a host satisfies it by
// mapping that specifier to a real module. This is that module for the wasm side
// of the parity check; the browser has its own in the worker.
//
// SYNCHRONOUS, and it must stay that way: an `async` function here returns a
// Promise, which jco only supports under `--async-imports` (Decision 22, see
// docs/WASI-UPGRADES.md). It would also break the engine's assumption that
// emitting is fire-and-forget.
const recorded = [];

export function emit(topic, payload) {
  recorded.push({ topic, payload: Buffer.from(payload ?? []).toString("base64") });
}

/** Drain what was emitted since the last drain, in emission order. */
export function drainEvents() {
  return recorded.splice(0, recorded.length);
}
