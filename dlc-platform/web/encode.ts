// Building request bytes from parsed command-line values — the TypeScript
// counterpart of `engine/platform/cli/encode.go`.
//
// THE INVARIANT THIS FILE EXISTS TO KEEP: the same command line must produce the
// same request bytes on every tier that has one. Parsing happens *before* the
// boundary the parity check guards — parity compares command results, written
// filesystems and event streams, all downstream of a request that already exists
// — so two tiers could each be internally consistent while building different
// requests from identical input, with every check green. That is what the parse
// vectors (hosts/native/parsevector_test.go + test/terminal.spec.ts) pin down.
//
// WHY THIS IMPORTS NOTHING. An earlier version borrowed es-lite's wire writer,
// which meant a bare specifier — and this package is consumed by `file:` symlink
// with no node_modules of its own, so Vite resolved it from the real path and
// failed. The fix at the time was a Vite alias onto `dist/binary.js`, which
// reaches past the package's exports map into its internals and hardcodes a
// layout we do not control: it would break on a version bump with a "file not
// found" nowhere near the cause. So the wire encoding is written here instead.
// It is about forty lines, protobuf's scalar encoding has not changed in fifteen
// years, and the parse vectors are what guard it — which was the real answer to
// "don't implement a varint twice" all along.
import type { Command, Flag } from "./clispec";

/** Values collected from the command line, keyed by flag name. */
export type Values = Record<string, string[]>;

const WIRE_VARINT = 0;
const WIRE_BYTES = 2;

/**
 * Encode a request from resolved values.
 *
 * Fields go out in ascending field-number order. Proto does not require it, but
 * a deterministic encoding is what lets a parse vector compare two hosts' bytes
 * at all.
 */
export function encodeRequest(cmd: Command, values: Values): Uint8Array {
  const out: number[] = [];
  const flags = [...(cmd.flags ?? [])].sort((a, b) => a.field - b.field);

  for (const f of flags) {
    const vals = values[f.name];
    if (!vals || vals.length === 0) continue;
    if (!f.repeated && vals.length > 1) {
      throw new Error(`--${f.name} given ${vals.length} times but is not repeated`);
    }
    for (const raw of vals) appendField(out, f, raw);
  }
  return Uint8Array.from(out);
}

function appendField(out: number[], f: Flag, raw: string): void {
  switch (f.kind) {
    case "string":
    case "bytes": {
      // Same wire type; `bytes` differs only in where the value came from,
      // which the caller has already resolved.
      const body = new TextEncoder().encode(raw);
      appendTag(out, f.field, WIRE_BYTES);
      appendVarint(out, BigInt(body.length));
      for (const b of body) out.push(b);
      return;
    }

    case "bool": {
      if (raw !== "true" && raw !== "false" && raw !== "1" && raw !== "0") {
        throw new Error(`--${f.name}: ${JSON.stringify(raw)} is not a boolean`);
      }
      appendTag(out, f.field, WIRE_VARINT);
      appendVarint(out, raw === "true" || raw === "1" ? 1n : 0n);
      return;
    }

    case "int32":
    case "int64": {
      appendTag(out, f.field, WIRE_VARINT);
      appendVarint(out, twosComplement(parseInt64(f, raw)));
      return;
    }

    case "uint32":
    case "uint64": {
      const n = parseInt64(f, raw);
      if (n < 0n) {
        throw new Error(
          `--${f.name}: ${JSON.stringify(raw)} is not a non-negative integer`,
        );
      }
      appendTag(out, f.field, WIRE_VARINT);
      appendVarint(out, n);
      return;
    }

    case "enum": {
      // BY NAME, resolved to the number the APP DECLARED.
      //
      // This used to resolve to the name's POSITION, and said so: "proto3's
      // first value must be zero and the generator emits them in declaration
      // order." Only the first half is a rule. proto3 requires the FIRST value
      // to be zero; every value after it is the author's choice, so
      //
      //   PROBLEM_UNSPECIFIED = 0; PROBLEM_DIVIDE_BY_ZERO = 7;
      //
      // encoded `divide_by_zero` as 1 — a legal-looking enum the app never
      // declared. `enumNumbers` carries the real values, emitted from the same
      // loop over the descriptor that produced the names; falling back to the
      // position keeps a spec generated before that field existed working.
      //
      // SHORT NAMES TOO: the spec carries proto's full value names
      // (`COLOUR_AMBER`) and a person types `amber`. Matching only the full name
      // made an app's OWN DECLARED DEFAULT unusable as an argument.
      const values = f.enumValues ?? [];
      const short = shortEnum(values);
      const i = values.findIndex(
        (name, at) =>
          name.toLowerCase() === raw.toLowerCase() ||
          short[at].toLowerCase() === raw.toLowerCase(),
      );
      if (i < 0) {
        throw new Error(
          `--${f.name}: ${JSON.stringify(raw)} is not one of ${short.join(", ")}`,
        );
      }
      const numbers = f.enumNumbers ?? [];
      appendTag(out, f.field, WIRE_VARINT);
      appendVarint(out, BigInt(i < numbers.length ? numbers[i] : i));
      return;
    }
  }
}

/**
 * BigInt, not Number: an epoch second fits in a double but a millisecond
 * timestamp does not, and silently losing precision on a 64-bit field is the
 * kind of divergence that surfaces years later.
 */
function parseInt64(f: Flag, raw: string): bigint {
  if (!/^[+-]?\d+$/.test(raw.trim())) {
    throw new Error(`--${f.name}: ${JSON.stringify(raw)} is not an integer`);
  }
  return BigInt(raw.trim());
}

/**
 * Signed ints are varint-encoded as 64-bit two's complement — so -1 is ten
 * bytes, not one, and an int32 is sign-extended to 64 bits before encoding.
 *
 * This is the one genuinely non-obvious rule in protobuf's scalar encoding, and
 * it is why the parse vectors include a negative value: getting it wrong on one
 * tier only is precisely the divergence they exist to catch.
 */
function twosComplement(n: bigint): bigint {
  return n < 0n ? (1n << 64n) + n : n;
}

export function appendTag(out: number[], field: number, wire: number): void {
  appendVarint(out, BigInt((field << 3) | wire));
}

export function appendVarint(out: number[], n: bigint): void {
  let v = n;
  while (v > 0x7fn) {
    out.push(Number((v & 0x7fn) | 0x80n));
    v >>= 7n;
  }
  out.push(Number(v));
}

/**
 * Drops the prefix every value of an enum shares.
 *
 *   PROBLEM_UNSPECIFIED, PROBLEM_DIVIDE_BY_ZERO, PROBLEM_OVERFLOW
 *   -> unspecified, divide_by_zero, overflow
 *
 * THE COMMON PREFIX, not "everything after the last underscore" — that rule
 * turns `PROBLEM_DIVIDE_BY_ZERO` into `zero`, which is a different word rather
 * than an abbreviation. "Before the first underscore" fails the other way on an
 * enum whose own name is two words (`SPEC_KIND_ENUM` would keep `KIND_ENUM`).
 *
 * The twin of `clispec.ShortEnum` in the Go half. Two implementations of one
 * rule is a cost; the alternative is the web tier importing Go, which is not a
 * thing.
 */
export function shortEnum(values: readonly string[]): string[] {
  const out = values.slice();
  if (values.length < 2) return out;

  let prefix = values[0];
  for (const value of values.slice(1)) {
    while (!value.startsWith(prefix)) {
      prefix = prefix.slice(0, -1);
      if (prefix === "") return out;
    }
  }
  // Back to a word boundary: two values sharing `PROBLEM_OVER` share more
  // characters than words, and cutting there leaves `flow` and `draft`.
  const at = prefix.lastIndexOf("_");
  if (at < 0) return out;
  prefix = prefix.slice(0, at + 1);

  return out.map((value) => {
    const trimmed = value.slice(prefix.length);
    return trimmed === "" ? value : trimmed.toLowerCase();
  });
}
