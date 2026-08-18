import { expect, test } from "@playwright/test";

// The commands inspector, and the THIRD front end in the parse vectors.
//
// There are now three ways to build a request: argv on the CLI, a typed line in
// the terminal, and a form here. Parsing happens BEFORE the boundary the parity
// check guards, so all three could be internally consistent while producing
// different bytes for the same intent, with every other check green.
//
// The vectors below use the same values as hosts/native/parsevector_test.go and
// test/terminal.spec.ts. Three independent front ends asserting identical bytes
// is the check.

test.beforeEach(async ({ page }) => {
  await page.goto("/commands.html");
});

// A form and a command line are the same request.
//
// `created-at` is set explicitly rather than left to the clock: `requestHex`
// reports what the FORM holds, before fill runs, so the comparison is honest
// about which values it is comparing.
test("parse vector: form input matches the CLI's bytes", async ({ page }) => {
  const hex = await page.evaluate(() => {
    const i = (window as any).inspector;
    i.select("create");
    i.set("title", "Buy milk");
    i.set("body", "two litres");
    i.set("created-at", "1700000000");
    return i.requestHex();
  });
  expect(
    hex,
    "the CLI builds a different request for the same values — see hosts/native/parsevector_test.go",
  ).toBe("0a08427579206d696c6b120a74776f206c69747265731880e2cfaa06");
});

test("parse vector: a field left empty is absent, not zero", async ({ page }) => {
  const hex = await page.evaluate(() => {
    const i = (window as any).inspector;
    i.select("create");
    i.set("title", "Buy milk");
    i.set("body", "");
    i.set("created-at", "1700000000");
    return i.requestHex();
  });
  expect(hex).toBe("0a08427579206d696c6b1880e2cfaa06");
});

// Everything on the page comes from the .proto — documentation that cannot go
// stale, because there is nothing to keep in sync.
test("the surface documents itself", async ({ page }) => {
  await expect(page.getByTestId("command-create")).toBeVisible();
  await expect(page.getByTestId("command-index")).toContainText("Write a new note");

  await page.getByTestId("command-create").click();
  await expect(page.getByTestId("detail-name")).toHaveText("create");
  // The permanent wire id and the message it decodes as — the facts a developer
  // debugging a request wants and no other view shows.
  await expect(page.getByTestId("detail-meta")).toContainText("method_id 10000");
  await expect(page.getByTestId("detail-meta")).toContainText("CreateRecordRequest");
  // Field metadata, all declared in the schema.
  await expect(page.getByTestId("field-title")).toBeVisible();
  await expect(page.getByTestId("command-detail")).toContainText("required");
  await expect(page.getByTestId("command-detail")).toContainText("note title");
  await expect(page.getByTestId("command-detail")).toContainText("positional 1");
});

// The form and the terminal are visibly the same surface.
test("copy as command line reproduces what you would type", async ({ page }) => {
  const line = await page.evaluate(() => {
    const i = (window as any).inspector;
    i.select("create");
    i.set("title", "Buy milk");
    i.set("body", "two litres");
    i.set("created-at", "");
    return i.commandLine();
  });
  expect(line).toBe('create --title "Buy milk" --body "two litres"');
});

test("running the form drives the real engine", async ({ page }) => {
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [n] of (root as any).entries()) {
      await root.removeEntry(n, { recursive: true });
    }
  });
  await page.reload();

  await page.getByTestId("command-create").click();
  await page.getByTestId("field-title").fill("From the form");
  await page.getByTestId("detail-run").click();

  await expect(page.getByTestId("command-output")).toContainText("created from-the-form", {
    timeout: 30_000,
  });
});

test("a missing required field is refused, and says which", async ({ page }) => {
  await page.getByTestId("command-create").click();
  await page.getByTestId("field-title").fill("");
  await page.getByTestId("detail-run").click();
  await expect(page.getByTestId("command-output")).toContainText("--title is required");
});

// Enum fields become selects built from the schema's own values, so the form
// cannot offer a choice the engine would reject.
test("enum fields are selects built from the schema", async ({ page }) => {
  await page.getByTestId("command-import-fs").click();
  const mode = page.getByTestId("field-mode");
  await expect(mode).toHaveRole("combobox");
  // The values declared in platform.proto's ImportMode, with the prefix every
  // one of them shares removed — the same short form the terminal completes to
  // and the encoder accepts. An inspector offering `IMPORT_MODE_MERGE` beside a
  // terminal that takes `merge` would be the odd surface out.
  await expect(mode.locator("option")).toHaveText(["unspecified", "merge", "replace"]);
  // …and the schema's declared default is preselected — even though the schema
  // spells it `IMPORT_MODE_MERGE` and the options are short. Resolving the
  // default through the same matching is what keeps those two from disagreeing.
  await expect(mode).toHaveValue("merge");
});

// The inherited verbs are here, and they are the ones with no button in the UI.
test("inherited platform verbs appear alongside the app's own", async ({ page }) => {
  for (const name of ["version", "export-fs", "import-fs", "reset-fs"]) {
    await expect(page.getByTestId(`command-${name}`)).toBeVisible();
  }
});

// A bytes field says where its value comes from rather than pretending to be a
// text box — `cli_source` is declared in the schema and rendered here.
test("a file-sourced field says so", async ({ page }) => {
  await page.getByTestId("command-import-fs").click();
  await expect(page.getByTestId("field-bundle")).toHaveAttribute(
    "placeholder",
    "path in OPFS",
  );
  await expect(page.getByTestId("command-detail")).toContainText("from file");
});
