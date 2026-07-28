// Drives notes' web slot with NO engine (Decision 34, host-layer plan Phase 2).
//
// Imported by URL from `page.evaluate`, exactly as `driver.ts` is: page.evaluate
// runs in the BROWSER, which Vite never transformed, so a bare specifier like
// `@devalbo/ilc-web/testing` cannot resolve there. A module fetched from the dev
// server can, because Vite transforms it on the way out.
//
// Nothing in the app imports this, so a production build never sees it.
import { createFakePort, ok, err } from "@devalbo/ilc-web/testing";
import type { FakePort } from "@devalbo/ilc-web/testing";

import {
  ListRecordsResponse,
  CreateRecordResponse,
  RecordChangedEvent,
} from "@gen/notes/v1/commands.pb";
import {
  MethodCreateRecord,
  MethodDeleteRecord,
  MethodListRecords,
} from "@gen/notes/v1/commands.registry.pb";

import { mountNotes, notesDom, TopicRecordChanged } from "../src/view";
import type { NotesView } from "../src/view";

type Rec = { id: string; title: string };

let fake: FakePort;
let view: NotesView;

/** What the engine will answer `list-records` with, until changed. */
function listReply(records: Rec[]) {
  return ok(ListRecordsResponse.toBinary({ records }));
}

/**
 * Mount the REAL markup with a FAKE engine.
 *
 * index.html is fetched rather than duplicated: the slot must be tested against
 * the form users actually get, and a copy here would drift without anything
 * noticing.
 */
export async function setup(records: Rec[] = []): Promise<void> {
  const html = await fetch("/index.html").then((r) => r.text());
  const parsed = new DOMParser().parseFromString(html, "text/html");
  const harness = document.getElementById("harness")!;
  harness.replaceChildren(...Array.from(parsed.body.children));

  fake = createFakePort({
    [MethodListRecords]: listReply(records),
    [MethodCreateRecord]: ok(
      CreateRecordResponse.toBinary({
        record: { id: "new-note", title: "New note" },
        path: "records/new-note.json",
      }),
    ),
    [MethodDeleteRecord]: ok(new Uint8Array()),
  });

  // A fixed clock: the host supplies it (the engine has none), and a fixed one
  // is what keeps a projection comparable between runs and between tiers.
  view = mountNotes(fake.port, notesDom(harness), () => 1_700_000_000);
  await view.refresh();
}

/** What the slot currently shows. */
export function projection(): string {
  return view.projection();
}

/** Commands the slot has run, as method ids. */
export function calls(): number[] {
  return fake.calls.map((c) => c.method);
}

/** Change what the engine would answer `list-records` with from now on. */
export function setRecords(records: Rec[]): void {
  fake.reply(MethodListRecords, listReply(records));
}

/** Make `list-records` fail, as the envelope reports it. */
export function breakList(message: string): void {
  fake.reply(MethodListRecords, err(message));
}

/**
 * Deliver an event, as the worker would after a flush.
 *
 * This is the whole point of the phase: no command ran, no engine exists, and
 * the view must still repaint. Returns once the view's re-list has settled.
 */
export async function emitRecordChanged(id: string): Promise<void> {
  fake.emit(
    TopicRecordChanged,
    RecordChangedEvent.toBinary({ id, method: MethodCreateRecord }),
  );
  await settle();
}

/** Deliver a topic the slot does not know. It must be ignored entirely. */
export async function emitForeignTopic(): Promise<void> {
  fake.emit("someone-else.thing-happened", new Uint8Array([1, 2, 3]));
  await settle();
}

/** Click the real create button, so the handler path runs, not a direct call. */
export async function clickCreate(title: string, body: string): Promise<void> {
  const dom = notesDom(document.getElementById("harness")!);
  dom.title.value = title;
  dom.body.value = body;
  dom.create.click();
  await settle();
}

/** What the create form sent, so a test can assert the host built the request. */
export function lastCreateRequest(): number[] {
  const call = [...fake.calls].reverse().find((c) => c.method === MethodCreateRecord);
  return call ? Array.from(call.request) : [];
}

export function subscriberCount(): number {
  return fake.subscriberCount();
}

export async function unmount(): Promise<void> {
  await view.unmount();
}

/**
 * Let the view's promise chain finish.
 *
 * The event handler calls refresh() without awaiting (it cannot — it is a sync
 * callback), so a test that asserted immediately would race it. Two turns of the
 * microtask queue covers subscribe → execute → render, and this is a fake port
 * with no I/O behind it, so there is nothing slower to wait for.
 */
function settle(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}
