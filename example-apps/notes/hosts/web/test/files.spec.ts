import { expect, test } from "@playwright/test";

// The OPFS browser.
//
// What this really tests is §7.1: *the files are the truth*. A note created
// through the UI must appear as a readable JSON file with the content the engine
// wrote — not as an app-private blob. On the terminal tier you check that with
// `ls` and `cat`; this is the browser's version of the same check, and until
// this page existed the claim was unverifiable by eye on this tier.

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [n] of (root as any).entries()) {
      await root.removeEntry(n, { recursive: true });
    }
  });
  await page.reload();
});

test("a note written by the app is a readable file", async ({ page }) => {
  await page.getByTestId("title").fill("Buy milk");
  await page.getByTestId("body").fill("two litres");
  await page.getByTestId("create").click();
  await expect(page.getByTestId("count")).toHaveText("1", { timeout: 30_000 });

  await page.goto("/files.html");
  await expect(page.getByTestId("file-records/buy-milk.json")).toBeVisible();

  // …and its CONTENT is the record, in a form a human can read without the app.
  await page.getByTestId("file-records/buy-milk.json").click();
  // The read is async; wait for content rather than racing it.
  await expect(page.getByTestId("files-viewer")).toContainText("buy-milk");
  const shown = await page.getByTestId("files-viewer").textContent();
  expect(JSON.parse(shown!)).toMatchObject({
    id: "buy-milk",
    title: "Buy milk",
    body: "two litres",
  });
});

test("an empty store says so", async ({ page }) => {
  await page.goto("/files.html");
  await expect(page.getByTestId("files-summary")).toContainText("empty");
});

// The reactivity loop pointed at a filesystem view: no refresh button pressed,
// no navigation — the engine announced a write and the listing re-read.
//
// The subscription deliberately does NOT filter on `ilc.data-changed`: that
// topic comes from the inherited fs verbs, while notes' own writes announce
// `notes.record-changed`. Filtering on the platform topic would miss every note.
test("the listing updates when a command writes", async ({ page }) => {
  await page.goto("/files.html");
  await expect(page.getByTestId("files-summary")).toContainText("empty");

  await page.evaluate(async () => {
    const url = "/test/driver.ts";
    const { createDirect } = (await import(/* @vite-ignore */ url)) as typeof import("./driver");
    await createDirect("From nowhere");
  });

  await expect(page.getByTestId("file-records/from-nowhere.json")).toBeVisible({
    timeout: 30_000,
  });
});

test("sizes are shown, because existing and being right are different claims", async ({
  page,
}) => {
  await page.getByTestId("title").fill("Buy milk");
  await page.getByTestId("create").click();
  await expect(page.getByTestId("count")).toHaveText("1", { timeout: 30_000 });

  await page.goto("/files.html");
  await expect(page.getByTestId("files-summary")).toContainText("1 file(s)");
  await expect(page.getByTestId("files-list")).toContainText("B");
});
