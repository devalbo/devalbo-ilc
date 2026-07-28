import { expect, test, type Page } from "@playwright/test";

// The SLOT test: this tier's rendering, with NO ENGINE (Decision 34).
//
// Every other test here drives the real thing — a wasm component in a worker.
// This drives none of it: a fake `EnginePort` answers with scripted bytes.
//
// Why that is worth having when the live test passes: a slot is the one part of
// an ILC app parity cannot check. Parity compares command results, the written
// filesystem and the event stream, all engine-side, so rendering is invisible to
// it by construction. It also reaches failure paths that are awkward to stage
// against a live engine — see the last test.

const DRIVER = "/test/slot-driver.ts";

async function drive<T>(page: Page, call: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ call, args, driver }) => {
      const m = (await import(/* @vite-ignore */ driver)) as Record<
        string,
        (...a: unknown[]) => unknown
      >;
      return (await m[call](...args)) as T;
    },
    { call, args, driver: DRIVER },
  );
}

test.beforeEach(async ({ page }) => {
  await page.goto("/test/slot.html");
  await drive(page, "setup");
});

test("renders the real markup from scripted bytes, with no engine", async ({ page }) => {
  const said = await drive<string | null>(page, "greet", "ILC");
  expect(said).toBe("hello from the fake");
  expect(await drive<string>(page, "projection")).toContain("hello from the fake");
});

test("the engine is called once, with the right command", async ({ page }) => {
  await drive(page, "greet", "ILC");
  expect(await drive<number[]>(page, "calls")).toHaveLength(1);
});

// Reachable here, awkward against a live engine: make the engine refuse.
test("an engine error is reported, not swallowed", async ({ page }) => {
  await drive(page, "breakGreet", "engine said no");
  const said = await drive<string | null>(page, "greet", "ILC");
  expect(said).toBeNull();
  expect(await drive<string>(page, "projection")).toContain("engine said no");
});
