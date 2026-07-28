import { expect, test } from "@playwright/test";

// dlc's own text routes — the same three every scaffolded app ships.
//
// These exist because `dlc` is an app like any other (AGENTS.md §3). Until now
// it was the ONE app lacking every capability built for apps: no terminal, no
// file browser, no command inspector, no console handle. The tool that teaches
// the pattern was the one not following it, which is exactly the dogfood drift
// the review item in the tasks doc exists to catch — and it had fired three
// times without anyone acting.

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

test("the terminal runs dlc's own commands", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("version");
  await page.getByTestId("terminal-input").press("Enter");
  // The version string, not the banner — the banner says "dlc" too, and an
  // assertion matching it would pass while the command failed. That false pass
  // happened three times today in other suites.
  await expect(page.getByTestId("terminal-output")).toContainText("0.0.0-bootstrap", {
    timeout: 30_000,
  });
});

test("the terminal scaffolds, and the file browser shows it", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("new --tiers native fromterminal");
  await page.getByTestId("terminal-input").press("Enter");
  await expect(page.getByTestId("terminal-output")).toContainText("scaffold fromterminal", {
    timeout: 30_000,
  });

  // §7.1 by eye: what the command wrote is a readable file in OPFS.
  await page.goto("/files.html");
  await expect(page.getByTestId("file-fromterminal/go.mod")).toBeVisible();
  await page.getByTestId("file-fromterminal/go.mod").click();
  await expect(page.getByTestId("files-viewer")).toContainText("module ");
});

test("the inspector documents dlc's surface from the schema", async ({ page }) => {
  await page.goto("/commands.html");
  await expect(page.getByTestId("command-new")).toBeVisible();
  await page.getByTestId("command-new").click();
  await expect(page.getByTestId("detail-meta")).toContainText("NewRequest");
  // `tiers` is required on the CLI surface — declared in the .proto, rendered
  // here without anyone writing it down twice.
  await expect(page.getByTestId("command-detail")).toContainText("required");
  // The inherited verbs sit alongside dlc's own.
  for (const name of ["version", "export-fs", "import-fs", "reset-fs"]) {
    await expect(page.getByTestId(`command-${name}`)).toBeVisible();
  }
});

test("window.app drives the React slot from the console", async ({ page }) => {
  const ok = await page.evaluate(() =>
    (window as any).app.new("fromconsole", "example.com/fromconsole"),
  );
  expect(ok).toBe(true);
  // The same path the button takes, so the file list updates by the same route.
  await expect(page.getByTestId("files")).toContainText("fromconsole", { timeout: 30_000 });
});
