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

/**
 * Permanent method ids. Dispatch keys on these numbers, so an rpc rename is
 * wire-safe. Mirrored by hand today; `protoc-gen-dlc-registry` will generate
 * them.
 *
 * The ranges are the platform/app boundary and are permanent — per-capability
 * blocks below 1000, the app above it (devalbo/ilc/v1/platform.proto):
 *   1–99      core lifecycle        600–999   reserved
 *   100–199   filesystem            1000+     the app (devalbo/dlc/v1)
 *   200–599   index / events / display / network
 */
export const PlatformMethod = {
  Version: 1,
  ExportFs: 100,
  ImportFs: 101,
  ResetFs: 102,
} as const;

export const Method = {
  New: 1000,
  Echo: 1001,
} as const;

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
