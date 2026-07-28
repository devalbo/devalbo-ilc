import { expect, test } from "@playwright/test";

// The terminal ROUTE, against a real engine.
//
// terminal.spec.ts drives the command surface with a fake port and asserts
// request BYTES; this asserts the page works — typing runs commands against the
// same wasm component the buttons drive, and what comes back is rendered.
//
// It is also the test surface the terminal was partly built for: driving the app
// by typing is closer to what a user does than reaching into modules with
// page.evaluate.

test.beforeEach(async ({ page }) => {
  await page.goto("/terminal.html");
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [name] of (root as any).entries()) {
      await root.removeEntry(name, { recursive: true });
    }
  });
  await page.reload();
});

async function type(page: any, line: string) {
  await page.getByTestId("terminal-input").fill(line);
  await page.getByTestId("terminal-input").press("Enter");
}

test("runs commands against the real engine", async ({ page }) => {
  const out = page.getByTestId("terminal-output");

  await type(page, "list");
  await expect(out).toContainText("(no notes)");

  await type(page, 'create --title "Buy milk" --body "two litres"');
  // The id is derived in engine/commands.go — not in any TypeScript — so a
  // matching slug is evidence the browser ran the same logic as the CLI.
  await expect(out).toContainText("created buy-milk", { timeout: 30_000 });

  await type(page, "list");
  await expect(out).toContainText("buy-milk");

  await type(page, "open --id buy-milk");
  await expect(out).toContainText("two litres");
});

// The inherited platform verbs are reachable here even though the UI exposes no
// button for them — which is a large part of why a terminal is worth having.
test("inherited verbs work with no UI for them", async ({ page }) => {
  const out = page.getByTestId("terminal-output");
  await type(page, 'create --title "Buy milk"');
  await expect(out).toContainText("created buy-milk", { timeout: 30_000 });

  // Assert on the VERSION STRING, not the app name: the banner already says
  // "notes", and the terminal echoes what you type, so `toContainText("notes")`
  // passed even while `version` was not a registered command at all. It matched
  // the banner. Two false passes in this file came from asserting on text the
  // page prints for other reasons.
  await type(page, "version");
  await expect(out).toContainText("notes 0.1.0");
});

test("help is generated from the schema", async ({ page }) => {
  await type(page, "help");
  await expect(page.getByTestId("terminal-output")).toContainText("Write a new note");
});

test("history recalls the last command", async ({ page }) => {
  await type(page, "list");
  // Wait for it to FINISH: the first command boots the engine, and the prompt is
  // disabled while a command runs, so pressing Up before then presses nothing.
  await expect(page.getByTestId("terminal-output")).toContainText("(no notes)", {
    timeout: 30_000,
  });
  await page.getByTestId("terminal-input").press("ArrowUp");
  await expect(page.getByTestId("terminal-input")).toHaveValue("list");
});

// A write from the terminal is a write from "somewhere else" as far as the main
// page is concerned — so this is the event loop working across a route.
test("a terminal write is a real write", async ({ page }) => {
  await type(page, 'create --title "From the terminal"');
  await expect(page.getByTestId("terminal-output")).toContainText(
    "created from-the-terminal",
    { timeout: 30_000 },
  );

  await page.goto("/");
  await expect(page.getByTestId("count")).toHaveText("1");
  await expect(page.getByTestId("list")).toContainText("From the terminal");
});

// The prompt is focused on arrival: this page IS a prompt, and having to click
// before typing is friction with no purpose.
test("the prompt has focus on load", async ({ page }) => {
  await expect(page.getByTestId("terminal-input")).toBeFocused();
  // …and typing goes straight in, with no click first.
  await page.keyboard.type("help");
  await expect(page.getByTestId("terminal-input")).toHaveValue("help");
});
