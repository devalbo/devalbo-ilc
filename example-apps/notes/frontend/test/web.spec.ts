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
