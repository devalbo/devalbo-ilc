// The spike proper. Everything here runs in a DEDICATED WORKER, which is where
// the engine runs in the real web host (dlc-platform/web/worker.ts) and also the
// only place `createSyncAccessHandle` has ever been available.
//
// The claim under test (SQLITE-INDEX-PLAN.md D2): a query can be answered with
// NO await in the call path, so a synchronous component import can return rows.
// Everything else in the plan rests on it.

import sqlite3InitModule from "@sqlite.org/sqlite-wasm";

let poolUtil = null;
let db = null;

// ---- init: async, and that is fine ----------------------------------------
//
// Loading the wasm and opening the SyncAccessHandle pool are both async. That
// costs nothing: the worker's boot sequence is already async (probe → hydrate →
// instantiate → manifest). Only the QUERY has to be synchronous, because that is
// what happens inside a component call.
async function init() {
  const sqlite3 = await sqlite3InitModule({ print: () => {}, printErr: () => {} });
  if (typeof sqlite3.installOpfsSAHPoolVfs !== "function") {
    throw new Error("no installOpfsSAHPoolVfs in this build");
  }
  poolUtil = await sqlite3.installOpfsSAHPoolVfs({ name: "ilc-spike" });
  db = new poolUtil.OpfsSAHPoolDb("/index.sqlite3");
  return {
    version: sqlite3.version.libVersion,
    // Recorded, not required: if this is false and the queries still work, the
    // SAH-pool VFS needs no COOP/COEP headers, which is the difference between
    // "add a dependency" and "impose cross-origin isolation on every ILC app".
    crossOriginIsolated: globalThis.crossOriginIsolated === true,
  };
}

// ---- the synchronous query path -------------------------------------------

/** The whole point: no `async`, no `await`, returns rows. */
function querySync(sql, bind) {
  return db.exec({ sql, bind, rowMode: "array", returnValue: "resultRows" });
}

/**
 * Run a query and prove nothing awaited, the way the async spike proved the
 * opposite with a tick counter.
 *
 * A microtask queued immediately before the call cannot run until the current
 * synchronous execution finishes. So if `ranMicrotask` is true by the time
 * querySync returns, control reached the event loop — which means something
 * awaited, which means this cannot be a component import. False is the pass.
 */
function queryWithMicrotaskProbe(sql, bind) {
  let ranMicrotask = false;
  queueMicrotask(() => {
    ranMicrotask = true;
  });
  const rows = querySync(sql, bind);
  return { rows, ranMicrotask };
}

// ---- what the VFS puts in OPFS --------------------------------------------
//
// The engine's root IS the OPFS root, and the host mirrors a FileData tree onto
// it. Whatever the pool creates here is something that mirror has to know about.
async function listOpfsRoot() {
  const root = await navigator.storage.getDirectory();
  const names = [];
  for await (const name of root.keys()) names.push(name);
  names.sort();
  return names;
}

/**
 * Reproduce what `loadTreeFromOPFS` does at boot: walk everything under the root
 * and read each file's bytes into an in-memory tree.
 *
 * The pool's files are held open with SyncAccessHandles, so this asks the
 * question Phase 3 needs answered before it starts: does merely INSTALLING the
 * VFS break the existing hydrate, and does the index end up inside `export-fs`
 * bundles (it must not — a disposable index has no business travelling).
 */
async function hydrateProbe() {
  const root = await navigator.storage.getDirectory();
  const read = [];
  const failed = [];
  async function walk(dir, prefix) {
    for await (const [name, handle] of dir.entries()) {
      const path = prefix ? `${prefix}/${name}` : name;
      if (handle.kind === "directory") {
        await walk(handle, path);
        continue;
      }
      try {
        const file = await handle.getFile();
        read.push({ path, bytes: file.size });
      } catch (err) {
        failed.push(`${path}: ${err}`);
      }
    }
  }
  await walk(root, "");
  return { read, failed };
}

/**
 * The other half of the flush: `writeDir` calls `createWritable` on EVERY file
 * in the tree, every time. Since the hydrate above pulls the pool's files INTO
 * the tree, the flush will try to rewrite files the pool holds open — so this is
 * the call the real host would actually make after every command.
 */
async function writeProbe(path) {
  const root = await navigator.storage.getDirectory();
  const parts = path.split("/");
  let dir = root;
  for (const part of parts.slice(0, -1)) {
    dir = await dir.getDirectoryHandle(part);
  }
  const handle = await dir.getFileHandle(parts[parts.length - 1]);
  try {
    const writable = await handle.createWritable();
    await writable.write(new Uint8Array([0]));
    await writable.close();
    return { wrote: true, error: null };
  } catch (err) {
    return { wrote: false, error: String(err) };
  }
}

/**
 * Reproduce what `flushTreeToOPFS` does today: it MIRRORS, deleting any OPFS
 * entry the in-memory tree does not have. The engine's tree will never contain
 * the pool directory, so today's flush would delete the live index.
 *
 * Destructive — the harness runs it last.
 */
async function mirrorFlushHazard(knownNames) {
  const root = await navigator.storage.getDirectory();
  const attempted = [];
  const removed = [];
  const refused = [];
  for await (const name of root.keys()) {
    if (knownNames.includes(name)) continue;
    attempted.push(name);
    try {
      await root.removeEntry(name, { recursive: true });
      removed.push(name);
    } catch (err) {
      refused.push(`${name}: ${err}`);
    }
  }
  let queryError = null;
  try {
    querySync("select count(*) from notes");
  } catch (err) {
    queryError = String(err);
  }
  return { attempted, removed, refused, queryError };
}

// ---- a tiny RPC so Playwright can drive this ------------------------------

const ops = {
  async init() {
    return init();
  },
  async seed() {
    querySync("create table if not exists notes(id text primary key, title text)");
    querySync("delete from notes");
    // Deliberately inserted out of order, so an ORDER BY that silently did
    // nothing would be visible in the result.
    for (const [id, title] of [
      ["c", "cherry"],
      ["a", "apple"],
      ["b", "banana"],
    ]) {
      querySync("insert into notes(id,title) values(?,?)", [id, title]);
    }
    return { count: querySync("select count(*) from notes")[0][0] };
  },
  async ordered() {
    return queryWithMicrotaskProbe("select id,title from notes order by title");
  },
  async reopened() {
    // No seed this time: the rows must come off OPFS from the previous page.
    return queryWithMicrotaskProbe("select id,title from notes order by title");
  },
  async listOpfs() {
    return listOpfsRoot();
  },
  async hydrate() {
    return hydrateProbe();
  },
  async write(path) {
    return writeProbe(path);
  },
  async hazard(knownNames) {
    return mirrorFlushHazard(knownNames);
  },
};

self.onmessage = async (event) => {
  const { id, op, args } = event.data;
  try {
    self.postMessage({ id, ok: true, value: await ops[op](...(args ?? [])) });
  } catch (err) {
    self.postMessage({ id, ok: false, error: String(err?.stack ?? err) });
  }
};
