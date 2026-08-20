// WHAT THE APP'S MESSAGES LOOK LIKE — raw, for a page that wants to render them.
//
// # What this is for
//
// A page can already BUILD a request for an app it was never compiled against:
// `clispec` describes every request field, which is what `inspector.ts` turns
// into a form. Reading a RESPONSE stops short — `SpecResult` is flat, so a reply
// carrying a nested message arrives as bytes with no names.
//
// `SchemaInfo` closes that. This module hands it back RAW: the four fields as
// the app declared them, with the fetch-and-verify helper, and no rendering.
// Rendering is deliberately somebody else's job — see the note at the bottom on
// what a renderer would look like and why it is not here yet.
//
// # Prior art
//
// `react-qroma-lib` renders arbitrary protobuf messages in a browser by walking
// `@protobuf-ts` runtime metadata: one component reads `IMessageType.fields[]`,
// branches on `field.kind`, and produces inputs for scalars, enums, nested
// messages and oneofs. That is the proof that generic rendering works from field
// metadata alone.
//
// What it does not do is DISCOVERY: its registry is populated from the app's
// generated TypeScript, imported at build time, so the page must be compiled
// against the same protos as the device. This module is the missing half — the
// device saying what it is, so a page can meet an app it has never seen.

import { execute } from "./api";
import { MethodGetSchema } from "./gen/devalbo/ilc/v1/platform.registry.pb";
import {
  F_GET_SCHEMA_RESPONSE_SCHEMA,
  F_SCHEMA_INFO_DESCRIPTOR,
  F_SCHEMA_INFO_SCHEMA_ID,
  F_SCHEMA_INFO_URL,
  F_SCHEMA_INFO_VERSION,
} from "./gen/devalbo/ilc/v1/platform.fields";

/** What an app declared about its own schema. Every field may be empty. */
export interface SchemaInfo {
  /** Content hash of the descriptor. THE IDENTITY — see platform.proto. */
  schemaId: string;
  /** The app's own version for this schema, e.g. "1.4.0". Free text. */
  version: string;
  /** Where a copy may be fetched. A HINT, verified against `schemaId`. */
  url: string;
  /** A serialized FileDescriptorSet, if the app embedded one. */
  descriptor: Uint8Array;
}

/** Whether an app said anything at all. */
export function declared(info: SchemaInfo): boolean {
  return (
    info.schemaId !== "" ||
    info.version !== "" ||
    info.url !== "" ||
    info.descriptor.length > 0
  );
}

/**
 * Ask the app what its messages look like.
 *
 * ALWAYS ANSWERS. An app that declared nothing returns empty fields rather than
 * failing, so a caller learns "this app does not describe itself" in one round
 * trip instead of by timing out. Use `declared()` to tell the two apart.
 */
export async function schema(): Promise<SchemaInfo> {
  const result = await execute(MethodGetSchema, new Uint8Array());
  const empty: SchemaInfo = {
    schemaId: "",
    version: "",
    url: "",
    descriptor: new Uint8Array(),
  };
  if (!result.success) return empty;
  const inner = fieldBytes(result.output, F_GET_SCHEMA_RESPONSE_SCHEMA);
  if (!inner) return empty;
  return {
    schemaId: text(fieldBytes(inner, F_SCHEMA_INFO_SCHEMA_ID)),
    version: text(fieldBytes(inner, F_SCHEMA_INFO_VERSION)),
    url: text(fieldBytes(inner, F_SCHEMA_INFO_URL)),
    descriptor: fieldBytes(inner, F_SCHEMA_INFO_DESCRIPTOR) ?? new Uint8Array(),
  };
}

// ---------------------------------------------------------------------------
// Where schemas are hosted
// ---------------------------------------------------------------------------

/**
 * How to turn a schema identity into a URL, given a base route.
 *
 * # Why a convention rather than a free-form URL per app
 *
 * Because the alternative is every app inventing a layout, and every page
 * hard-coding the one it happens to know. A base route plus a rule means a page
 * can serve any app from one origin — which is the case a device fleet actually
 * produces: many apps, one schema host.
 *
 * # Why the ID is in the path
 *
 * So the URL is CONTENT-ADDRESSED and therefore immutable. A path that named
 * only the app (`/schemas/tictactoe.binpb`) resolves to whatever is there now,
 * and when that drifts from the firmware a page renders wrong names over correct
 * bytes with nothing to say so. Putting `schemaId` in the path means a hit is
 * the right schema by construction, and it makes the response cacheable forever.
 */
export interface SchemaHost {
  /** Base route, e.g. "https://schemas.example.com/dlc" or "/schemas". */
  base: string;
  /** File extension for the descriptor. Defaults to ".binpb". */
  extension?: string;
}

/**
 * The URL a schema should live at under `host`.
 *
 * Trailing slashes on the base are tolerated: a base is something a person
 * types into a config file, and "it worked until I added a slash" is a bad way
 * to spend an afternoon.
 */
export function schemaUrl(host: SchemaHost, schemaId: string): string {
  const base = host.base.replace(/\/+$/, "");
  const ext = host.extension ?? ".binpb";
  return `${base}/${encodeURIComponent(schemaId)}${ext}`;
}

/** What a fetch produced, and how much of it can be trusted. */
export type Resolution =
  | { kind: "embedded"; descriptor: Uint8Array }
  | { kind: "fetched"; descriptor: Uint8Array }
  | { kind: "unverified"; descriptor: Uint8Array; reason: string }
  | { kind: "none"; reason: string };

/**
 * Get the descriptor for `info`, embedded first, then over the network.
 *
 * # The verification is the point
 *
 * A fetched descriptor is hashed and compared against `schemaId`. A mismatch
 * does NOT throw and does not silently succeed — it returns `unverified` with
 * the reason, because the bytes may still be useful and the caller is the one
 * who gets to decide. What must never happen is a page showing verified and
 * unverified names identically: "I checked these" and "I am guessing" are
 * different claims, and a reader cannot tell them apart from the field names
 * alone.
 *
 * # Why `fetch` is injectable
 *
 * So this is testable without a network, and so a page can supply its own —
 * a cache, an offline bundle, an authenticated origin. The default is the
 * platform `fetch`.
 */
export async function resolveDescriptor(
  info: SchemaInfo,
  host?: SchemaHost,
  fetcher: typeof fetch = fetch,
): Promise<Resolution> {
  if (info.descriptor.length > 0) {
    // NO VERIFICATION NEEDED: it came from the app itself, over the same channel
    // as everything else it has told us. Hashing it would only check that the
    // app agrees with itself.
    return { kind: "embedded", descriptor: info.descriptor };
  }

  const url = info.url || (host && info.schemaId ? schemaUrl(host, info.schemaId) : "");
  if (!url) {
    return { kind: "none", reason: "the app declared no descriptor and no url" };
  }

  let bytes: Uint8Array;
  try {
    const response = await fetcher(url);
    if (!response.ok) {
      return { kind: "none", reason: `${url}: HTTP ${response.status}` };
    }
    bytes = new Uint8Array(await response.arrayBuffer());
  } catch (err) {
    // OFFLINE IS NOT AN ERROR STATE HERE. A bench with no network has lost an
    // optimisation, not correctness — the caller falls back to the flat spec.
    return { kind: "none", reason: `${url}: ${String(err)}` };
  }

  if (!info.schemaId) {
    return {
      kind: "unverified",
      descriptor: bytes,
      reason: "the app declared no schema_id, so nothing could be checked",
    };
  }
  const got = await sha256Hex(bytes);
  if (got !== info.schemaId) {
    return {
      kind: "unverified",
      descriptor: bytes,
      reason: `schema_id mismatch: app says ${info.schemaId}, ${url} hashes to ${got}`,
    };
  }
  return { kind: "fetched", descriptor: bytes };
}

/** SubtleCrypto SHA-256, hex. Available in every browser that has WebSerial. */
async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", bytes as BufferSource);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

// ---------------------------------------------------------------------------
// A minimal reader, because this module must not depend on a protobuf runtime
// ---------------------------------------------------------------------------
//
// `SchemaInfo` is four fields and the reply is one nested message. Pulling in a
// decoder for that would put a dependency into a package whose whole job is to
// stay small — and the field NUMBERS are generated, so this cannot drift from
// the .proto the way a hand-written decoder would.

/** The bytes of a length-delimited field, or null. */
function fieldBytes(buffer: Uint8Array, field: number): Uint8Array | null {
  let at = 0;
  while (at < buffer.length) {
    const [tag, tagLen] = varint(buffer, at);
    if (tagLen === 0) return null;
    at += tagLen;
    const number = tag >>> 3;
    const wire = tag & 7;
    if (wire === 2) {
      const [len, lenLen] = varint(buffer, at);
      if (lenLen === 0) return null;
      at += lenLen;
      if (number === field) return buffer.subarray(at, at + len);
      at += len;
    } else if (wire === 0) {
      const [, n] = varint(buffer, at);
      if (n === 0) return null;
      at += n;
    } else if (wire === 5) {
      at += 4;
    } else if (wire === 1) {
      at += 8;
    } else {
      // An unknown wire type means the rest is unparseable; stop rather than
      // guess a length and read someone else's bytes.
      return null;
    }
  }
  return null;
}

function varint(buffer: Uint8Array, at: number): [number, number] {
  let value = 0;
  let shift = 0;
  let read = 0;
  while (at + read < buffer.length) {
    const byte = buffer[at + read];
    value |= (byte & 0x7f) << shift;
    read += 1;
    if ((byte & 0x80) === 0) return [value >>> 0, read];
    shift += 7;
    if (shift > 28) return [0, 0];
  }
  return [0, 0];
}

function text(bytes: Uint8Array | null): string {
  return bytes ? new TextDecoder().decode(bytes) : "";
}

// ---------------------------------------------------------------------------
// WHAT IS DELIBERATELY NOT HERE
// ---------------------------------------------------------------------------
//
// A renderer. Turning a descriptor into a form or a data view needs a protobuf
// runtime that can build message types at RUNTIME — `@bufbuild/protobuf`'s
// `createFileRegistry`, or `@protobuf-ts`'s reflection, which is what qroma
// walks. Both are real dependencies with real weight, and which one a page wants
// depends on what it already uses.
//
// So this package hands back the descriptor and stops. A page that wants names
// does:
//
//     const info = await schema();
//     const got  = await resolveDescriptor(info, { base: "/schemas" });
//     if (got.kind === "none") { /* fall back to clispec's flat results */ }
//     // else feed got.descriptor to the protobuf runtime of your choice,
//     // and SHOW whether it was verified — see `Resolution`.
//
// Raw first is the honest order: the transport and the verification are the
// parts that have to be right, and they are the parts a page cannot reasonably
// write itself.
