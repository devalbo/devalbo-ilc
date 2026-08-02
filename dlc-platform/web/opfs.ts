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

/**
 * Top-level OPFS entries the ENGINE never sees — reserved for the HOST.
 *
 * WHY A HOST NEEDS THIS. A host owns things an app must not: which user is
 * driving, what they last selected, anything remembered between sessions.
 * Natively that is trivial — a host is an ordinary program and writes to
 * ~/.config. In a browser the host and the engine share one OPFS, and this
 * bridge hydrates EVERYTHING under the root into the engine's tree, so
 * host-owned state would become app data: visible to any command, and carried
 * inside `export-fs` bundles that are supposed to be portable between people.
 *
 * Skipped on BOTH sides, and the flush side is the one that matters. `writeDir`
 * mirrors — it deletes whatever OPFS has that the tree does not — so a directory
 * the engine never hydrated would be deleted on the next command. Reading past
 * it is a nicety; not deleting it is the requirement.
 *
 * TOP LEVEL ONLY. A `.ilc-host` directory nested deeper is ordinary app data and
 * is treated as such — the reservation is a property of the root, not of the
 * name.
 *
 * NAMESPACED so an app cannot plausibly collide, which matters because the
 * failure would be silent: an engine that wrote `.ilc-host/x` would find it
 * dropped on flush rather than refused. `.ilc-index` (docs/INDEX-PLAN.md D9)
 * joins this list if a host-provided store ever lands.
 */
export const HOST_RESERVED: readonly string[] = [".ilc-host"];

export async function opfsRoot(): Promise<FileSystemDirectoryHandle> {
  return navigator.storage.getDirectory();
}

/** Load OPFS into a preview2-shim FileData tree. */
export async function loadTreeFromOPFS(
  root?: FileSystemDirectoryHandle,
): Promise<FileDataEntry> {
  const dir = root ?? (await opfsRoot());
  const tree: FileDataEntry = { dir: {} };
  await readDir(dir, tree, HOST_RESERVED);
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
  await writeDir(dir, tree, HOST_RESERVED);
}

/**
 * A cheap content digest of the engine's in-memory tree.
 *
 * WHY THIS EXISTS: the host flushes the whole tree after every command, because
 * the engine cannot say whether it wrote — which means a READ used to rewrite
 * every file and, once tabs could hear each other, told every other tab their
 * store had changed. Opening a second tab invalidated the first one for doing
 * nothing but listing.
 *
 * So the host asks a question it CAN answer: is the tree different from the one
 * I last persisted? Same answer, no cooperation from the engine needed.
 *
 * FNV-1a over paths and bytes rather than `JSON.stringify(_getFileData())`,
 * which the shim itself warns against: stringify expands every Uint8Array into
 * `{0:n,1:n,…}` and was pathologically slow on a scaffold-sized tree. This walks
 * the live tree once, allocating nothing.
 *
 * A digest can collide in principle. The consequence of a collision is a skipped
 * flush, so the bar is "not by accident" rather than "not by an adversary" —
 * and lengths, names and bytes all feed it, which no plausible edit survives.
 */
export function treeFingerprint(entry: FileDataEntry): string {
  let h = 0x811c9dc5;
  const mix = (byte: number) => {
    h ^= byte;
    // >>> 0 keeps it an unsigned 32-bit value; Math.imul is the standard way to
    // get a 32-bit multiply out of a float64 without losing the low bits.
    h = Math.imul(h, 0x01000193) >>> 0;
  };
  const mixString = (s: string) => {
    for (let i = 0; i < s.length; i++) mix(s.charCodeAt(i) & 0xff);
    mix(0);
  };
  const walk = (node: FileDataEntry) => {
    if (node.dir) {
      // Sorted, so iteration order cannot change the digest for an unchanged
      // tree — the same reason every other ordered output in this project sorts.
      for (const name of Object.keys(node.dir).sort()) {
        mixString(name);
        walk(node.dir[name]);
      }
      return;
    }
    const source = node.source ?? new Uint8Array();
    const bytes =
      typeof source === "string" ? new TextEncoder().encode(source) : source;
    mixString(String(bytes.length));
    for (let i = 0; i < bytes.length; i++) mix(bytes[i]);
  };
  walk(entry);
  return h.toString(16);
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
  // Only the top-level call passes these; recursion deliberately does not, so
  // the reservation applies to the root and not to every directory.
  skip: readonly string[] = [],
): Promise<void> {
  for await (const [name, child] of (handle as any).entries()) {
    if (skip.includes(name)) continue;
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
  skip: readonly string[] = [],
): Promise<void> {
  const wanted = entry.dir ?? {};

  // Prune first: anything OPFS still has that the tree dropped. Collect names
  // before removing — mutating a directory while iterating its async entries is
  // asking for trouble.
  const present: string[] = [];
  for await (const [name] of (handle as any).entries()) present.push(name);
  for (const name of present) {
    // The reservation's whole job: the engine's tree never contained this, so
    // without the guard the mirror would delete the host's own state.
    if (skip.includes(name)) continue;
    if (!(name in wanted)) {
      await handle.removeEntry(name, { recursive: true });
    }
  }

  for (const [name, child] of Object.entries(wanted)) {
    if (skip.includes(name)) continue;
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
