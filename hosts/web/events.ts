// The `devalbo:ilc/events` import, for the web tier.
//
// jco turns a WIT import into a bare module specifier: the transpiled component
// does `import { emit } from 'devalbo:ilc/events'`, and `dlc build web` maps that
// specifier to THIS module. So the engine calling `platform.Emit` inside wasm
// lands in the function below, in the worker.
//
// It lives in the ILC web host package rather than being generated into each
// app, for the same reason the platform is a dependency and not a copy: a fix
// here reaches every app on a version bump.
//
// ─── the rule that matters ───────────────────────────────────────────────────
//
// `emit` MUST be synchronous and MUST NOT throw.
//
//   * Synchronous: an `async` function returns a Promise, and jco only supports
//     Promise-returning imports under `--async-imports`, which ILC deliberately
//     does not use (Decision 22, docs/WASI-UPGRADES.md). The failure surfaces as
//     a jco type error a long way from whoever added the `async`.
//   * Non-throwing: this runs on the engine's stack, mid-command. An exception
//     here would propagate into wasm and fail a command whose filesystem write
//     has ALREADY committed.
//
// It also must not call back into `execute` — that would re-enter the engine
// while a command is on the stack. The forwarder below sends a message to the
// main thread instead, which makes re-entrancy structurally impossible rather
// than merely discouraged.

/** What the worker installs to relay events to the main thread. */
export type EventForwarder = (topic: string, payload: Uint8Array) => void;

let forward: EventForwarder | null = null;

/**
 * Installed by the worker at startup. Not part of an app's API — apps subscribe
 * through `@devalbo/ilc-web/api`.
 */
export function setForwarder(fn: EventForwarder | null): void {
  forward = fn;
}

/**
 * Called BY THE ENGINE, through the component boundary. Never call it directly.
 */
export function emit(topic: string, payload: Uint8Array): void {
  if (!forward) return; // nobody listening is a normal state, not an error
  try {
    // Copy: `payload` points into the component's linear memory, which is reused
    // as soon as this returns. Handing the raw view to the main thread would
    // deliver whatever the engine wrote next — a data race with no error.
    forward(topic, new Uint8Array(payload));
  } catch {
    // Swallowed on purpose: see the header. A listener's bug must not fail a
    // command that already succeeded.
  }
}
