import { test, expect, type Page } from "@playwright/test";

// The slot test: notes' web tier rendering with NO ENGINE (Decision 34).
//
// Everything else in this directory drives the real thing — a wasm component in
// a worker over OPFS. This file drives none of it. There is no engine, no
// worker, no filesystem: a fake `EnginePort` answers with scripted bytes and
// delivers events on demand.
//
// Why that is worth having, given web.spec.ts already passes:
//
//   * A slot is the ONE part of an ILC app parity cannot check. Parity compares
//     command results, the written filesystem, and the event stream — all
//     engine-side. Rendering is invisible to it by construction.
//   * Failure paths become reachable. "What does the list do when list-records
//     returns an error" takes real effort to stage against a live engine and one
//     line here.
//   * It is how a renderer for a tier that cannot run yet gets developed — an
//     ESP32 slot has no host to run against until the firmware exists.
//
// The assertions are on the slot's PROJECTION, not on markup. A projection is
// what a slot says it is showing, which is the only thing two slots on different
// tiers can be compared on (host parity, Phase 4). Asserting DOM here would tie
// this to notes' particular <li>.

const DRIVER = "/test/slot-driver.ts";

// page.evaluate runs in the browser, where bare specifiers do not resolve — so
// every call goes through the driver module, which Vite transforms on fetch.
async function drive<T>(page: Page, call: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ call, args, driver }) => {
      const m = (await import(/* @vite-ignore */ driver)) as Record<
        string,
        (...a: unknown[]) => unknown
      >;
      return (await m[call](...args)) as T;
    },
    { call, args, driver: DRIVER },
  );
}

test.beforeEach(async ({ page }) => {
  await page.goto("/test/slot.html");
});

test("renders the real markup from scripted bytes, with no engine", async ({
  page,
}) => {
  await drive(page, "setup", [
    { id: "buy-milk", title: "Buy milk" },
    { id: "call-mum", title: "Call mum" },
  ]);

  expect(await drive<string>(page, "projection")).toBe(
    ["records: 2", "- buy-milk — Buy milk", "- call-mum — Call mum"].join("\n"),
  );
});

// The reactivity claim, isolated. web.spec.ts proves it end to end through a
// real engine; this proves the SLOT's half of it — that an event with nothing
// behind it still repaints the view.
test("an event repaints, with no command and no engine", async ({ page }) => {
  await drive(page, "setup", [{ id: "buy-milk", title: "Buy milk" }]);
  expect(await drive<string>(page, "projection")).toBe(
    "records: 1\n- buy-milk — Buy milk",
  );

  // Someone else wrote a note. No handler in this page ran.
  await drive(page, "setRecords", [
    { id: "buy-milk", title: "Buy milk" },
    { id: "from-nowhere", title: "From nowhere" },
  ]);
  await drive(page, "emitRecordChanged", "from-nowhere");

  expect(await drive<string>(page, "projection")).toBe(
    ["records: 2", "- buy-milk — Buy milk", "- from-nowhere — From nowhere"].join(
      "\n",
    ),
  );
});

// An event per `list` would loop any subscriber that re-lists on an event, and a
// topic the slot does not know must cost nothing at all.
test("a foreign topic is ignored — no re-list", async ({ page }) => {
  await drive(page, "setup", [{ id: "buy-milk", title: "Buy milk" }]);
  const before = (await drive<number[]>(page, "calls")).length;

  await drive(page, "emitForeignTopic");

  expect(await drive<number[]>(page, "calls")).toHaveLength(before);
});

// Reachable here, awkward against a live engine: make the engine fail.
test("a failed list leaves the previous view standing", async ({ page }) => {
  await drive(page, "setup", [{ id: "buy-milk", title: "Buy milk" }]);

  await drive(page, "breakList", "index unavailable");
  await drive(page, "emitRecordChanged", "buy-milk");

  // Not blanked, not "records: 0" — a failed read is not an empty result, and a
  // slot that conflated them would show a user their notes had vanished.
  expect(await drive<string>(page, "projection")).toBe(
    "records: 1\n- buy-milk — Buy milk",
  );
});

// The create path still runs through the real button and the real form, so this
// covers the host-side request building (Decision 28) without an engine to
// decode it.
test("create sends a request and does not refresh by hand", async ({ page }) => {
  await drive(page, "setup", []);
  const before = (await drive<number[]>(page, "calls")).length;

  await drive(page, "clickCreate", "Buy milk", "semi-skimmed");

  expect(await drive<number[]>(page, "lastCreateRequest")).not.toHaveLength(0);
  // create + nothing else: no manual re-list. The event is what re-reads, which
  // is why deleting the emit in the engine breaks the UI rather than passing
  // quietly.
  expect(await drive<number[]>(page, "calls")).toHaveLength(before + 1);
});

test("unmount stops listening", async ({ page }) => {
  await drive(page, "setup", []);
  expect(await drive<number>(page, "subscriberCount")).toBe(1);

  await drive(page, "unmount");

  expect(await drive<number>(page, "subscriberCount")).toBe(0);
});
