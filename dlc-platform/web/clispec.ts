// The command surface, in TypeScript — the web tier's mirror of
// `engine/platform/clispec` (Decision 29).
//
// Same data, same generator, second consumer. That is the point: the generated
// surface CLAIMS to be tier-neutral, and until a language other than Go read it
// that claim was untested.
//
// GENERATED FILES IMPORT NOTHING. The Go side keeps `clispec` a leaf package
// because generated code lives in the message package and must not reach back;
// the TypeScript equivalent goes further and has the generator emit plain object
// literals with STRING kinds (`"string"`, not an enum member), so a generated
// surface is valid on its own with no import at all. These types describe that
// shape structurally — TypeScript checks it without the generated file ever
// naming them.

/** A flag's wire type. Strings, so generated data needs no import. */
export type Kind =
  | "string"
  | "bool"
  | "int32"
  | "int64"
  | "uint32"
  | "uint64"
  | "enum"
  | "bytes";

/**
 * Where a value comes from, as opposed to what type it is.
 *
 * `file` and `stdin` are NATIVE concepts, and this tier has neither a working
 * directory nor standard input. See `resolveValue` in `terminal.ts` for what
 * they mean here — the short version is that a browser's filesystem is OPFS, so
 * a path resolves there, and stdin is refused by name rather than hanging.
 */
export type Source = "literal" | "file" | "stdin";

export type Flag = {
  name: string;
  short?: string;
  /** The proto field NUMBER — what the encoder writes, never the name. */
  field: number;
  kind: Kind;
  source?: Source;
  repeated?: boolean;
  /** 1-based argument position, absent for flag-only. */
  positional?: number;
  help?: string;
  required?: boolean;
  default?: string;
  enumValues?: readonly string[];
};

export type Command = {
  name: string;
  method: number;
  request: string;
  summary?: string;
  flags?: readonly Flag[];
  /** Request fields the command line cannot express (nested messages, maps). */
  unsupported?: readonly string[];
};

/** A command's positional fields, in position order. */
export function positionals(cmd: Command): Flag[] {
  return (cmd.flags ?? [])
    .filter((f) => (f.positional ?? 0) > 0)
    .sort((a, b) => (a.positional ?? 0) - (b.positional ?? 0));
}

export function findCommand(
  commands: readonly Command[],
  name: string,
): Command | undefined {
  return commands.find((c) => c.name === name);
}
