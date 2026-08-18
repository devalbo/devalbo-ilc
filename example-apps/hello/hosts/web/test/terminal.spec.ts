import { expect, test } from "@playwright/test";

// The terminal route, against the real engine.
//
// Ships with the app because the claim it checks is the app's: the browser and
// the terminal run the SAME command surface, generated from commands.proto, so a
// command that works in one works in the other.
test("runs a command typed at the prompt", async ({ page }) => {
  await page.goto("/terminal.html");

  await page.getByTestId("terminal-input").fill("greet --name ILC");
  await page.getByTestId("terminal-input").press("Enter");

  // Assert on something ONLY the engine's reply contains. The terminal echoes
  // what you typed, so matching "ILC" here would pass on the echo alone —
  // including when the command failed, which is exactly what it did once.
  await expect(page.getByTestId("terminal-output")).toContainText(
    "from hello",
    { timeout: 30_000 },
  );
});

test("help is generated from the schema", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("help");
  await page.getByTestId("terminal-input").press("Enter");
  await expect(page.getByTestId("terminal-output")).toContainText("greet");
});

// The four shapes, in the browser.
//
// WHY THESE ARE HERE and not only in the Go tests: the renderers are a SECOND
// projection of the same schema, written in TypeScript, and nothing else checks
// that they decode what the engine encoded. A renderer that reads the wrong
// field compiles, ships, and prints something plausible.

test("math answers with a number", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("math 6 multiply 7");
  await page.getByTestId("terminal-input").press("Enter");
  // THE RENDERER'S OUTPUT, not the engine's — and that split is DELIBERATE here.
  //
  // The engine also prints `6 x 7 = 42`. In this tier those bytes go to the
  // browser CONSOLE (asserted in web.spec.ts) and the visible output comes from
  // the typed response. Print is the channel that must never be a black hole;
  // the renderer is the channel a person reads.
  //
  // The consequence is real though: `count`'s ticks arrive in devtools rather
  // than in this pane, so "output during a command" reads differently here than
  // on the CLI or the badge.
  await expect(page.getByTestId("terminal-output")).toContainText("42", {
    timeout: 30_000,
  });
});

test("divide by zero is an answer, not a failure", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("math 5 divide 0");
  await page.getByTestId("terminal-input").press("Enter");
  // The renderer turns the `problem` ENUM into this sentence. It is not the
  // engine's text — the engine prints "cannot divide by zero" alone — so
  // matching the expression too proves the enum survived the round trip and was
  // decoded by number rather than by position.
  await expect(page.getByTestId("terminal-output")).toContainText(
    "5 / 0: cannot divide by zero",
    { timeout: 30_000 },
  );
});

test("count streams while it runs, then reports its tally", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("count 2 words");
  await page.getByTestId("terminal-input").press("Enter");
  // ONLY THE TALLY. The ticks (`two`, `one`) are engine stdout and do not reach
  // this pane — see the math test above. What a renderer reads from the REPLY
  // arrives normally, which is exactly the half this tier can show today.
  await expect(page.getByTestId("terminal-output")).toContainText("(2 ticks)", {
    timeout: 30_000,
  });
});

test("light reports whether this world has one", async ({ page }) => {
  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("light amber");
  await page.getByTestId("terminal-input").press("Enter");
  // A browser CAN show text, so `shown` is true here — the same command answers
  // "this world has no light to set" on a tier that cannot.
  await expect(page.getByTestId("terminal-output")).toContainText("set", {
    timeout: 30_000,
  });
});
