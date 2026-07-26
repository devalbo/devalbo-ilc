// T-B3.1 — `dlc new` in the browser, persisted in OPFS.
//
// This is the web half of the bootstrap milestone: the SAME engine the CLI links
// natively, running as a wasip2 component under jco, writing a real scaffold
// tree into OPFS through a WASI preopen — and surviving a page reload.
//
// The assertions deliberately mirror the native `dlc new` test in
// engine/execute_test.go: same file set, same refusal to overwrite. If these two
// ever disagree, the cross-tier promise is broken.
import { expect, test } from "@playwright/test";

const EXPECTED = ["myapp/README.md", "myapp/engine/execute.go", "myapp/go.mod"];

// Each test starts from an empty OPFS: it is origin-scoped and outlives a
// reload (that being the point), so leftovers from a previous run would make
// `new` refuse to scaffold and the results order-dependent.
test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [name] of (root as any).entries()) {
      await root.removeEntry(name, { recursive: true });
    }
  });
  await page.reload();
  await expect(page.getByTestId("files")).toContainText("empty");
});

test("scaffolds into OPFS and survives a reload", async ({ page }) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();

  await expect(page.getByTestId("log")).toContainText("scaffolded myapp", {
    timeout: 30_000,
  });
  const files = page.getByTestId("files");
  for (const f of EXPECTED) {
    await expect(files).toContainText(f);
  }

  // The claim that matters: OPFS is durable, so a reload — which throws away
  // the whole wasm instance and its in-memory FileData tree — still shows the
  // tree, rehydrated from disk.
  await page.reload();
  for (const f of EXPECTED) {
    await expect(page.getByTestId("files")).toContainText(f);
  }

  // …and the bytes are really in OPFS, not just in the UI's state: read one
  // file back directly through the browser API, bypassing the engine entirely.
  const goMod = await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const dir = await root.getDirectoryHandle("myapp");
    const file = await (await dir.getFileHandle("go.mod")).getFile();
    return file.text();
  });
  expect(goMod).toBe("module github.com/you/myapp\n\ngo 1.23.0\n");
});

test("honors --module, like the CLI does", async ({ page }) => {
  await page.getByTestId("name").fill("acmeapp");
  await page.getByTestId("module").fill("github.com/acme/acmeapp");
  await page.getByTestId("new").click();
  await expect(page.getByTestId("log")).toContainText("scaffolded acmeapp", {
    timeout: 30_000,
  });

  const goMod = await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const dir = await root.getDirectoryHandle("acmeapp");
    const file = await (await dir.getFileHandle("go.mod")).getFile();
    return file.text();
  });
  expect(goMod).toBe("module github.com/acme/acmeapp\n\ngo 1.23.0\n");
});

test("refuses to scaffold over an existing tree", async ({ page }) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();
  await expect(page.getByTestId("log")).toContainText("scaffolded myapp", {
    timeout: 30_000,
  });

  // Same engine rule as the native run — the error text comes from engine/, not
  // from any JavaScript, which is what makes it identical across tiers.
  await page.getByTestId("new").click();
  await expect(page.getByTestId("log")).toContainText(
    "already exists and is not empty",
  );
});

// T-B3.2 — BFT export/import in the browser (§7.3).
//
// "Download my project" and `export-fs` are the same operation, and the bundle
// is plain text, so it crosses any channel: this browser, a terminal, an
// embedded device. The strongest claim here is the last one — the bytes the
// browser produces are the bytes the CLI produces.
test("exports a BFT bundle and re-imports it", async ({ page }) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();
  await expect(page.getByTestId("log")).toContainText("scaffolded myapp", {
    timeout: 30_000,
  });

  const download = await Promise.all([
    page.waitForEvent("download"),
    page.getByTestId("export").click(),
  ]).then(([d]) => d);
  expect(download.suggestedFilename()).toBe("dlc-workspace.bft.json");

  const stream = await download.createReadStream();
  const bundle = await new Promise<string>((resolve, reject) => {
    let out = "";
    stream.on("data", (c) => (out += c));
    stream.on("end", () => resolve(out));
    stream.on("error", reject);
  });

  // Readable BFT, not an opaque blob — that is the format's whole argument.
  expect(bundle).toContain('"type": "directory"');
  expect(bundle).toContain("module github.com/you/myapp");
  // The whole root, so the app directory is preserved and import is an exact
  // inverse — see the note on runExport().
  const parsed = JSON.parse(bundle);
  expect(Object.keys(parsed.entries)).toEqual(["myapp"]);
  expect(Object.keys(parsed.entries.myapp.entries).sort()).toEqual([
    "README.md",
    "engine",
    "go.mod",
  ]);

  // Wipe, then import the downloaded bundle back — the tree returns.
  // `clear OPFS` reloads the page by design: a running engine cannot be rebound
  // to a fresh filesystem root, so the restart is what makes the clear real.
  await page.getByRole("button", { name: "clear OPFS" }).click();
  await expect(page.getByTestId("files")).toContainText("empty");
  await expect(page.getByTestId("log")).toHaveText("");

  await page.getByTestId("import").setInputFiles({
    name: "myapp.bft.json",
    mimeType: "application/json",
    buffer: Buffer.from(bundle),
  });
  await expect(page.getByTestId("log")).toContainText("imported 3 files");
  for (const f of EXPECTED) {
    await expect(page.getByTestId("files")).toContainText(f);
  }
});

// A bundle is untrusted input wherever it arrives from. The refusal comes from
// engine/, so the browser inherits the CLI's safety rather than reimplementing it.
test("refuses a bundle whose paths escape the root", async ({ page }) => {
  const evil = JSON.stringify({
    type: "directory",
    entries: { "../escaped.txt": { type: "text", content: "pwned" } },
  });
  await page.getByTestId("import").setInputFiles({
    name: "evil.bft.json",
    mimeType: "application/json",
    buffer: Buffer.from(evil),
  });
  await expect(page.getByTestId("log")).toContainText("import-fs failed");
  await expect(page.getByTestId("files")).toContainText("empty");
});

// Import defaults to REPLACE in this UI, which only means anything if the
// engine can genuinely delete. The stock browser preview2-shim stubs
// `unlinkFileAt`/`removeDirectoryAt` as no-ops that silently succeed — the
// vendored copy in hosts/web/shim/ patches them. Without that patch this test
// passes the "3 files" assertion and still shows the stale tree.
test("import --replace really deletes what the bundle omits", async ({
  page,
}) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();
  await expect(page.getByTestId("log")).toContainText("scaffolded myapp", {
    timeout: 30_000,
  });
  await expect(page.getByTestId("files")).toContainText("myapp/go.mod");

  // A bundle with ONE file where the tree currently has three.
  const bundle = JSON.stringify({
    type: "directory",
    entries: {
      myapp: {
        type: "directory",
        entries: { "only.txt": { type: "text", content: "replaced\n" } },
      },
    },
  });
  await page.getByTestId("import").setInputFiles({
    name: "replace.bft.json",
    mimeType: "application/json",
    buffer: Buffer.from(bundle),
  });
  await expect(page.getByTestId("log")).toContainText("imported 1 files");

  await expect(page.getByTestId("files")).toContainText("myapp/only.txt");
  await expect(page.getByTestId("files")).not.toContainText("myapp/go.mod");
  await expect(page.getByTestId("files")).not.toContainText("README.md");
});
