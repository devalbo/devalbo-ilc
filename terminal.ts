// An in-page terminal: the app's command surface, in the browser.
//
// Type `create --title "Buy milk"` and it runs against the same engine the
// buttons drive, through the same generated surface the native CLI reads. It is
// the second consumer of `clispec`, which is what turns "the surface is
// tier-neutral" from a claim into something checked.
//
// MIRRORS `platform/cli`, deliberately — same three inputs (a port, a surface,
// per-method renderers), same order of operations (defaults, fill, required,
// encode). Where it reads oddly for TypeScript, that is usually because the Go
// runner does it that way and the two must not drift: parsing happens BEFORE the
// boundary the parity check guards, so two tiers can each be self-consistent
// while building different requests from identical input.
//
// A SLOT RENDERS; IT NEVER DECIDES (Decision 34). The terminal owns parsing and
// history; the app owns what a response looks like.
import type { Command, Flag } from "./clispec";
import { findCommand, positionals } from "./clispec";
import { encodeRequest, type Values } from "./encode";
import type { EnginePort } from "./port";

/** Prints one response. Undefined means the command prints nothing. */
export type Renderer = (response: Uint8Array) => string;

export type TerminalOptions = {
  port: EnginePort;
  commands: readonly Command[];
  /** method id → renderer. A command with no entry is an error, not silence. */
  render: Record<number, Renderer | undefined>;
  /**
   * Supplies values the user should not have to type — the clock being the
   * standing case, since the engine has no clock capability.
   */
  fill?: (cmd: Command, values: Values) => void;
  /**
   * Reads a `file`-sourced value. On this tier the engine's filesystem IS OPFS,
   * so a path resolves there and `import-fs backup.json` reads the way it looks.
   * Absent means file-sourced flags are refused by name.
   */
  readFile?: (path: string) => Promise<string>;
};

export type Terminal = {
  /** Run one command line, as if typed. Returns everything printed. */
  run(line: string): Promise<string>;
  /** Everything printed so far — the slot's projection. */
  projection(): string;
};

/**
 * Split a command line into argv.
 *
 * Quoting is the only genuinely fiddly part, and it is here rather than in the
 * shared encoder because it is the ONE thing legitimately allowed to differ per
 * tier: a native CLI never sees a raw line, the shell already split it. Anything
 * past this point must behave identically to the Go runner.
 */
export function tokenize(line: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote: '"' | "'" | null = null;
  let started = false;

  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quote) {
      if (c === "\\" && quote === '"' && i + 1 < line.length) {
        cur += line[++i];
      } else if (c === quote) {
        quote = null;
      } else {
        cur += c;
      }
      continue;
    }
    if (c === '"' || c === "'") {
      quote = c;
      started = true; // `--body ""` is an empty value, not an absent one
      continue;
    }
    if (c === " " || c === "\t") {
      if (started) out.push(cur);
      cur = "";
      started = false;
      continue;
    }
    cur += c;
    started = true;
  }
  if (quote) throw new Error(`unbalanced ${quote} quote`);
  if (started) out.push(cur);
  return out;
}

/**
 * Tab completion, entirely from the generated surface.
 *
 * This is the payoff for the surface being DATA rather than a hand-written
 * parser: completing subcommands, then that command's flags, then an enum's
 * permitted values needs no extra source of truth and cannot fall out of step
 * with what the command actually accepts. A hand-written CLI would need a second
 * table, and the second table is always the one that goes stale.
 *
 * Returns the candidates, and the common prefix to insert — the two things a
 * prompt needs and nothing about how they are displayed, which is the widget's
 * business.
 */
export function complete(
  line: string,
  commands: readonly Command[],
): { candidates: string[]; prefix: string } {
  // Tokenizing here would throw on the half-typed quote a user is in the middle
  // of, so completion works on plain whitespace splitting.
  const parts = line.split(/\s+/);
  const atStart = !line.endsWith(" ");
  const word = atStart ? (parts.pop() ?? "") : "";

  let candidates: string[];
  if (parts.length === 0) {
    candidates = commands.map((c) => c.name);
  } else {
    const cmd = findCommand(commands, parts[0]);
    if (!cmd) return { candidates: [], prefix: word };

    const previous = parts[parts.length - 1];
    const flagName = previous.replace(/^--?/, "");
    const flag = (cmd.flags ?? []).find(
      (f) => previous.startsWith("-") && (f.name === flagName || f.short === flagName),
    );
    if (flag?.enumValues?.length) {
      // Positioned on a value for an enum flag: offer what it accepts.
      candidates = [...flag.enumValues];
    } else {
      candidates = (cmd.flags ?? []).map((f) => `--${f.name}`);
    }
  }

  const matches = candidates.filter((c) => c.startsWith(word)).sort();
  return { candidates: matches, prefix: commonPrefix(matches) || word };
}

function commonPrefix(values: string[]): string {
  if (values.length === 0) return "";
  let prefix = values[0];
  for (const v of values.slice(1)) {
    while (!v.startsWith(prefix)) prefix = prefix.slice(0, -1);
  }
  return prefix;
}

/**
 * Run one command from already-parsed values — the step every front end shares.
 *
 * SHARED ON PURPOSE. There are now three ways to build a request: argv on the
 * CLI, a typed line in the terminal, and a form in the inspector. What must NOT
 * differ between them is everything after "here are the values": defaults, the
 * host's fill hook, the required check, source resolution, and the encoding.
 * Those steps decide the request BYTES, and parsing happens before the boundary
 * the parity check guards — so three copies would be three chances to disagree
 * with nothing to notice.
 *
 * Only the part that is genuinely per-front-end stays outside: how a user
 * expresses the values. Tokenizing a line, or filling in fields.
 *
 * The ORDER matches the Go runner exactly, and one bit of it is deliberate:
 * required is checked AFTER fill, so a host supplying the clock satisfies a
 * required `created_at` and nobody is told to type an epoch.
 */
export async function executeCommand(
  cmd: Command,
  values: Values,
  opts: Pick<TerminalOptions, "port" | "render" | "fill" | "readFile">,
): Promise<string> {
  for (const f of cmd.flags ?? []) {
    if (f.default !== undefined && (values[f.name] ?? []).length === 0) {
      values[f.name] = [f.default];
    }
  }
  opts.fill?.(cmd, values);
  for (const f of cmd.flags ?? []) {
    if (f.required && (values[f.name] ?? []).length === 0) {
      throw new Error(`${cmd.name}: --${f.name} is required`);
    }
  }

  await resolveSources(cmd, values, opts.readFile);

  const request = encodeRequest(cmd, values);
  const result = await opts.port.execute(cmd.method, request);
  if (!result.success) {
    // Errors ride the envelope, not exceptions — the same on every tier.
    throw new Error(result.error ?? "(no message)");
  }

  if (!(cmd.method in opts.render)) {
    throw new Error(`${cmd.name}: no renderer registered (method ${cmd.method})`);
  }
  return opts.render[cmd.method]?.(result.output) ?? "";
}

export function createTerminal(opts: TerminalOptions): Terminal {
  const log: string[] = [];
  const say = (s: string) => {
    if (s !== "") log.push(s);
  };

  async function run(line: string): Promise<string> {
    const before = log.length;
    try {
      say(await dispatch(line));
    } catch (e) {
      say(`error: ${(e as Error).message}`);
    }
    return log.slice(before).join("\n");
  }

  async function dispatch(line: string): Promise<string> {
    const argv = tokenize(line);
    if (argv.length === 0) return "";

    const name = argv[0];
    if (name === "help" || name === "-h" || name === "--help") {
      return usage(opts.commands);
    }
    const cmd = findCommand(opts.commands, name);
    if (!cmd) {
      throw new Error(`unknown command ${JSON.stringify(name)} (try \`help\`)`);
    }
    if (argv.includes("-h") || argv.includes("--help")) {
      return commandUsage(cmd);
    }

    const values = parseFlags(cmd, argv.slice(1));
    return executeCommand(cmd, values, opts);
  }

  return { run, projection: () => log.join("\n") };
}

/**
 * Resolve `file` and `stdin` sources.
 *
 * These are NATIVE concepts and this tier has neither a working directory nor
 * standard input, so they need a meaning rather than an accident. A browser's
 * filesystem is OPFS — which is the engine's filesystem here — so a path
 * resolves there. Stdin has no analogue at all and is refused BY NAME: a prompt
 * that quietly hung waiting on a stream nobody can write to would be the worst
 * available outcome.
 */
async function resolveSources(
  cmd: Command,
  values: Values,
  readFile?: (path: string) => Promise<string>,
): Promise<void> {
  for (const f of cmd.flags ?? []) {
    const vals = values[f.name];
    if (!vals || vals.length === 0) continue;

    if (f.source === "stdin") {
      throw new Error(
        `--${f.name} reads stdin, which a browser has no equivalent of — pass a path instead`,
      );
    }
    if (f.source === "file") {
      if (!readFile) {
        throw new Error(`--${f.name} reads a file, and this terminal was given no reader`);
      }
      values[f.name] = await Promise.all(
        vals.map(async (path) => {
          if (path === "-") {
            throw new Error(`--${f.name}: \`-\` means stdin, which this tier has not got`);
          }
          try {
            return await readFile(path);
          } catch (e) {
            throw new Error(`--${f.name}: ${path}: ${(e as Error).message}`);
          }
        }),
      );
    }
  }
}

/** Parse argv into values, honouring both flags and declared positions. */
function parseFlags(cmd: Command, argv: string[]): Values {
  const values: Values = {};
  const byName = new Map<string, Flag>();
  for (const f of cmd.flags ?? []) {
    byName.set(f.name, f);
    if (f.short) byName.set(f.short, f);
  }

  const rest: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const tok = argv[i];
    if (!tok.startsWith("-") || tok === "-") {
      rest.push(tok);
      continue;
    }
    // `--flag=value` and `--flag value` both, because both are typed.
    let name = tok.replace(/^--?/, "");
    let inline: string | undefined;
    const eq = name.indexOf("=");
    if (eq >= 0) {
      inline = name.slice(eq + 1);
      name = name.slice(0, eq);
    }
    const f = byName.get(name);
    if (!f) throw new Error(`${cmd.name}: unknown flag --${name}`);

    let value: string;
    if (inline !== undefined) {
      value = inline;
    } else if (f.kind === "bool") {
      value = "true"; // `--force`, not `--force true`
    } else if (i + 1 < argv.length) {
      value = argv[++i];
    } else {
      throw new Error(`${cmd.name}: --${f.name} needs a value`);
    }
    (values[f.name] ??= []).push(value);
  }

  // Positionals, same rules as the Go runner: a value already given as a flag
  // does not consume a slot, and a repeated positional takes the remainder.
  let i = 0;
  for (const f of positionals(cmd)) {
    if ((values[f.name] ?? []).length > 0) continue;
    if (i >= rest.length) break;
    if (f.repeated) {
      (values[f.name] ??= []).push(...rest.slice(i));
      i = rest.length;
      break;
    }
    values[f.name] = [rest[i++]];
  }
  if (i < rest.length) {
    throw new Error(`${cmd.name}: unexpected argument ${JSON.stringify(rest[i])}`);
  }
  return values;
}

function usage(commands: readonly Command[]): string {
  const width = Math.max(...commands.map((c) => c.name.length), 0);
  const lines = [...commands]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((c) => `  ${c.name.padEnd(width)}  ${c.summary ?? ""}`.trimEnd());
  return ["commands:", ...lines, "", "`<command> -h` for flags"].join("\n");
}

function commandUsage(cmd: Command): string {
  const parts: string[] = [cmd.name];
  for (const f of positionals(cmd)) {
    parts.push(f.repeated ? `[${f.name}...]` : f.required ? `<${f.name}>` : `[${f.name}]`);
  }
  const lines = [cmd.summary ?? "", `usage: ${parts.join(" ")} [flags]`, ""].filter(
    (l, i) => !(i === 0 && l === ""),
  );
  for (const f of cmd.flags ?? []) {
    const bits = [f.help ?? ""];
    if (f.enumValues?.length) bits.push(`one of: ${f.enumValues.join(", ")}`);
    if (f.required) bits.push("(required)");
    if (f.repeated) bits.push("(repeatable)");
    lines.push(`  --${f.name}${f.short ? `, -${f.short}` : ""}  ${bits.join(" ").trim()}`);
  }
  if (cmd.unsupported?.length) {
    lines.push(`  note: ${cmd.unsupported.join(", ")} cannot be set here`);
  }
  return lines.join("\n");
}
