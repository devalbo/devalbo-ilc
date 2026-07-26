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
import {
  _getFileData,
  _setFileData,
} from "@bytecodealliance/preview2-shim/filesystem";
import {
  clearOPFS,
  flushTreeToOPFS,
  listOPFS,
  loadTreeFromOPFS,
  reviveFileDataJSON,
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
  await flushTreeToOPFS(reviveFileDataJSON(_getFileData()));
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
    const r = mod.execute(method, request);
    await flush();
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
   * then writes into the old tree while `_getFileData()` returns the new one,
   * and the flush silently persists the wrong thing — which looks exactly like
   * "clear worked, then my import vanished".
   *
   * One instance per page load is therefore the contract. Re-instantiation
   * without a reload needs `jco transpile --instantiation`, which exports a
   * factory instead of a live module; worth doing when a flow needs it.
   */
  async clearStorage(): Promise<void> {
    await clearOPFS();
  },
};

export type WorkerApi = typeof api;

Comlink.expose(api);
