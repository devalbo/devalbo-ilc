// The commands inspector: the app's command surface, browsable and runnable.
//
// The terminal makes the surface USABLE; this makes it VISIBLE. Every fact on
// the page — subcommands, flags, types, required, defaults, enum choices,
// positions, method ids, and the summary text — comes from `commands.proto`. It
// is API documentation that cannot go stale, because there is nothing to keep in
// sync: delete an rpc and its entry disappears.
//
// A FORM IS THE HONEST WEB FRONT END. Decision 28 says each tier builds requests
// its own way; the terminal borrows the CLI's idiom because it is a terminal,
// and a browser's native idiom is a form. Building one from the same `clispec`
// is the strongest evidence the surface is genuinely tier-neutral — flags on one
// tier, fields on another, one schema.
//
// It also shows what a command line CANNOT express. `Unsupported` is carried per
// command (nested messages, maps, floats); a terminal can only mention it, a
// form can render those fields as unsettable and make the gap explicit rather
// than folkloric.
//
// EVERYTHING AFTER "here are the values" IS SHARED with the terminal, via
// `executeCommand` — defaults, fill, required, source resolution, encoding. That
// is what keeps three front ends building identical request bytes, and the parse
// vectors assert it.
import type { Command, Flag } from "./clispec";
import { positionals } from "./clispec";
import type { Values } from "./encode";
import { encodeRequest, shortEnum } from "./encode";
import { executeCommand, type Renderer } from "./terminal";
import { liveSurface } from "./world-manifest";
import type { EnginePort } from "./port";

export type InspectorOptions = {
  port: EnginePort;
  commands: readonly Command[];
  render: Record<number, Renderer | undefined>;
  fill?: (cmd: Command, values: Values) => void;
  readFile?: (path: string) => Promise<string>;
};

export type Inspector = {
  /** Select a command by name, as clicking it would. */
  select(name: string): void;
  /** Set one field's value, as typing into it would. */
  set(flag: string, value: string): void;
  /** Run the selected command with what the form currently holds. */
  run(): Promise<string>;
  /** The request the form would send right now, as hex — for parse vectors. */
  requestHex(): string;
  /** The equivalent command line for what the form holds. */
  commandLine(): string;
  /** Method ids this host actually registered, or null if it could not say. */
  surface(): number[] | null;
  destroy(): void;
};

export function mountInspector(root: HTMLElement, opts: InspectorOptions): Inspector {
  let current: Command | null = null;
  const inputs = new Map<string, HTMLInputElement | HTMLSelectElement>();

  const list = document.createElement("ul");
  list.className = "ilc-insp-list";
  // NOT "command-list": a command named `list` produces `command-list` for its
  // own button, and two elements answering one testid is a test that silently
  // asserts about whichever it found first.
  list.dataset.testid = "command-index";

  const detail = document.createElement("div");
  detail.className = "ilc-insp-detail";
  detail.dataset.testid = "command-detail";

  const output = document.createElement("pre");
  output.className = "ilc-insp-output";
  output.dataset.testid = "command-output";

  root.append(list, detail, output);

  list.replaceChildren(
    ...[...opts.commands]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((cmd) => {
        const li = document.createElement("li");
        const b = document.createElement("button");
        b.textContent = cmd.name;
        b.dataset.testid = `command-${cmd.name}`;
        b.className = "ilc-insp-name";
        b.addEventListener("click", () => select(cmd.name));
        const s = document.createElement("span");
        s.className = "ilc-insp-summary";
        s.textContent = cmd.summary ?? "";
        li.append(b, " ", s);
        li.dataset.command = cmd.name;
        return li;
      }),
  );

  // Mark what THIS host cannot do (§6.4a). Asynchronous and after the fact: the
  // list renders immediately from the generated schema, then the answer from
  // the live registry annotates it. Waiting for the engine before showing
  // anything would make an inspector that is blank until a command round-trips.
  //
  // Marked rather than removed, for the same reason as the CLI: a user who read
  // the docs and finds a command silently missing has no way to learn that this
  // host simply cannot do it.
  // Resolved surface, exposed so a test can tell "everything is available"
  // apart from "the answer never decoded". Both mark nothing, and without this
  // an assertion that no command is struck through would pass just as happily
  // against a broken decoder.
  let surfaceIds: number[] | null = null;

  void markUnavailable();

  // RE-READ when the facts move (§6.4a). The engine announces
  // `ilc.world-manifest-changed` after the surface is already in line with the new
  // manifest, so asking again here cannot race the registration it is reacting
  // to.
  //
  // The event carries only a revision and this deliberately ignores it: acting
  // on a payload would make the event a second source of truth, when the engine
  // is the first one and is sitting right there. Ask it.
  let unsubscribe: (() => void) | null = null;
  void opts.port
    .subscribe?.((topic) => {
      if (topic === "ilc.world-manifest-changed") void markUnavailable();
    })
    .then((off) => {
      unsubscribe = off;
    })
    .catch(() => {});

  async function markUnavailable(): Promise<void> {
    const live = await liveSurface(opts.port);
    if (live === null) return; // the engine cannot say — claim nothing
    surfaceIds = [...live].sort((x, y) => x - y);
    for (const cmd of opts.commands) {
      const li = list.querySelector<HTMLElement>(`[data-command="${cmd.name}"]`);
      if (!li) continue;
      const stale = li.querySelector(".ilc-insp-unavailable");
      if (live.has(cmd.method)) {
        // A capability can come BACK — a browser that regains a storage grant,
        // a host that re-mounts an index. Marking has to be reversible or the
        // first absence is permanent.
        delete li.dataset.unavailable;
        stale?.remove();
        continue;
      }
      li.dataset.unavailable = "true";
      if (stale) continue;
      const note = document.createElement("span");
      note.className = "ilc-insp-unavailable";
      note.dataset.testid = `unavailable-${cmd.name}`;
      note.textContent = "unavailable on this host";
      li.append(" ", note);
    }
  }

  function select(name: string) {
    const cmd = opts.commands.find((c) => c.name === name);
    if (!cmd) throw new Error(`no such command: ${name}`);
    current = cmd;
    inputs.clear();
    output.textContent = "";

    const parts: HTMLElement[] = [];

    const h = document.createElement("h2");
    h.textContent = cmd.name;
    h.dataset.testid = "detail-name";
    parts.push(h);

    if (cmd.summary) {
      const p = document.createElement("p");
      p.textContent = cmd.summary;
      parts.push(p);
    }

    // The facts a developer debugging a request actually wants, and which no
    // other view shows: the permanent wire id and the message it decodes as.
    const meta = document.createElement("p");
    meta.className = "ilc-insp-meta";
    meta.dataset.testid = "detail-meta";
    meta.textContent = `method_id ${cmd.method} · ${cmd.request}`;
    parts.push(meta);

    const positional = new Set(positionals(cmd).map((f) => f.name));
    for (const f of cmd.flags ?? []) {
      parts.push(field(f, positional.has(f.name)));
    }

    if (cmd.unsupported?.length) {
      // Stated, not hidden: a command whose request has fields no front end can
      // set is a gap in the surface, and pretending otherwise is how it stays
      // folklore.
      const warn = document.createElement("p");
      warn.className = "ilc-insp-unsupported";
      warn.dataset.testid = "detail-unsupported";
      warn.textContent = `cannot be set here: ${cmd.unsupported.join(", ")}`;
      parts.push(warn);
    }

    const line = document.createElement("code");
    line.dataset.testid = "detail-commandline";
    line.className = "ilc-insp-cmdline";

    const copy = document.createElement("button");
    copy.textContent = "copy as command line";
    copy.dataset.testid = "detail-copy";
    copy.addEventListener("click", () => {
      void navigator.clipboard?.writeText(commandLine()).catch(() => {});
    });

    const go = document.createElement("button");
    go.textContent = "run";
    go.dataset.testid = "detail-run";
    go.addEventListener("click", () => {
      run().catch(() => {}); // run() already reports into the output pane
    });

    const bar = document.createElement("div");
    bar.className = "ilc-insp-bar";
    bar.append(go, " ", copy, " ", line);
    parts.push(bar);

    detail.replaceChildren(...parts);
    refreshCommandLine();
  }

  function field(f: Flag, isPositional: boolean): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "ilc-insp-field";

    const label = document.createElement("label");
    label.textContent = f.name;
    label.htmlFor = `f-${f.name}`;

    let input: HTMLInputElement | HTMLSelectElement;
    if (f.enumValues?.length) {
      // A select, from the schema's own values — the same list the terminal
      // completes with and the CLI validates against.
      const sel = document.createElement("select");
      // SHORT NAMES, matching what the terminal accepts and what the app's own
      // `default` is written as. A dropdown offering `COLOUR_AMBER` beside a
      // default of `amber` cannot even preselect it.
      const short = shortEnum(f.enumValues);
      f.enumValues.forEach((v, at) => {
        const o = document.createElement("option");
        o.value = short[at];
        o.textContent = short[at];
        sel.append(o);
      });
      // THE DEFAULT, RESOLVED THE SAME WAY THE OPTIONS WERE.
      //
      // An app writes its default in either spelling — platform.proto says
      // `IMPORT_MODE_MERGE`, hello says `amber` — and the options are short. So
      // assigning `f.default` straight in matched nothing whenever the schema
      // used the full name, and the browser silently fell back to the FIRST
      // option: a form that preselected `unspecified` while the schema said
      // `merge`, with nothing to show for it.
      const wanted = f.default
        ? f.enumValues.findIndex(
            (name, at) =>
              name.toLowerCase() === f.default!.toLowerCase() ||
              short[at].toLowerCase() === f.default!.toLowerCase(),
          )
        : -1;
      sel.value = short[wanted >= 0 ? wanted : 0];
      input = sel;
    } else {
      const inp = document.createElement("input");
      inp.type = "text";
      inp.value = f.default ?? "";
      inp.placeholder = placeholderFor(f);
      input = inp;
    }
    input.id = `f-${f.name}`;
    input.dataset.testid = `field-${f.name}`;
    input.addEventListener("input", refreshCommandLine);
    input.addEventListener("change", refreshCommandLine);
    inputs.set(f.name, input);

    const note = document.createElement("span");
    note.className = "ilc-insp-note";
    note.textContent = [
      f.kind,
      isPositional ? `positional ${f.positional}` : null,
      f.required ? "required" : null,
      f.repeated ? "repeatable" : null,
      f.source && f.source !== "literal" ? `from ${f.source}` : null,
      f.short ? `-${f.short}` : null,
      f.help ?? null,
    ]
      .filter(Boolean)
      .join(" · ");

    wrap.append(label, " ", input, " ", note);
    return wrap;
  }

  function placeholderFor(f: Flag): string {
    if (f.source === "file") return "path in OPFS";
    if (f.source === "stdin") return "(stdin — not available in a browser)";
    return f.kind;
  }

  /** What the form currently holds, in the shape every front end produces. */
  function currentValues(): Values {
    const values: Values = {};
    for (const [name, el] of inputs) {
      const v = el.value;
      if (v !== "") values[name] = [v];
    }
    return values;
  }

  function commandLine(): string {
    if (!current) return "";
    const values = currentValues();
    const parts = [current.name];
    for (const f of current.flags ?? []) {
      for (const v of values[f.name] ?? []) {
        parts.push(`--${f.name}`, /\s/.test(v) ? JSON.stringify(v) : v);
      }
    }
    return parts.join(" ");
  }

  function refreshCommandLine() {
    const el = detail.querySelector<HTMLElement>('[data-testid="detail-commandline"]');
    if (el) el.textContent = commandLine();
  }

  async function run(): Promise<string> {
    if (!current) throw new Error("no command selected");
    try {
      // The SAME path the terminal takes. A form that encoded its own request
      // would be a third implementation of the mapping, free to disagree.
      const text = await executeCommand(current, currentValues(), opts);
      output.textContent = text;
      return text;
    } catch (e) {
      const text = `error: ${(e as Error).message}`;
      output.textContent = text;
      return text;
    }
  }

  return {
    select,
    set(flag, value) {
      const el = inputs.get(flag);
      if (!el) throw new Error(`no field ${flag} on ${current?.name ?? "(nothing)"}`);
      el.value = value;
      refreshCommandLine();
    },
    run,
    requestHex() {
      if (!current) return "";
      // NOTE: encodes what the form holds, WITHOUT fill/defaults — those are
      // applied by executeCommand. A vector comparing this to the CLI must
      // therefore supply the same values explicitly, which is the honest
      // comparison anyway.
      const bytes = encodeRequest(current, currentValues());
      return Array.from(bytes)
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");
    },
    commandLine,
    surface: () => surfaceIds,
    destroy() {
      unsubscribe?.();
      list.remove();
      detail.remove();
      output.remove();
    },
  };
}

/** Minimal styling, shipped with the widget. Overridable by an app's own CSS. */
export function inspectorStyles(): string {
  return `
.ilc-insp { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
.ilc-insp-list { list-style: none; margin: 0 0 1rem; padding: 0; }
.ilc-insp-list li { padding: .15rem 0; }
.ilc-insp-name { border: 0; background: none; font: inherit; color: inherit; cursor: pointer; text-decoration: underline; padding: 0; }
.ilc-insp-summary { opacity: .7; }
.ilc-insp-unavailable { opacity: .7; font-style: italic; }
li[data-unavailable] .ilc-insp-name { text-decoration: line-through; }
.ilc-insp-meta { opacity: .7; }
.ilc-insp-field { padding: .2rem 0; }
.ilc-insp-field label { display: inline-block; min-width: 10em; }
.ilc-insp-note { opacity: .6; }
.ilc-insp-unsupported { opacity: .8; font-style: italic; }
.ilc-insp-bar { padding: .5rem 0; }
.ilc-insp-cmdline { opacity: .8; }
.ilc-insp-output { margin: .5rem 0 0; padding: .5rem; white-space: pre-wrap; border-top: 1px solid currentColor; min-height: 2em; }
`.trim();
}
