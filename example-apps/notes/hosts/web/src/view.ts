// notes' web tier slot — the view (Decision 34).
//
// The form IS this tier's command parser (Decision 28): it collects fields,
// encodes a typed request, and hands the bytes to the engine. What create/list/
// delete MEAN lives in engine/ — shared with the CLI — which is why a note
// written here reads identically from `notes list` in a terminal.
//
// A SLOT RENDERS; IT NEVER DECIDES. Nothing here works anything out that the
// engine could have: the list is what `list-records` returned, the count is that
// list's length. If a rule about what a note IS appears in this file, it is in
// the wrong place — it belongs in engine/, where every tier shares it.
//
// The engine arrives as an ARGUMENT (`EnginePort`), not as an import. That is
// the whole reason this file is separate from main.ts: mount it against the real
// port and it drives a wasm component in a worker; mount it against the fake in
// `@devalbo/ilc-web/testing` and the same code renders with no engine, no
// worker, and no OPFS.
import type { EnginePort } from "@devalbo/ilc-web/port";

import {
  CreateRecordRequest,
  CreateRecordResponse,
  DeleteRecordRequest,
  ListRecordsRequest,
  ListRecordsResponse,
  RecordChangedEvent,
} from "@gen/notes/v1/commands.pb";
import {
  MethodCreateRecord,
  MethodDeleteRecord,
  MethodListRecords,
} from "@gen/notes/v1/commands.registry.pb";

/** notes' own topic. The app names it; nothing registers it (Decision 33 D3). */
export const TopicRecordChanged = "notes.record-changed";

export type NotesView = {
  /**
   * Write a note. Returns whether the engine accepted it.
   *
   * Takes its arguments rather than reading the form, so the SAME operation
   * serves the button, a test, and the dev console. The form's job is to collect
   * two strings and call this; if it did more, the console and the button would
   * be two subtly different ways to create a note.
   */
  create(title: string, body: string): Promise<boolean>;
  /** Delete a note by id. Returns whether anything was deleted. */
  remove(id: string): Promise<boolean>;
  /** Stop listening. The DOM is left as it is. */
  unmount(): Promise<void>;
  /**
   * A normalized text projection of what is on screen right now.
   *
   * Deliberately NOT a DOM scrape by the test: a scrape couples the assertion to
   * markup, and a slot on another tier (an ASCII board in a terminal, an
   * embedded TFT) has no DOM to scrape at all. A slot SAYS what it is showing,
   * so two slots on different tiers can be compared against one another — which
   * is the only mechanical check this layer can have.
   */
  projection(): string;
  /** Re-read from the engine. The event subscription calls this; so does boot. */
  refresh(): Promise<void>;
};

export type NotesDom = {
  out: HTMLElement;
  list: HTMLElement;
  count: HTMLElement;
  title: HTMLInputElement;
  body: HTMLInputElement;
  create: HTMLElement;
};

/** Find the slot's elements in a document (or any fragment, for tests). */
export function notesDom(scope: ParentNode = document): NotesDom {
  const need = <T extends Element>(sel: string): T => {
    const el = scope.querySelector<T>(sel);
    if (!el) throw new Error(`notes view: no element matching ${sel}`);
    return el;
  };
  return {
    out: need("#out"),
    list: need("#list"),
    count: need('[data-testid="count"]'),
    title: need("#title"),
    body: need("#body"),
    create: need("#create"),
  };
}

/**
 * Wire the view to an engine and start it.
 *
 * `now` is injected for the same reason the engine has no clock capability: a
 * browser tab and an MCU disagree about what one is, so the HOST supplies it —
 * exactly as the CLI does with `time.Now()`. A test supplies a fixed one, which
 * is also what keeps a projection comparable across runs.
 */
export function mountNotes(
  port: EnginePort,
  dom: NotesDom = notesDom(),
  now: () => number = () => Math.floor(Date.now() / 1000),
): NotesView {
  const log: string[] = [];
  let records: { id: string; title: string }[] = [];

  function say(line: string) {
    log.push(line);
    dom.out.textContent += line + "\n";
  }

  async function refresh() {
    const r = await port.execute(
      MethodListRecords,
      ListRecordsRequest.toBinary({}),
    );
    if (!r.success) {
      say(`list failed: ${r.error ?? "(no message)"}`);
      return;
    }
    const decoded = ListRecordsResponse.fromBinary(r.output);
    records = (decoded.records ?? []).map((rec) => ({
      id: rec.id ?? "",
      title: rec.title ?? "",
    }));
    dom.count.textContent = String(records.length);
    dom.list.replaceChildren(
      ...records.map((rec) => {
        const li = document.createElement("li");
        li.dataset.id = rec.id;
        li.textContent = `${rec.id} — ${rec.title}`;
        const del = document.createElement("button");
        del.textContent = "delete";
        del.dataset.testid = `delete-${rec.id}`;
        del.addEventListener("click", () => {
          remove(rec.id).catch((e) => say(`ERROR: ${(e as Error).message}`));
        });
        li.append(" ", del);
        return li;
      }),
    );
  }

  async function create(title: string, body: string): Promise<boolean> {
    // The HOST supplies the clock, exactly as the CLI does with time.Now(): the
    // engine has no clock capability, because a browser tab and an MCU disagree
    // about what one even is.
    const request = CreateRecordRequest.toBinary({
      title,
      body,
      createdAt: BigInt(now()),
    });
    const r = await port.execute(MethodCreateRecord, request);
    if (!r.success) {
      say(`create failed: ${r.error ?? "(no message)"}`);
      return false;
    }
    const resp = CreateRecordResponse.fromBinary(r.output);
    say(`created ${resp.record?.id} -> ${resp.path}`);
    return true;
    // NO refresh() here. The engine emits `notes.record-changed` and the
    // subscription below re-lists — see the note at the bottom of this file.
  }

  async function remove(id: string): Promise<boolean> {
    const r = await port.execute(
      MethodDeleteRecord,
      DeleteRecordRequest.toBinary({ id }),
    );
    if (!r.success) {
      say(`delete failed: ${r.error ?? "(no message)"}`);
      return false;
    }
    say(`deleted ${id}`);
    return true;
    // NO refresh() here either.
  }

  // The form's entire job: collect two strings, call the operation, clear up.
  // Everything a user can do from a button, they can do from `window.app` and
  // from a test, because all three go through the same two functions.
  const onCreate = () => {
    const title = dom.title.value;
    const body = dom.body.value;
    create(title, body)
      .then((created) => {
        if (!created) return;
        dom.title.value = "";
        dom.body.value = "";
      })
      .catch((e) => say(`ERROR: ${(e as Error).message}`));
  };
  dom.create.addEventListener("click", onCreate);

  // The reactivity loop (§6.3), and the reason create/remove no longer refresh.
  //
  // Calling refresh() after your own command is correct only while this tab,
  // driven by these two handlers, is the sole writer. It is already wrong for a
  // second tab on the same OPFS origin, for a CLI writing records/ while this
  // page is open, for an inherited `import-fs` replacing the store underneath
  // the list, and for any future sync. Asking the engine "what changed?" is the
  // wrong question; it tells us.
  //
  // The handlers above therefore only WRITE, and this is the only thing that
  // reads after a write. That is not a stylistic choice — it means the UI is
  // visibly wrong if events stop working, instead of quietly carrying a dead
  // capability.
  //
  // Safe to call back into the engine from here: this runs on the main thread
  // and reaches us by message from the worker, so there is no engine on the
  // stack.
  const unsubscribing = port.subscribe((topic, payload) => {
    if (topic !== TopicRecordChanged) return;
    const { id, method } = RecordChangedEvent.fromBinary(payload);
    say(`event ${topic} — ${id} (method ${method})`);
    refresh().catch((e) => say(`ERROR: ${(e as Error).message}`));
  });
  unsubscribing.catch((e) => say(`ERROR subscribing: ${(e as Error).message}`));

  return {
    create,
    remove,
    async unmount() {
      dom.create.removeEventListener("click", onCreate);
      (await unsubscribing)();
    },
    projection() {
      // Stable and tier-neutral: a count, then one line per record. No markup,
      // no ordering the engine did not give us, nothing derived.
      return [
        `records: ${records.length}`,
        ...records.map((r) => `- ${r.id} — ${r.title}`),
      ].join("\n");
    },
    refresh,
  };
}
