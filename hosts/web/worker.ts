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

/** The main thread's listener, installed by `subscribe`. */
type Listener = (topic: string, payload: Uint8Array) => void;
let listener: Listener | null = null;

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
  // 1. OPFS → in-memory FileData tree → the WASI root the guest will see.
  _setFileData(await loadTreeFromOPFS());
  // 2. Instantiate only now, so the preopen it captures is the hydrated tree.
  //    Built by `make build-wasm` (TinyGo → jco transpile → frontend/src/wasm).
  engine = (await import("@wasm/engine.component.js")) as unknown as EngineModule;
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
    for (const [topic, payload] of events) deliver(topic, payload);
    return {
      success: r.success === true,
      output: Uint8Array.from(r.output ?? []),
      error: r.error ?? undefined,
    };
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
