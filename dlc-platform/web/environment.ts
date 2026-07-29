// The environment manifest, web side (§6.4a, Decision 32) — the worker telling
// the engine what THIS host can do.
//
// The Go counterpart is `platform.Boot`. Two host runtimes means two
// implementations of the same sequence; each is written once here in the
// inherited runtime, not once per app.
//
// WHY THIS MATTERS MORE IN A BROWSER THAN NATIVELY. A native host always has a
// filesystem. A browser one does not: `navigator.storage.getDirectory()` can
// reject — storage denied, some private-browsing modes, older Safari — and
// before this the worker would boot an engine whose every write failed with an
// error the app had no way to anticipate. That is the case that made the
// manifest worth building at all.

import { appendTag, appendVarint } from "./encode";

/** devalbo.ilc.v1.Availability */
export const AVAILABILITY_ABSENT = 1;
export const AVAILABILITY_PRESENT = 2;

/** devalbo.ilc.v1.FilesystemKind */
export const FILESYSTEM_KIND_OPFS = 3;

/** `SetEnvironment` — the core-lifecycle block, id 2. */
export const METHOD_SET_ENVIRONMENT = 2;

const WIRE_VARINT = 0;
const WIRE_BYTES = 2;

export interface Manifest {
  /** Monotonic and NON-ZERO. Zero is rejected by the engine. */
  revision: number;
  filesystem: { available: boolean; kind?: number; ephemeral?: boolean };
}

/**
 * Encode `SetEnvironmentRequest{ environment }`.
 *
 * Hand-encoded for the same reason `encode.ts` is: this package is consumed by
 * a `file:` symlink with no node_modules of its own, so a bare specifier for a
 * generated message would not resolve. Field NUMBERS are the contract and are
 * pinned by the parity vector, not by these names.
 */
export function encodeSetEnvironment(m: Manifest): Uint8Array {
  if (!Number.isInteger(m.revision) || m.revision <= 0) {
    // Refused here as well as in the engine, because a host that shipped a zero
    // would otherwise discover it as a failed command at launch rather than as
    // the programming error it is.
    throw new Error(`environment revision must be a positive integer, got ${m.revision}`);
  }

  const fs: number[] = [];
  appendTag(fs, 1, WIRE_VARINT); // Filesystem.availability
  appendVarint(fs, BigInt(m.filesystem.available ? AVAILABILITY_PRESENT : AVAILABILITY_ABSENT));
  if (m.filesystem.available && m.filesystem.kind !== undefined) {
    appendTag(fs, 2, WIRE_VARINT); // Filesystem.kind
    appendVarint(fs, BigInt(m.filesystem.kind));
  }
  if (m.filesystem.ephemeral) {
    appendTag(fs, 3, WIRE_VARINT); // Filesystem.ephemeral
    appendVarint(fs, 1n);
  }

  const env: number[] = [];
  appendTag(env, 1, WIRE_VARINT); // Environment.revision
  appendVarint(env, BigInt(m.revision));
  appendTag(env, 2, WIRE_BYTES); // Environment.filesystem
  appendVarint(env, BigInt(fs.length));
  env.push(...fs);

  const req: number[] = [];
  appendTag(req, 1, WIRE_BYTES); // SetEnvironmentRequest.environment
  appendVarint(req, BigInt(env.length));
  req.push(...env);
  return Uint8Array.from(req);
}

/**
 * Can this browser actually give us a filesystem?
 *
 * Probes rather than feature-detects. `navigator.storage?.getDirectory` being a
 * function says the API EXISTS; it does not say the user agent will hand over a
 * handle, and the failures that matter here — denied storage, a private window —
 * show up only when you ask. So we ask, once, at startup.
 */
export async function probeOPFS(): Promise<boolean> {
  try {
    await navigator.storage.getDirectory();
    return true;
  } catch {
    // Deliberately not classifying WHY. A quota error, a security error and a
    // missing API all mean the same thing to an engine: there is no filesystem
    // here. Telling them apart would be host-side reasoning that no app can act
    // on differently.
    return false;
  }
}

/** `GetCommandSurface` — core-lifecycle block, id 4. */
export const METHOD_GET_COMMAND_SURFACE = 4;

/**
 * Which commands are registered on THIS host right now.
 *
 * Resolves to null when the engine cannot say, which is a third state and not a
 * default: "available", "not available" and "no answer" are genuinely
 * different, and only the first two justify changing what a user sees.
 *
 * A truthful surface contains the id that answered it. Anything else did not
 * really answer — a scripted fake succeeds at every method and decodes to an
 * empty list, which would otherwise read as "this engine has no commands" and
 * mark every one of them unavailable.
 */
export async function liveSurface(
  port: { execute(method: number, request: Uint8Array): Promise<{ success: boolean; output: Uint8Array }> },
): Promise<Set<number> | null> {
  let res;
  try {
    res = await port.execute(METHOD_GET_COMMAND_SURFACE, new Uint8Array());
  } catch {
    return null;
  }
  if (!res.success) return null;

  const ids = new Set<number>();
  const buf = res.output;
  let i = 0;
  while (i < buf.length) {
    const [tag, tagLen] = readVarint(buf, i);
    if (tagLen < 0) return null;
    i += tagLen;
    const field = tag >>> 3;
    const wire = tag & 7;
    if (field === 1 && wire === 2) {
      // Packed repeated uint32 — the default encoding for a repeated scalar.
      const [len, lenLen] = readVarint(buf, i);
      if (lenLen < 0) return null;
      i += lenLen;
      const end = i + len;
      while (i < end) {
        const [v, n] = readVarint(buf, i);
        if (n < 0) return null;
        ids.add(v);
        i += n;
      }
    } else if (field === 1 && wire === 0) {
      // Unpacked, which a conforming encoder may also emit.
      const [v, n] = readVarint(buf, i);
      if (n < 0) return null;
      ids.add(v);
      i += n;
    } else {
      return null; // an unexpected shape is not an answer we should act on
    }
  }
  return ids.has(METHOD_GET_COMMAND_SURFACE) ? ids : null;
}

function readVarint(buf: Uint8Array, at: number): [number, number] {
  let result = 0;
  let shift = 0;
  for (let i = at; i < buf.length; i++) {
    const b = buf[i];
    result |= (b & 0x7f) << shift;
    if ((b & 0x80) === 0) return [result >>> 0, i - at + 1];
    shift += 7;
    if (shift > 28) return [0, -1];
  }
  return [0, -1];
}
