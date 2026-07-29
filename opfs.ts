// OPFS ↔ preview2-shim bridge.
//
// The engine writes with plain Go `os`, which TinyGo lowers to WASI calls
// against whatever root the host preopens (§5.2). In the browser that root is
// OPFS — but `@bytecodealliance/preview2-shim` (browser build) does NOT accept a
// FileSystemDirectoryHandle in `_setPreopens`: it wants an in-memory FileData
// tree (`{ dir: { name: { source: Uint8Array } } }`). So we hydrate that tree
// from OPFS before booting the engine and flush it back after writes, which is
// what makes a scaffold survive a page reload.
//
// Lifted from `spikes/opfs/opfs-bridge.js` (Spike 3, ✅ GREEN) — the spike is the
// proof, this is the product copy.

export interface FileDataEntry {
  dir?: Record<string, FileDataEntry>;
  source?: Uint8Array | string;
}

export async function opfsRoot(): Promise<FileSystemDirectoryHandle> {
  return navigator.storage.getDirectory();
}

/** Load OPFS into a preview2-shim FileData tree. */
export async function loadTreeFromOPFS(
  root?: FileSystemDirectoryHandle,
): Promise<FileDataEntry> {
  const dir = root ?? (await opfsRoot());
  const tree: FileDataEntry = { dir: {} };
  await readDir(dir, tree);
  return tree;
}

/**
 * Persist a FileData tree into OPFS, MIRRORING it: entries OPFS has but the
 * tree does not are removed.
 *
 * Write-only flushing looks fine until something deletes. The in-memory tree is
 * authoritative once the engine has run — it was hydrated from OPFS at boot and
 * mutated since — so an entry missing from it means the engine removed it. A
 * flush that only added would leave `reset-fs` and `import --replace` reporting
 * success while the deleted files quietly came back on the next reload.
 */
export async function flushTreeToOPFS(
  tree: FileDataEntry,
  root?: FileSystemDirectoryHandle,
): Promise<void> {
  const dir = root ?? (await opfsRoot());
  await writeDir(dir, tree);
}

/**
 * Parse `_getFileData()` JSON back into a FileData tree with Uint8Array sources.
 * JSON.stringify turns a Uint8Array into `{0:n,1:n,…}`, so it needs reviving.
 */
export function reviveFileDataJSON(json: string): FileDataEntry {
  return revive(JSON.parse(json));
}

/** Direct OPFS read, bypassing the engine — used by tests and the file browser. */
export async function readOPFSText(
  path: string,
  root?: FileSystemDirectoryHandle,
): Promise<string> {
  const dir = root ?? (await opfsRoot());
  const parts = path.replace(/^\/+/, "").split("/").filter(Boolean);
  let handle: FileSystemDirectoryHandle = dir;
  for (let i = 0; i < parts.length - 1; i++) {
    handle = await handle.getDirectoryHandle(parts[i]);
  }
  const file = await handle.getFileHandle(parts[parts.length - 1]);
  return new TextDecoder().decode(await (await file.getFile()).arrayBuffer());
}

/** Flat, sorted list of every file path in OPFS — for showing the scaffolded tree. */
export async function listOPFS(
  root?: FileSystemDirectoryHandle,
  prefix = "",
): Promise<string[]> {
  const dir = root ?? (await opfsRoot());
  const out: string[] = [];
  for await (const [name, child] of (dir as any).entries()) {
    const path = prefix ? `${prefix}/${name}` : name;
    if (child.kind === "directory") {
      out.push(...(await listOPFS(child as FileSystemDirectoryHandle, path)));
    } else {
      out.push(path);
    }
  }
  return out.sort();
}

/** Delete everything in OPFS — the browser equivalent of a fresh directory. */
export async function clearOPFS(root?: FileSystemDirectoryHandle): Promise<void> {
  const dir = root ?? (await opfsRoot());
  const names: string[] = [];
  for await (const [name] of (dir as any).entries()) names.push(name);
  for (const name of names) {
    await dir.removeEntry(name, { recursive: true });
  }
}

async function readDir(
  handle: FileSystemDirectoryHandle,
  entry: FileDataEntry,
): Promise<void> {
  for await (const [name, child] of (handle as any).entries()) {
    if (child.kind === "directory") {
      entry.dir![name] = { dir: {} };
      await readDir(child as FileSystemDirectoryHandle, entry.dir![name]);
    } else {
      const file = await (child as FileSystemFileHandle).getFile();
      entry.dir![name] = { source: new Uint8Array(await file.arrayBuffer()) };
    }
  }
}

async function writeDir(
  handle: FileSystemDirectoryHandle,
  entry: FileDataEntry,
): Promise<void> {
  const wanted = entry.dir ?? {};

  // Prune first: anything OPFS still has that the tree dropped. Collect names
  // before removing — mutating a directory while iterating its async entries is
  // asking for trouble.
  const present: string[] = [];
  for await (const [name] of (handle as any).entries()) present.push(name);
  for (const name of present) {
    if (!(name in wanted)) {
      await handle.removeEntry(name, { recursive: true });
    }
  }

  for (const [name, child] of Object.entries(wanted)) {
    if (child.dir) {
      const sub = await handle.getDirectoryHandle(name, { create: true });
      await writeDir(sub, child);
    } else {
      const fileHandle = await handle.getFileHandle(name, { create: true });
      const writable = await fileHandle.createWritable();
      const source = child.source ?? new Uint8Array();
      await writable.write(
        typeof source === "string" ? new TextEncoder().encode(source) : source,
      );
      await writable.close();
    }
  }
}

function revive(node: any): FileDataEntry {
  if (node && typeof node === "object" && "dir" in node) {
    const dir: Record<string, FileDataEntry> = {};
    for (const [name, child] of Object.entries(node.dir ?? {})) {
      dir[name] = revive(child);
    }
    return { dir };
  }
  if (node && typeof node === "object" && "source" in node) {
    const src = node.source;
    if (typeof src === "string") return { source: new TextEncoder().encode(src) };
    // JSON turned the Uint8Array into {0:n,1:n,…} — rebuild it in index order.
    const bytes = Object.keys(src)
      .map(Number)
      .sort((a, b) => a - b)
      .map((k) => src[k]);
    return { source: new Uint8Array(bytes) };
  }
  return { dir: {} };
}

/** One entry in the store: its path and how big it is. */
export interface OPFSEntry {
  path: string;
  size: number;
}

/**
 * Every file in OPFS with its size, sorted.
 *
 * Separate from `listOPFS` rather than replacing it: the flush path only needs
 * names, and opening every file to stat it is work that path should not pay.
 */
export async function listOPFSEntries(
  root?: FileSystemDirectoryHandle,
  prefix = "",
): Promise<OPFSEntry[]> {
  const dir = root ?? (await opfsRoot());
  const out: OPFSEntry[] = [];
  for await (const [name, child] of (dir as any).entries()) {
    const path = prefix ? `${prefix}/${name}` : name;
    if (child.kind === "directory") {
      out.push(...(await listOPFSEntries(child as FileSystemDirectoryHandle, path)));
    } else {
      const file = await (child as FileSystemFileHandle).getFile();
      out.push({ path, size: file.size });
    }
  }
  return out.sort((a, b) => a.path.localeCompare(b.path));
}

/**
 * Raw bytes of one file.
 *
 * The store is "one JSON file per record" today, but a bundle can carry binary
 * (BFT base64-encodes it precisely because some files are not text), so a
 * viewer that assumed UTF-8 would render mojibake and call it content.
 */
export async function readOPFSBytes(
  path: string,
  root?: FileSystemDirectoryHandle,
): Promise<Uint8Array> {
  const dir = root ?? (await opfsRoot());
  const parts = path.replace(/^\/+/, "").split("/").filter(Boolean);
  let handle: FileSystemDirectoryHandle = dir;
  for (let i = 0; i < parts.length - 1; i++) {
    handle = await handle.getDirectoryHandle(parts[i]);
  }
  const file = await handle.getFileHandle(parts[parts.length - 1]);
  return new Uint8Array(await (await file.getFile()).arrayBuffer());
}
