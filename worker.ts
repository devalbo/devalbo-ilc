// Web host worker — runs the SAME engine the CLI links natively, here as the
// wasip2 component under jco, with its WASI filesystem root bound to OPFS.
//
// Why a worker: OPFS sync access handles want one, and it keeps engine calls off
// the UI thread. The main thread talks to this through Comlink and never touches
// the engine directly (`api.ts` is the only door).
//
// TypeScript here is GLUE ONLY — no business logic. Anything that decides what a
// command means belongs in `engine/`, where every tier shares it.
//
// Boot ordering is load-bearing (Spike 3): hydrate OPFS into the shim's FileData
// tree BEFORE importing the transpiled component. The guest snapshots its
// preopen descriptor at instantiation, so a later `_setFileData` is invisible to
// a live engine — which is also why `reboot()` exists rather than a re-hydrate.
import * as Comlink from "comlink";

// The sink the transpiled component imports as `devalbo:ilc/events` (mapped by
// `dlc build web`). Importing it here gives the worker the SAME module instance
// the engine calls into — ES modules are singletons per graph — so installing a
// forwarder here is what makes the engine's events reachable.
import { setForwarder } from "./events";
import {
  _getFileDataTree,
  _setFileData,
} from "@bytecodealliance/preview2-shim/filesystem";
import {
  encodeSetEnvironment,
  probeOPFS,
  FILESYSTEM_KIND_OPFS,
  METHOD_SET_ENVIRONMENT,
} from "./environment";
import {
  clearOPFS,
  flushTreeToOPFS,
  listOPFS,
  loadTreeFromOPFS,
  type FileDataEntry,
} from "./opfs";

/** Mirrors the WIT `command-result` record. */
export interface CommandResult {
  success: boolean;
  output: Uint8Array;
  error?: string;
}

interface EngineModule {
  execute(method: number, request: Uint8Array): {
    success: boolean;
    output?: ArrayLike<number>;
    error?: string;
  };
}

let engine: EngineModule | null = null;

// The manifest revision in force. The HOST owns this counter: the engine treats
// an unchanged revision as a deliberate no-op (it re-runs capability
// registration), so a host that reused a number would silently fail to update
// the facts and never learn it had.
let envRevision = 0;

/** The main thread's listener, installed by `subscribe`. */
type Listener = (topic: string, payload: Uint8Array) => void;
let listener: Listener | null = null;

/**
 * The main thread's FLUSH listener, installed by `onFlush`.
 *
 * Deliberately NOT an engine event. Events are what the ENGINE announces about
 * its own domain (Decision 33); this is the HOST reporting that it has persisted
 * the tree to OPFS. Anything watching the FILESYSTEM — a file browser, a future
 * sync — wants this one, because a flush happens after every `execute` whereas
 * an event fires only when a handler chooses to emit. Inferring "the filesystem
 * moved" from "the app said something happened" is app-coupled reasoning, and a
 * command that writes without emitting would slip past it.
 */
let flushListener: (() => void) | null = null;

// Non-null only while a command is on the engine's stack — see `execute`.
let duringCommand: Array<[string, Uint8Array]> | null = null;

function deliver(topic: string, payload: Uint8Array): void {
  if (!listener) return;
  // NOT awaited, deliberately: `listener` is a Comlink proxy, so calling it
  // posts a message and returns a Promise for the main thread's reply, which
  // nothing here wants. Awaiting it would stall the worker on the UI thread.
  // The catch keeps a dead port (an unsubscribed page, a closed tab) from
  // surfacing as an unhandled rejection that reads like an engine fault.
  void Promise.resolve(listener(topic, payload)).catch(() => {});
}

// Installed ONCE, at module load rather than in `subscribe`, so events emitted
// before anyone subscribed are dropped here instead of piling up — and so the
// ordering rule below holds no matter when a listener arrives.
setForwarder((topic, payload) => {
  if (duringCommand) duringCommand.push([topic, payload]);
  else deliver(topic, payload); // spontaneous: no command to be durable yet
});

async function boot(): Promise<EngineModule> {
  if (engine) return engine;

  // 1. Ask whether this browser will actually give us a filesystem. Not a
  //    formality: getDirectory() rejects on denied storage and in some private
  //    windows, and until this probe existed the worker booted an engine whose
  //    every write failed with an error the app had no way to anticipate.
  const hasFilesystem = await probeOPFS();

  // 2. OPFS → in-memory FileData tree → the WASI root the guest will see.
  //    With no OPFS there is nothing to hydrate FROM, and the empty tree is
  //    what the guest gets: the engine still runs, it just has nowhere durable
  //    to write — which is exactly what the manifest below is about to say.
  _setFileData(hasFilesystem ? await loadTreeFromOPFS() : { dir: {} });

  // 3. Instantiate only now, so the preopen it captures is the hydrated tree.
  //    Built by `make build-wasm` (TinyGo → jco transpile → frontend/src/wasm).
  const mod = (await import("@wasm/engine.component.js")) as unknown as EngineModule;

  // 4. State the facts BEFORE any other command (docs/ENVIRONMENT-PLAN.md
  //    §2.5). Ordering is a correctness requirement, not a convention: an app
  //    on RegisterDiscovered registers its filesystem verbs FROM this, so a
  //    command sent first would find export-fs missing. Revision 1 — this is
  //    launch, so there is nothing to be newer than.
  envRevision = 1;
  const res = mod.execute(
    METHOD_SET_ENVIRONMENT,
    encodeSetEnvironment({
      revision: envRevision,
      filesystem: hasFilesystem
        ? { available: true, kind: FILESYSTEM_KIND_OPFS }
        : { available: false },
      // No index on this tier yet. Stated rather than omitted; see the Manifest
      // type. SLATED FOR REVERT — docs/INDEX-PLAN.md D8.
      index: { available: false },
    }),
  );
  if (!res.success) {
    // Fail loudly rather than continue with an engine that half-exists: every
    // command after this would fail for a reason that looks nothing like the
    // cause.
    throw new Error(`engine rejected the environment manifest: ${res.error ?? "unknown"}`);
  }

  engine = mod;
  return engine;
}

/** Persist whatever the engine wrote back into OPFS. */
async function flush(): Promise<void> {
  // Live tree, not JSON.stringify — see _getFileDataTree in the vendored shim.
  await flushTreeToOPFS(_getFileDataTree() as FileDataEntry);
}

const api = {
  /**
   * The real boundary (Decisions 28/31): a permanent method_id plus a flat
   * proto-encoded request. The UI builds the request; parsing is host-side, so
   * nothing here inspects what the command means.
   *
   * Flushes to OPFS after every call. That is deliberately conservative for the
   * bootstrap — the engine cannot yet signal that it wrote anything, so
   * flush-always is the only correct option. Revisit when the Events capability
   * can report a write.
   */
  async execute(method: number, request: Uint8Array): Promise<CommandResult> {
    const mod = await boot();
    // Collect this command's events instead of forwarding them as they fire.
    // The engine emits mid-command, BEFORE the flush below has persisted
    // anything; a listener that heard `ilc.data-changed` and immediately called
    // `listFiles` would race the flush and read a half-written OPFS. Holding
    // them until the write is durable makes the host's promise a real one: an
    // event never arrives before the change it announces.
    //
    // No `await` between here and the capture — `mod.execute` is synchronous —
    // so two overlapping `execute` calls cannot interleave into each other's
    // batch.
    duringCommand = [];
    let r: ReturnType<EngineModule["execute"]>;
    try {
      r = mod.execute(method, request);
    } catch (e) {
      duringCommand = null; // a trap must not leave the batch open forever
      throw e;
    }
    const events = duringCommand;
    duringCommand = null;
    await flush();
    // Flush signal first: a watcher told "the filesystem moved" must find it
    // moved, and the same ordering argument applies here as to events below.
    if (flushListener) {
      void Promise.resolve(flushListener()).catch(() => {});
    }
    for (const [topic, payload] of events) deliver(topic, payload);
    return {
      success: r.success === true,
      output: Uint8Array.from(r.output ?? []),
      error: r.error ?? undefined,
    };
  },

  /**
   * Re-state the facts, because something changed (§6.4a, plan D4).
   *
   * The host is the only party that can notice a capability appearing or
   * disappearing, so the engine cannot poll for this and does not try — a query
   * would be a second boundary, and on this tier a synchronous one that could
   * not await an OPFS probe anyway.
   *
   * WHAT IS MISSING, stated rather than implied: nothing here triggers
   * automatically yet. The browser gives no event for "your OPFS went away", so
   * this exists to be CALLED — by a page, a test, or a future storage watcher.
   * The re-send path being real and exercised is what makes it safe to rely on
   * when a trigger does arrive.
   *
   * Revision is assigned here, never by the caller: an off-by-one from a caller
   * would be a silent no-op, and the counter has exactly one owner.
   */
  async setEnvironment(hasFilesystem: boolean): Promise<CommandResult> {
    const mod = await boot();
    envRevision += 1;
    duringCommand = [];
    const r = mod.execute(
      METHOD_SET_ENVIRONMENT,
      encodeSetEnvironment({
        revision: envRevision,
        filesystem: hasFilesystem
          ? { available: true, kind: FILESYSTEM_KIND_OPFS }
          : { available: false },
        index: { available: false },
      }),
    );
    const events = duringCommand;
    duringCommand = null;
    // No flush: a manifest writes nothing. The events still go out, because
    // `ilc.environment-changed` is exactly how a slot learns to re-read.
    for (const [topic, payload] of events) deliver(topic, payload);
    return {
      success: r.success === true,
      output: Uint8Array.from(r.output ?? []),
      error: r.error ?? undefined,
    };
  },

  /**
   * Hear about every OPFS flush — a host fact, not an app one.
   *
   * One listener, like `subscribe`: the main thread fans out.
   */
  async onFlush(fn: () => void): Promise<void> {
    flushListener = fn;
  },

  /** Flat list of every file in OPFS — how the UI shows the scaffolded tree. */
  async listFiles(): Promise<string[]> {
    return listOPFS();
  },

  /**
   * Wipe OPFS. The caller MUST reload the page afterwards — `api.reset()` does.
   *
   * The engine cannot be rebound to a new filesystem root once it is running,
   * and not for want of trying: `await import()` is ES-module cached, so
   * "dropping" the instance and importing again hands back the *same* module,
   * still holding the preopen descriptor it captured on first instantiation.
   * Meanwhile `_setFileData` swaps the shim's tree underneath it. The engine
   * then writes into the old tree while flush reads the new one, and the flush
   * silently persists the wrong thing — which looks exactly like "clear worked,
   * then my import vanished".
   *
   * One instance per page load is therefore the contract. Re-instantiation
   * without a reload needs `jco transpile --instantiation`, which exports a
   * factory instead of a live module; worth doing when a flow needs it.
   */
  async clearStorage(): Promise<void> {
    await clearOPFS();
  },

  /**
   * Register the main thread's event listener. ONE per worker — `api.ts` keeps
   * the subscriber list and fans out on the main thread, so a second call here
   * replaces the relay rather than adding to it.
   *
   * `fn` arrives as a `Comlink.proxy`, so calling it POSTS A MESSAGE rather than
   * running main-thread code here. That is the whole safety story: the engine is
   * on the stack when an event fires, and a listener that called back into
   * `execute` would re-enter it mid-command. A message boundary makes that
   * structurally impossible instead of merely forbidden.
   */
  async subscribe(fn: Listener): Promise<void> {
    listener = fn;
  },
};

export type WorkerApi = typeof api;

Comlink.expose(api);
