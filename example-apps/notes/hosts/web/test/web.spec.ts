// notes in a browser — the same engine the CLI links, as a wasip2 component.
//
// This ships WITH the app on purpose: the cross-tier promise is only worth
// something if a check enforces it. If this passes and `notes create` / `notes
// list` pass in a terminal, the two tiers genuinely agree — and because every
// record is a plain JSON file in OPFS, the store has the same shape either way.
import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  // OPFS is origin-scoped and survives reloads, so leftovers would make results
  // order-dependent.
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [name] of (root as any).entries()) {
      await root.removeEntry(name, { recursive: true });
    }
  });
  await page.reload();
});

test("creates, lists, and deletes notes through the shared engine", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));

  await expect(page.getByTestId("count")).toHaveText("0");

  await page.getByTestId("title").fill("Buy milk");
  await page.getByTestId("body").fill("two litres");
  await page.getByTestId("create").click();

  // The id is derived in engine/commands.go — not in any TypeScript — so a
  // matching slug here is evidence the browser ran the same logic as the CLI.
  await expect(page.getByTestId("out")).toContainText("created buy-milk", {
    timeout: 30_000,
  });
  await expect(page.getByTestId("count")).toHaveText("1");
  await expect(page.getByTestId("list")).toContainText("Buy milk");

  // …and it is a real file in OPFS, readable without the engine. This is what
  // split storage buys: the store is inspectable, not a database blob.
  const stored = await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const dir = await root.getDirectoryHandle("records");
    const file = await (await dir.getFileHandle("buy-milk.json")).getFile();
    return file.text();
  });
  expect(JSON.parse(stored)).toMatchObject({ id: "buy-milk", title: "Buy milk" });

  // Listing after a reload exercises the directory scan on a cold engine —
  // the fallback path that exists because the index capability does not.
  await page.reload();
  await expect(page.getByTestId("count")).toHaveText("1");

  await page.getByTestId("delete-buy-milk").click();
  await expect(page.getByTestId("count")).toHaveText("0");

  expect(errors, "no uncaught errors while driving the engine").toEqual([]);
});

// Events (§6.3) — the UI stops asking what changed.
//
// `main.ts` has no refresh() after create or delete: the engine emits
// `notes.record-changed`, the host forwards it, and the subscription re-lists.
// So the test above already depends on the event path. This one isolates the
// claim that path exists FOR: a write this UI did not make still repaints it.
test("repaints for a write no UI handler made", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await expect(page.getByTestId("count")).toHaveText("0");

  // Boot the engine through the UI first, so this measures the event and not a
  // cold start (the worker instantiates on the first command either way).
  await page.getByTestId("title").fill("First");
  await page.getByTestId("create").click();
  await expect(page.getByTestId("count")).toHaveText("1", { timeout: 30_000 });

  // No click, no handler, no `main.ts` code on the stack — a different module
  // calls the engine directly. Nothing here touches the DOM.
  await page.evaluate(async () => {
    // A URL, not a bare specifier: this code runs in the BROWSER, which Vite
    // never transformed — the dev server resolves and transforms driver.ts when
    // the browser fetches it. Held in a variable so TypeScript type-checks the
    // shape below without trying to resolve a path that only exists at run time.
    const url = "/test/driver.ts";
    const { createDirect } = (await import(
      /* @vite-ignore */ url
    )) as typeof import("./driver");
    await createDirect("From nowhere");
  });

  // The list can only have learned about it from the event.
  await expect(page.getByTestId("count")).toHaveText("2");
  await expect(page.getByTestId("list")).toContainText("From nowhere");
  // …carrying which record changed, and what changed it: 10000 = CreateRecord.
  await expect(page.getByTestId("out")).toContainText(
    "event notes.record-changed — from-nowhere (method 10000)",
  );

  expect(errors, "no uncaught errors while driving the engine").toEqual([]);
});

// `window.app` — the dev-console handle (Decision 34).
//
// Worth a test rather than trusting it exists, because its value depends
// entirely on being the SAME path the buttons take. If the console handle ever
// became a parallel implementation, this page would have two front ends free to
// disagree — and the one nobody tests would be the one that drifts.
test("window.app drives the slot from the console", async ({ page }) => {
  await expect(page.getByTestId("count")).toHaveText("0");

  // No click, no form: exactly what someone types into devtools.
  const created = await page.evaluate(() =>
    (window as any).app.create("From the console", "typed by hand"),
  );
  expect(created).toBe(true);

  // The list updated by EVENT, as it does for a click — the console handle gets
  // the reactivity loop for free precisely because it is not a separate path.
  await expect(page.getByTestId("count")).toHaveText("1");

  // …and the slot can say what it is showing, which is what makes two slots on
  // different tiers comparable at all.
  const projection = await page.evaluate(() => (window as any).app.projection());
  expect(projection).toBe("records: 1\n- from-the-console — From the console");

  const removed = await page.evaluate(() =>
    (window as any).app.remove("from-the-console"),
  );
  expect(removed).toBe(true);
  await expect(page.getByTestId("count")).toHaveText("0");
});
