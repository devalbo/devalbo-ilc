// Bridge preview2-shim's in-memory browser FS ↔ real OPFS.
//
// Current @bytecodealliance/preview2-shim (browser) does NOT accept a
// FileSystemDirectoryHandle in _setPreopens — it wants a FileData tree
// ({ dir: { name: { source: Uint8Array } } }). We hydrate that tree from OPFS
// before booting the engine, and flush it back after writes so a page reload
// can restore state.

/**
 * @typedef {{ dir?: Record<string, FileDataEntry>, source?: Uint8Array|string }} FileDataEntry
 */

/** @returns {Promise<FileSystemDirectoryHandle>} */
export async function opfsRoot() {
  return navigator.storage.getDirectory();
}

/** Load OPFS into a preview2-shim FileData tree. */
export async function loadTreeFromOPFS(root = null) {
  const dir = root ?? (await opfsRoot());
  /** @type {FileDataEntry} */
  const tree = { dir: {} };
  await readDir(dir, tree);
  return tree;
}

/** Persist a FileData tree into OPFS (creates/overwrites files). */
export async function flushTreeToOPFS(tree, root = null) {
  const dir = root ?? (await opfsRoot());
  await writeDir(dir, tree);
}

/**
 * Parse `_getFileData()` JSON back into a FileData tree with Uint8Array sources.
 * JSON.stringify turns Uint8Array into `{0:n,1:n,…}` objects.
 */
export function reviveFileDataJSON(json) {
  return revive(JSON.parse(json));
}

/** Direct OPFS read for the Playwright assertion (bypass the engine). */
export async function readOPFSText(path, root = null) {
  const dir = root ?? (await opfsRoot());
  const parts = path.replace(/^\/+/, "").split("/").filter(Boolean);
  let handle = dir;
  for (let i = 0; i < parts.length - 1; i++) {
    handle = await handle.getDirectoryHandle(parts[i]);
  }
  const file = await handle.getFileHandle(parts[parts.length - 1]);
  return new TextDecoder().decode(await (await file.getFile()).arrayBuffer());
}

async function readDir(handle, entry) {
  for await (const [name, child] of handle.entries()) {
    if (child.kind === "directory") {
      entry.dir[name] = { dir: {} };
      await readDir(child, entry.dir[name]);
    } else {
      const file = await child.getFile();
      entry.dir[name] = { source: new Uint8Array(await file.arrayBuffer()) };
    }
  }
}

async function writeDir(handle, entry) {
  if (!entry?.dir) return;
  for (const [name, child] of Object.entries(entry.dir)) {
    if (child.dir) {
      const sub = await handle.getDirectoryHandle(name, { create: true });
      await writeDir(sub, child);
    } else {
      const fh = await handle.getFileHandle(name, { create: true });
      const writable = await fh.createWritable();
      const src =
        typeof child.source === "string"
          ? new TextEncoder().encode(child.source)
          : child.source instanceof Uint8Array
            ? child.source
            : new Uint8Array(child.source ?? []);
      await writable.write(src);
      await writable.close();
    }
  }
}

function revive(node) {
  if (node.source !== undefined) {
    if (typeof node.source === "string") return { source: node.source };
    if (Array.isArray(node.source)) return { source: new Uint8Array(node.source) };
    const keys = Object.keys(node.source)
      .map(Number)
      .sort((a, b) => a - b);
    return { source: new Uint8Array(keys.map((k) => node.source[k])) };
  }
  const dir = {};
  for (const [k, v] of Object.entries(node.dir ?? {})) {
    dir[k] = revive(v);
  }
  return { dir };
}
