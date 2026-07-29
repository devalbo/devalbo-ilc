// T-B3.1 — `dlc new` in the browser, persisted in OPFS.
//
// This is the web half of the bootstrap milestone: the SAME engine the CLI links
// natively, running as a wasip2 component under jco, writing a real scaffold
// tree into OPFS through a WASI preopen — and surviving a page reload.
//
// The assertions deliberately mirror the native `dlc new` test in
// engine/execute_test.go: same file set, same refusal to overwrite. If these two
// ever disagree, the cross-tier promise is broken.
import { expect, test, type Page } from "@playwright/test";

// Everything the page said that a human would want on a failure. A timed-out
// `toContainText` tells you what the log DIDN'T say; these tell you why.
// Collected per page and appended to the assertion message, so a CI failure is
// self-explaining instead of needing an artifact download — which is exactly
// what a wasm 404 and an engine exception both looked like the last time.
const diagnostics = new WeakMap<Page, string[]>();

function watch(page: Page) {
  const lines: string[] = [];
  diagnostics.set(page, lines);
  page.on("pageerror", (e) => lines.push(`pageerror: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() === "error" || m.type() === "warning") {
      lines.push(`console.${m.type()}: ${m.text()}`);
    }
  });
  // A failed fetch is how a mislocated engine.component.core.wasm shows up; the
  // in-page error only says "HTTP status code is not ok".
  page.on("requestfailed", (r) =>
    lines.push(`requestfailed: ${r.url()} — ${r.failure()?.errorText ?? "?"}`),
  );
  page.on("response", (r) => {
    if (r.status() >= 400) lines.push(`http ${r.status()}: ${r.url()}`);
  });
}

function report(page: Page): string {
  const lines = diagnostics.get(page) ?? [];
  return lines.length ? `\n\npage diagnostics:\n  ${lines.join("\n  ")}` : "";
}

// A representative slice of the scaffold, not the whole list — the full set is
// asserted natively in engine/execute_test.go, and duplicating it here would
// mean two places to update for one template change. What matters on this tier
// is that the browser writes the SAME tree, including nested directories.
const EXPECTED = [
  "myapp/go.mod",
  "myapp/engine/commands.go",
  "myapp/hosts/native/main.go",
  "myapp/proto/myapp/v1/commands.proto",
];

/** Wait for `dlc new` (or surface whatever the log actually says on failure). */
async function expectScaffolded(page: import("@playwright/test").Page, name: string) {
  const log = page.getByTestId("log");
  await expect(log).toContainText(new RegExp(`scaffolded ${name}|new failed|ERROR:`), {
    timeout: 60_000,
  });
  const text = await log.innerText();
  expect(text, `log was:\n${text}${report(page)}`).toContain(`scaffolded ${name}`);
}

// Each test starts from an empty OPFS: it is origin-scoped and outlives a
// reload (that being the point), so leftovers from a previous run would make
// `new` refuse to scaffold and the results order-dependent.
test.beforeEach(async ({ page }) => {
  watch(page);
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

  await expectScaffolded(page, "myapp");
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
  expect(goMod).toContain("module github.com/you/myapp");
  // No --platform-path in the browser, so the replace directive comes through
  // commented, with instructions rather than a broken build.
  // A scaffolded app depends on the PLATFORM, never on dlc (§16.4) — dlc is the
  // tool that generated it, not something it links.
  expect(goMod).toContain("// replace github.com/devalbo/dlc-platform =>");
  expect(goMod).not.toContain("require github.com/devalbo/devalbo-ilc");
});

test("honors --module, like the CLI does", async ({ page }) => {
  await page.getByTestId("name").fill("acmeapp");
  await page.getByTestId("module").fill("github.com/acme/acmeapp");
  await page.getByTestId("new").click();
  await expectScaffolded(page, "acmeapp");

  const goMod = await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const dir = await root.getDirectoryHandle("acmeapp");
    const file = await (await dir.getFileHandle("go.mod")).getFile();
    return file.text();
  });
  expect(goMod).toContain("module github.com/acme/acmeapp");
});

test("refuses to scaffold over an existing tree", async ({ page }) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();
  await expectScaffolded(page, "myapp");

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
  await expectScaffolded(page, "myapp");

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
  const app = Object.keys(parsed.entries.myapp.entries).sort();
  expect(app).toContain("go.mod");
  expect(app).toContain("engine");
  expect(app).toContain("proto");

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
  // Count, not a fixed number: the template grows, and pinning the total makes
  // every template addition look like a web-tier regression. The file
  // assertions below are what actually check the import.
  await expect(page.getByTestId("log")).toContainText(/imported \d+ files/);
  for (const f of EXPECTED) {
    await expect(page.getByTestId("files")).toContainText(f);
  }
});

// Events, Phase 3 — the engine's `emit` reaches the main thread (§6.3).
//
// The whole path is under test here and nowhere else: the engine calls
// `platform.Emit` inside wasm → jco routes the `devalbo:ilc/events` import to
// hosts/web/events.ts → the worker forwards over Comlink → api.ts fans out → the
// UI re-lists. Parity already proves the engine EMITS on both tiers; what it
// cannot see is whether a browser host receives it.
//
// This test is only meaningful because `runImport` no longer calls refresh():
// the file list below can be updated by nothing but the event.
test("an engine event repaints the UI with no refresh() call", async ({
  page,
}) => {
  await page.getByTestId("name").fill("myapp");
  await page.getByTestId("new").click();
  await expectScaffolded(page, "myapp");

  const bundle = JSON.stringify({
    type: "directory",
    entries: {
      "from-the-event.txt": { type: "text", content: "arrived\n" },
    },
  });
  await page.getByTestId("import").setInputFiles({
    name: "events.bft.json",
    mimeType: "application/json",
    buffer: Buffer.from(bundle),
  });

  // The event itself, with its decoded payload — so a failure distinguishes
  // "no event arrived" from "an event arrived carrying the wrong thing".
  // method 0 would mean the emitter forgot to say what caused the change.
  const log = page.getByTestId("log");
  await expect(log).toContainText(/event ilc\.data-changed — prefix "", method [1-9]/);

  // …and the consequence: the list re-read itself. Ordering is load-bearing —
  // the worker holds the event until the OPFS flush completes, so a listener
  // that lists immediately cannot observe a half-written tree.
  await expect(page.getByTestId("files")).toContainText("from-the-event.txt");
  await expect(page.getByTestId("files")).not.toContainText("myapp/go.mod");
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
  await expectScaffolded(page, "myapp");
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
