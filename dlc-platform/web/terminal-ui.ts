// The terminal WIDGET — output area, prompt, history.
//
// Separate from `terminal.ts` on purpose: that file is the command surface
// (tokenize, parse, encode, dispatch) and touches no DOM, which is why it can be
// tested with no browser and compared byte-for-byte against the Go runner. This
// file is the part that only makes sense on a screen.
//
// Deliberately NOT xterm.js. The value here is the command surface, not terminal
// emulation — there is no ANSI to render, no curses, no job control, and a
// dependency that provides them would be answering a question nobody asked. If
// those ever become necessary, the terminal has stopped being a command surface
// and become an emulator, and that is the moment to reconsider rather than now.
import { complete, createTerminal, type TerminalOptions } from "./terminal";
import { readOPFSText } from "./opfs";

export type MountOptions = Omit<TerminalOptions, "readFile"> & {
  /**
   * Reads a `file`-sourced flag. Defaults to OPFS, which is the right answer on
   * this tier: the engine's filesystem IS OPFS, so `import-fs backup.json`
   * resolves against the same tree the command will write into.
   */
  readFile?: (path: string) => Promise<string>;
  /** Lines printed before the first prompt. */
  banner?: string[];
  /**
   * Focus the prompt on mount. Default true.
   *
   * Right for a terminal ROUTE, where the prompt is the whole page and having to
   * click before typing is friction with no purpose. Switch it off when the
   * terminal is a panel beside something else, since stealing focus from a form
   * the user was filling in is worse than one extra click.
   */
  autofocus?: boolean;
};

export type MountedTerminal = {
  /** Run a line as if typed — for tests and for `window.term`. */
  run(line: string): Promise<string>;
  /** Everything printed so far. */
  projection(): string;
  destroy(): void;
};

export function mountTerminal(
  root: HTMLElement,
  opts: MountOptions,
): MountedTerminal {
  const term = createTerminal({
    ...opts,
    readFile: opts.readFile ?? ((path) => readOPFSText(path)),
  });

  const out = document.createElement("pre");
  out.dataset.testid = "terminal-output";
  out.className = "ilc-terminal-output";

  const prompt = document.createElement("span");
  prompt.textContent = "> ";
  prompt.className = "ilc-terminal-prompt";

  const input = document.createElement("input");
  input.type = "text";
  input.spellcheck = false;
  input.autocomplete = "off";
  input.dataset.testid = "terminal-input";
  input.className = "ilc-terminal-input";
  input.setAttribute("aria-label", "command");

  const line = document.createElement("div");
  line.className = "ilc-terminal-line";
  line.append(prompt, input);

  root.append(out, line);
  if (opts.autofocus !== false) input.focus();

  const print = (text: string) => {
    if (text === "") return;
    out.textContent += text + "\n";
    out.scrollTop = out.scrollHeight;
  };
  for (const b of opts.banner ?? []) print(b);

  // History: Up/Down through what was typed. Cheap, and the first thing missing
  // from a prompt that has none.
  const history: string[] = [];
  let cursor = 0;

  async function submit(raw: string) {
    const text = raw.trim();
    // Echo before running, so a slow command still shows what it is doing and
    // the transcript reads like a session rather than a result list.
    print("> " + text);
    if (text === "") return;
    history.push(text);
    cursor = history.length;

    // Disabled while running: a second command entered mid-flight would
    // interleave its output with the first, and the engine is one instance.
    input.disabled = true;
    try {
      print(await term.run(text));
    } finally {
      input.disabled = false;
      input.focus();
    }
  }

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Tab") {
      // Always preventDefault, even with no candidates: Tab's default is to
      // move focus off the prompt, which in a terminal is never what was meant.
      e.preventDefault();
      const { candidates, prefix } = complete(input.value, opts.commands);
      if (candidates.length === 0) return;
      // Replace only the word being typed, so completing a flag does not eat
      // the command in front of it.
      const head = input.value.replace(/\S*$/, "");
      input.value = head + prefix + (candidates.length === 1 ? " " : "");
      if (candidates.length > 1) print(candidates.join("  "));
      return;
    }
    if (e.key === "Enter") {
      const text = input.value;
      input.value = "";
      void submit(text);
      return;
    }
    if (e.key === "ArrowUp" && cursor > 0) {
      e.preventDefault();
      input.value = history[--cursor] ?? "";
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      cursor = Math.min(cursor + 1, history.length);
      input.value = history[cursor] ?? "";
    }
  };
  input.addEventListener("keydown", onKey);

  // Clicking anywhere in the terminal focuses the prompt, as a terminal does.
  const onClick = () => {
    if (!window.getSelection()?.toString()) input.focus();
  };
  root.addEventListener("click", onClick);

  return {
    run: async (line: string) => {
      await submit(line);
      return term.projection();
    },
    projection: () => term.projection(),
    destroy() {
      input.removeEventListener("keydown", onKey);
      root.removeEventListener("click", onClick);
      out.remove();
      line.remove();
    },
  };
}

/**
 * Minimal styling, injected once.
 *
 * Shipped with the widget rather than left to each app: a terminal that does not
 * use a monospace font or scroll its output is not really a terminal, and every
 * app rediscovering that is the sort of thing a runtime package exists to
 * prevent. Everything here is overridable by an app's own CSS.
 */
export function terminalStyles(): string {
  return `
.ilc-terminal { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
.ilc-terminal-output { margin: 0; padding: .5rem; max-height: 24em; overflow: auto; white-space: pre-wrap; word-break: break-word; }
.ilc-terminal-line { display: flex; align-items: center; padding: 0 .5rem .5rem; }
.ilc-terminal-prompt { opacity: .6; }
.ilc-terminal-input { flex: 1; border: 0; outline: 0; background: transparent; font: inherit; color: inherit; }
.ilc-terminal-input:disabled { opacity: .5; }
`.trim();
}
