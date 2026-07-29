// A file browser for OPFS — a window onto the store the engine writes.
//
// WHY THIS BELONGS IN THE PLATFORM. §7.1's central claim is that *the files are
// the truth*: one readable file per record, inspectable without the app. In a
// terminal you check that with `ls` and `cat`. In a browser, OPFS has no Finder,
// so on this tier the architecture's main promise has been unverifiable by eye —
// you had to take it on faith or export a bundle. This makes it visible.
//
// It also answers the question the terminal cannot. "What is in there now?" —
// after an `import-fs --replace`, or when a command wrote something unexpected —
// is a filesystem question, not a command question.
//
// READ-ONLY, AND NOT MERELY OUT OF CAUTION. On this tier the web host hydrates
// the whole OPFS tree into an in-memory FileData structure BEFORE instantiating
// the component, and flushes that tree back after every `execute`. Two
// consequences, the second of which is the decisive one:
//
//   1. A write made directly to OPFS is invisible to the running engine — it is
//      reading its own in-memory copy.
//   2. The next command FLUSHES, and `writeDir` prunes anything OPFS holds that
//      the in-memory tree does not. So an externally written file is not just
//      ignored, it is DELETED by the next command that runs.
//
// A file editor here would therefore lose the user's edit at the next keystroke
// elsewhere in the app, silently. Editing goes through the engine —
// `import-fs`, which emits `ilc.data-changed` like any other write — so the
// in-memory tree is what changes and the flush persists it.
import { listOPFSEntries, readOPFSBytes, type OPFSEntry } from "./opfs";

export type FilesOptions = {
  /**
   * Hear about OPFS flushes — `onFlush` from `@devalbo/dlc-web/api`.
   *
   * THE RIGHT SIGNAL FOR A WATCHER, and the reason it is not `subscribe`. This
   * page observes the FILESYSTEM; it knows nothing about what an app's commands
   * mean, and should not have to. Engine events are app-domain facts —
   * `notes.record-changed` means something only if you know notes — so a
   * filesystem view driven by them is inferring "files probably moved" from "the
   * app said something happened".
   *
   * They also come apart in practice: a flush happens after EVERY `execute`,
   * while an event fires only when a handler chooses to emit. A command that
   * writes without emitting moves the store with no event at all, and an
   * event-driven watcher would sit there showing a stale tree.
   */
  onFlush?: (fn: () => void) => Promise<() => void>;
};

export type FilesBrowser = {
  refresh(): Promise<void>;
  /** What the browser is showing, as text — the slot's projection. */
  projection(): string;
  destroy(): void;
};

export function mountFiles(root: HTMLElement, opts: FilesOptions = {}): FilesBrowser {
  let entries: OPFSEntry[] = [];
  let selected: string | null = null;

  const bar = document.createElement("div");
  bar.className = "ilc-files-bar";
  const summary = document.createElement("span");
  summary.dataset.testid = "files-summary";
  const reload = document.createElement("button");
  reload.textContent = "refresh";
  reload.dataset.testid = "files-refresh";
  bar.append(summary, " ", reload);

  const list = document.createElement("ul");
  list.className = "ilc-files-list";
  list.dataset.testid = "files-list";

  const viewer = document.createElement("pre");
  viewer.className = "ilc-files-viewer";
  viewer.dataset.testid = "files-viewer";

  root.append(bar, list, viewer);

  async function show(path: string) {
    selected = path;
    const bytes = await readOPFSBytes(path);
    viewer.textContent = decode(bytes, path);
    for (const el of list.querySelectorAll("li")) {
      el.classList.toggle("selected", el.dataset.path === path);
    }
  }

  async function refresh() {
    entries = await listOPFSEntries();
    const total = entries.reduce((n, e) => n + e.size, 0);
    summary.textContent =
      entries.length === 0
        ? "(empty — no files in OPFS)"
        : `${entries.length} file(s), ${bytes(total)}`;

    list.replaceChildren(
      ...entries.map((e) => {
        const li = document.createElement("li");
        li.dataset.path = e.path;
        if (e.path === selected) li.classList.add("selected");

        const open = document.createElement("button");
        open.className = "ilc-files-name";
        open.textContent = e.path;
        open.dataset.testid = `file-${e.path}`;
        open.addEventListener("click", () => {
          show(e.path).catch((err) => {
            viewer.textContent = `error: ${(err as Error).message}`;
          });
        });

        const size = document.createElement("span");
        size.className = "ilc-files-size";
        size.textContent = bytes(e.size);

        const dl = document.createElement("a");
        dl.textContent = "download";
        dl.dataset.testid = `download-${e.path}`;
        dl.href = "#";
        dl.addEventListener("click", (ev) => {
          ev.preventDefault();
          void download(e.path);
        });

        li.append(open, " ", size, " ", dl);
        return li;
      }),
    );

    // A file that has been deleted underneath us should not keep showing its
    // old contents as though it were still there.
    if (selected && !entries.some((e) => e.path === selected)) {
      selected = null;
      viewer.textContent = "";
    }
  }

  let unsubscribe: (() => void) | null = null;
  if (opts.onFlush) {
    void opts.onFlush(() => void refresh()).then((off) => {
      unsubscribe = off;
    });
  }
  reload.addEventListener("click", () => void refresh());

  return {
    refresh,
    projection() {
      // Sizes included: "the file exists" and "the file has the right contents"
      // are different claims, and a listing that hid size could only make the
      // first.
      return entries.length === 0
        ? "(empty)"
        : entries.map((e) => `${e.path}  ${e.size}`).join("\n");
    },
    destroy() {
      unsubscribe?.();
      bar.remove();
      list.remove();
      viewer.remove();
    },
  };
}

/**
 * Decode for display, or say it is binary.
 *
 * `fatal: true` on purpose: the alternative silently substitutes replacement
 * characters, so a binary file would render as plausible-looking nonsense and a
 * reader could not tell whether the file or the viewer was wrong.
 */
function decode(data: Uint8Array, path: string): string {
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(data);
  } catch {
    return `(binary — ${bytes(data.length)}, not shown)\n${path}`;
  }
}

async function download(path: string): Promise<void> {
  const data = await readOPFSBytes(path);
  const url = URL.createObjectURL(new Blob([data as BlobPart]));
  const a = document.createElement("a");
  a.href = url;
  a.download = path.split("/").pop() ?? path;
  a.click();
  // Revoked on the next tick: revoking immediately can beat the click on some
  // browsers and download an empty file.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** Minimal styling, shipped with the widget. Overridable by an app's own CSS. */
export function filesStyles(): string {
  return `
.ilc-files { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
.ilc-files-bar { padding: .5rem 0; }
.ilc-files-list { list-style: none; margin: 0; padding: 0; }
.ilc-files-list li { padding: .15rem 0; }
.ilc-files-list li.selected { font-weight: 600; }
.ilc-files-name { border: 0; background: none; font: inherit; color: inherit; cursor: pointer; text-decoration: underline; padding: 0; }
.ilc-files-size { opacity: .6; }
.ilc-files-viewer { margin: .5rem 0 0; padding: .5rem; max-height: 24em; overflow: auto; white-space: pre-wrap; word-break: break-word; border-top: 1px solid currentColor; }
`.trim();
}
