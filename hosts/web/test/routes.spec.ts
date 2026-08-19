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


// The inspector reads the LIVE registry, not just the generated schema.
//
// Asserting "nothing is struck through" would be a false pass waiting to
// happen: a decoder that failed also marks nothing, and this decoder is
// hand-written varint parsing against whatever the real wasm engine emits
// (packed vs unpacked repeated fields being exactly the kind of thing that
// differs). So the assertion is on the DECODED SURFACE — proof the answer came
// back and was understood.
test("the inspector reads the live command surface from the engine", async ({ page }) => {
  await page.goto("/commands.html");
  const surface = await page.waitForFunction(
    () => (window as any).inspector?.surface() ?? null,
    undefined,
    { timeout: 15000 },
  );
  const ids = (await surface.jsonValue()) as number[];

  // Ascending, because the engine sorts: Go map iteration is unordered, and an
  // unsorted answer would make the parity diff flake.
  expect([...ids].sort((a, b) => a - b)).toEqual(ids);
  // 4 = GetCommandSurface itself, 100 = export-fs. dlc registers its filesystem
  // block from the manifest, so 100 being here is the discovery path having run.
  expect(ids).toContain(4);
  expect(ids).toContain(100);
});

// A capability going away MID-SESSION, and coming back.
//
// This is the volatile half of the manifest (§6.4a, plan D4). Nothing in the
// browser triggers it automatically — there is no "your OPFS went away" event —
// so the re-send path is driven here directly. That is the point: the path is
// kept real and exercised so it can be trusted the day a trigger exists.
//
// It is also where the absent branch is watched running in a real browser: the
// engine unregisters its filesystem verbs, announces the change, and the
// inspector re-reads and strikes the command through — none of which any Go
// test can show.
test("a capability that goes away updates the inspector, and comes back", async ({ page }) => {
  await page.goto("/commands.html");
  await page.waitForFunction(() => (window as any).inspector?.surface() ?? null, undefined, {
    timeout: 15000,
  });
  await expect(page.getByTestId("unavailable-export-fs")).toHaveCount(0);

  const drop = await page.evaluate(async () => (await (window as any).host.setEnvironment(false)).success);
  expect(drop).toBe(true);

  // No reload, and no explicit refresh call: the inspector heard
  // `ilc.world-manifest-changed` and asked the engine again.
  await expect(page.getByTestId("unavailable-export-fs")).toHaveCount(1);
  expect(await page.evaluate(() => (window as any).inspector.surface())).not.toContain(100);

  const restore = await page.evaluate(async () => (await (window as any).host.setEnvironment(true)).success);
  expect(restore).toBe(true);

  // Reversible: a first absence must not be permanent, or a host that regains
  // a storage grant is stuck with a surface that no longer matches it.
  await expect(page.getByTestId("unavailable-export-fs")).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).inspector.surface())).toContain(100);
});
