// hello in a browser — the same engine the CLI links, as a wasip2
// component under jco.
//
// This test ships WITH your app on purpose. The cross-tier promise ("write it
// once, run it everywhere") is only worth anything if something checks it, and
// the check has to live where the code does. If this passes and
// `./hello greet` passes, the two tiers genuinely agree.
//
// Run: make build-web && cd hosts/web && npm test
import { expect, test } from "@playwright/test";

test("greets from the browser, using the same engine as the CLI", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));

  await page.goto("/");
  await page.fill("#name", "browser");
  await page.getByTestId("greet").click();

  // The greeting text comes from engine/commands.go — not from any TypeScript.
  // That is what makes it evidence rather than decoration.
  await expect(page.getByTestId("out")).toContainText("hello, browser", {
    timeout: 30_000,
  });
  await expect(page.getByTestId("out")).toContainText("hello");

  expect(errors, "no uncaught errors while booting the engine").toEqual([]);
});

// The engine's `fmt.Println` must reach the BROWSER CONSOLE.
//
// WHY THIS IS NOT OBVIOUS. Nothing in this repo writes it: jco maps
// `wasi:cli/stdout` to `@bytecodealliance/preview2-shim`, whose browser build
// forwards writes to `console.log`. That is a dependency's behaviour, in a
// module resolved by an export condition, inside a Worker — three things that
// can change without anyone here touching a line, and all of which fail
// SILENTLY. The app keeps working; its printed output just stops existing.
//
// It matters because printing is the one channel an app can always use. A tier
// with no outlet may discard the bytes, but nothing may PREVENT the write — an
// app's `fmt.Println` must never become a trap or a black hole it cannot detect.
// The browser has a console, so here the bytes must actually arrive.
//
// Listening on `console` rather than asserting on the DOM deliberately: the
// visible output comes from the typed response, which is a different channel.
// This one is about the print.
test("the engine's stdout reaches the browser console", async ({ page }) => {
  const logged: string[] = [];
  page.on("console", (msg) => logged.push(msg.text()));

  await page.goto("/");
  await page.fill("#name", "console");
  await page.getByTestId("greet").click();

  // Wait for the command to land before reading what was logged.
  await expect(page.getByTestId("out")).toContainText("hello, console", {
    timeout: 30_000,
  });

  await expect
    .poll(() => logged.some((line) => line.includes("hello, console")), {
      message: "engine stdout never arrived in the browser console",
      timeout: 10_000,
    })
    .toBe(true);
});
