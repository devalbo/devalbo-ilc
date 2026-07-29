// Environment-agnostic adapter — the ONLY path from the UI to the engine.
//
// The UI builds a typed request with the generated es-lite messages, this hands
// (method_id, bytes) to the worker, and the response comes back as bytes the UI
// decodes. Parsing/menus are host-side (Decision 28), so the shape of a command
// is defined once in `commands.proto` and shared by every tier.
//
// Keeping this the single door is what lets the same React UI run against a
// different backing later (a desktop host, or a wasm runtime other than jco)
// without touching a component.
import * as Comlink from "comlink";
import type { CommandResult, WorkerApi } from "./worker";

export type { CommandResult };

// NO METHOD IDS LIVE HERE. They are generated per-proto by
// protoc-gen-dlc-registry (`*.registry.pb.ts`) and imported by the app:
//
//	import { MethodGreet } from "@gen/myapp/v1/commands.registry.pb";
//	import { MethodExportFs } from "@gen/devalbo/ilc/v1/platform.registry.pb";
//
// Two reasons. An app's ids are the app's, so a shared host package holding them
// would be a layering inversion — this package would need to know every app that
// ever exists. And hand-mirroring numbers into TypeScript is precisely the hole
// the generator closed for Go; reopening it here would mean an id lives in two
// places again, with nothing checking they agree.

let worker: Worker | null = null;
let remote: Comlink.Remote<WorkerApi> | null = null;

function connect(): Comlink.Remote<WorkerApi> {
  if (remote) return remote;
  worker = new Worker(new URL("./worker.ts", import.meta.url), {
    type: "module",
  });
  remote = Comlink.wrap<WorkerApi>(worker);
  return remote;
}

/** Run one command. Errors ride the result envelope, not exceptions. */
export async function execute(
  method: number,
  request: Uint8Array,
): Promise<CommandResult> {
  return connect().execute(method, request);
}

/**
 * Tell the engine the facts changed (§6.4a).
 *
 * Nothing calls this automatically: the browser has no "your OPFS went away"
 * event. It is the re-send path, kept real and exercised so that it can be
 * trusted the day a trigger exists.
 */
export async function setEnvironment(hasFilesystem: boolean): Promise<CommandResult> {
  return connect().setEnvironment(hasFilesystem);
}

/**
 * Listen for engine events (§6.3). Returns an unsubscribe.
 *
 * This is how a UI stops polling or hand-refreshing: the engine announces that
 * something changed, and the view re-reads. It fires for writes this tab did not
 * make — another command path, an `import-fs`, eventually a sync — which is
 * precisely what a manual `refresh()` after your own command cannot do.
 *
 * The callback runs on the MAIN thread, delivered by message from the worker, so
 * it may safely call `execute` again. (Inside the worker that would be
 * re-entrancy; across a message boundary it is just another command.)
 */
export async function subscribe(
  fn: (topic: string, payload: Uint8Array) => void,
): Promise<() => void> {
  listeners.add(fn);
  // ONE relay proxy for the worker, however many subscribers there are, because
  // the worker holds a single listener: handing it each `fn` directly would mean
  // the second subscriber silently evicted the first. Fan-out happens here, on
  // the main thread, where a listener is just a function call.
  //
  // Registered lazily and only once — a React app mounting three components that
  // each subscribe must not open three proxies (each is a retained MessagePort).
  attached ??= connect().subscribe(
    Comlink.proxy((topic: string, payload: Uint8Array) => {
      // Snapshot: a listener may unsubscribe (or subscribe) from inside its own
      // callback, and mutating the set mid-iteration would skip a sibling.
      for (const listener of [...listeners]) {
        try {
          listener(topic, payload);
        } catch (e) {
          // One broken subscriber must not silence the others. Logged rather
          // than swallowed — this is main-thread application code, where a
          // thrown error is a bug someone can fix, not an engine hazard.
          console.error(`ilc: event listener for "${topic}" threw`, e);
        }
      }
    }),
  );
  await attached;
  return () => {
    listeners.delete(fn);
    // The relay stays registered even at zero listeners: re-subscribing later
    // is then free, and an idle relay costs one no-op call per event.
  };
}

const listeners = new Set<(topic: string, payload: Uint8Array) => void>();
let attached: Promise<void> | null = null;

/**
 * Hear about every OPFS flush. Returns an unsubscribe.
 *
 * NOT an engine event, and the distinction is the point. `subscribe` carries
 * what the ENGINE announces about its own domain (Decision 33) — an app's topic,
 * an app's payload, meaningful only if you know the app. This carries a HOST
 * fact: the tree has been persisted.
 *
 * Anything watching the FILESYSTEM rather than the application wants this one. A
 * flush happens after every `execute`; an event fires only when a handler
 * chooses to emit. So a file browser driven by events is really inferring "the
 * filesystem may have moved" from "the app said something happened" — which is
 * app-coupled reasoning, and misses a command that writes without emitting.
 *
 * Fires AFTER the write is durable, same ordering guarantee as events.
 */
export async function onFlush(fn: () => void): Promise<() => void> {
  flushListeners.add(fn);
  flushAttached ??= connect().onFlush(
    Comlink.proxy(() => {
      for (const listener of [...flushListeners]) {
        try {
          listener();
        } catch (e) {
          console.error("ilc: flush listener threw", e);
        }
      }
    }),
  );
  await flushAttached;
  return () => {
    flushListeners.delete(fn);
  };
}

const flushListeners = new Set<() => void>();
let flushAttached: Promise<void> | null = null;

// NO TOPIC CONSTANTS HERE, deliberately.
//
// `TopicDataChanged = "ilc.data-changed"` used to live in this file AND in
// `engine/platform/events.go` — the hand-mirroring AGENTS.md §1 bans for method
// ids, which events escaped only because they predated the rule having teeth.
//
// Both sides now read one `(topic)` declaration on the message, and the
// generated bindings carry it: import `DataChangedEventTopic` from
// `@gen/devalbo/ilc/v1/platform.events.pb`, which `dlc gen` puts in every app's
// tree. It is not re-exported from here on purpose — this package must not
// depend on an app's `@gen` alias, which would point the runtime at the
// application instead of the other way round.

/** Every file currently in OPFS, sorted. */
export async function listFiles(): Promise<string[]> {
  return connect().listFiles();
}

/**
 * Wipe OPFS and reload the page.
 *
 * The reload is not optional housekeeping — it is how the engine gets a fresh
 * filesystem root. A running component holds the preopen it captured at
 * instantiation and cannot be rebound (see `worker.ts`), so clearing storage
 * without restarting leaves an engine writing into a tree nobody will persist.
 */
export async function reset(): Promise<void> {
  await connect().clearStorage();
  location.reload();
}
