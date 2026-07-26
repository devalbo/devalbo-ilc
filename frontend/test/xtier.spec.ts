// Cross-tier bundle interchange: write the browser's exported bundle to disk so
// the CLI can import it. The assertion lives in scripts/verify-bundle-xtier.sh.
import { writeFileSync } from "node:fs";
import { test } from "@playwright/test";

test("emit a browser-exported bundle", async ({ page }) => {
  await page.goto("/");
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [name] of (root as any).entries()) {
      await root.removeEntry(name, { recursive: true });
    }
  });
  await page.reload();
  await page.getByTestId("name").fill("xtier");
  await page.getByTestId("module").fill("github.com/acme/xtier");
  await page.getByTestId("new").click();
  await page.getByTestId("log").filter({ hasText: "scaffolded" }).waitFor();

  const download = await Promise.all([
    page.waitForEvent("download"),
    page.getByTestId("export").click(),
  ]).then(([d]) => d);
  await download.saveAs(process.env.XTIER_OUT!);
});
