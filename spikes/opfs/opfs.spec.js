// Spike 3 — OPFS persistence across reload (T-B1.3).
//
// Default: headless Chromium (CI / make spike-opfs).
// Watch:   make spike-opfs-watch  (or SPIKE_OPFS_WATCH=1) — headed, slowMo,
//          pauses at the end so you can inspect Application → OPFS in DevTools.
import { test, expect } from "@playwright/test";

const watch = process.env.SPIKE_OPFS_WATCH === "1";

test.describe("T-B1.3 OPFS persistence", () => {
  test("engine write survives reload; file visible in OPFS", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("body")).toHaveAttribute("data-status", "ready", {
      timeout: 30_000,
    });

    // Clear any leftover OPFS from prior runs in this origin.
    await page.evaluate(async () => {
      const root = await navigator.storage.getDirectory();
      for await (const [name] of root.entries()) {
        await root.removeEntry(name, { recursive: true });
      }
    });

    const written = await page.evaluate(async () => window.__spike.write());
    expect(written.content).toBe("persist-me");
    expect(written.opfsDirect).toBe("persist-me");
    await expect(page.locator("body")).toHaveAttribute("data-status", "wrote");

    // Full navigation reload — new JS heap, new engine instance.
    await page.reload();
    await expect(page.locator("body")).toHaveAttribute("data-status", "ready", {
      timeout: 30_000,
    });

    const read = await page.evaluate(async () => window.__spike.read());
    expect(read.text).toBe("persist-me");
    expect(read.opfsDirect).toBe("persist-me");
    await expect(page.locator("body")).toHaveAttribute("data-status", "pass");

    // Bypass the engine: confirm the file exists in OPFS directly.
    const opfsOnly = await page.evaluate(async () =>
      window.__spike.readOPFSText(window.__spike.PATH),
    );
    expect(opfsOnly).toBe("persist-me");

    if (watch) {
      // Keep the browser open so you can inspect DevTools → Application → OPFS.
      // Resume/continue in the Playwright inspector to finish the test.
      // eslint-disable-next-line no-console
      console.log(
        "\nSPIKE_OPFS_WATCH: paused — open DevTools → Application → Storage → OPFS, then resume.\n",
      );
      await page.pause();
    }
  });
});
