import { expect, test } from "@playwright/test";

// The web slot, against the real engine.
//
// What it demonstrates is the SEMANTIC RENDER PATH (Decision 34): the engine
// emits what is true, and this tier decides what that looks like. The same
// events drive an ASCII board in the terminal, which shares no code with this.

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

test("the board repaints from an EVENT, not from the click", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });

  await page.getByTestId("square-5").click();

  // `play` deliberately does not redraw. The only thing that repaints this
  // board is `game.state-changed`, so if events stop working the UI visibly
  // freezes rather than the capability rotting unnoticed.
  await expect(page.getByTestId("square-5")).toHaveText("X");
  await expect(page.getByTestId("status")).toContainText("O to play");
});

test("the engine's rules are the only rules", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });
  await page.getByTestId("square-5").click();
  await expect(page.getByTestId("square-5")).toHaveText("X");

  // Disabled because the engine FILLED it — this slot did not decide the move
  // would be illegal, it rendered a square that is no longer empty.
  await expect(page.getByTestId("square-5")).toBeDisabled();
});

test("a win highlights the line the engine named", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });
  for (const sq of [1, 4, 2, 5, 3]) {
    await page.getByTestId(`square-${sq}`).click();
  }

  await expect(page.getByTestId("status")).toContainText("X wins");
  // The class comes from `winningLine`, which the ENGINE computed. This slot
  // cannot find a winning line and does not try.
  for (const sq of [1, 2, 3]) {
    await expect(page.getByTestId(`square-${sq}`)).toHaveClass(/won/);
  }
  await expect(page.getByTestId("square-4")).not.toHaveClass(/won/);
});

// COLD START (D5): events are ephemeral, so a slot rendering only from the
// stream would show an empty board on reload.
test("the board survives a reload", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });
  await page.getByTestId("square-5").click();
  await expect(page.getByTestId("square-5")).toHaveText("X");

  await page.reload();
  await expect(page.getByTestId("square-5")).toHaveText("X", { timeout: 30_000 });
  await expect(page.getByTestId("status")).toContainText("O to play");
});

// A move made outside this page — the reactivity loop across front ends.
test("a move from the terminal repaints the board", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });

  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("play 5");
  await page.getByTestId("terminal-input").press("Enter");
  await expect(page.getByTestId("terminal-output")).toContainText("O to play", {
    timeout: 30_000,
  });

  await page.goto("/");
  await expect(page.getByTestId("square-5")).toHaveText("X", { timeout: 30_000 });
});

// TIME TRAVEL, with no per-slot code.
//
// `state <n>` reached the browser through the generated command surface the
// moment the rpc gained an `at` field — no TypeScript was written for it. That
// is the payoff for the surface being data: a capability added in the schema
// arrives on every tier at once.
test("reviewing a prior state works in the browser, unwritten", async ({ page }) => {
  await expect(page.getByTestId("status")).toContainText("X to play", { timeout: 30_000 });
  for (const sq of [5, 1, 9]) {
    await page.getByTestId(`square-${sq}`).click();
  }
  await expect(page.getByTestId("status")).toContainText("move 4");

  await page.goto("/terminal.html");
  await page.getByTestId("terminal-input").fill("state 1");
  await page.getByTestId("terminal-input").press("Enter");

  // The board as of move 1: X in the middle, square 1 still empty.
  const out = page.getByTestId("terminal-output");
  await expect(out).toContainText("O to play (move 2)", { timeout: 30_000 });
  await expect(out).toContainText(" 1 | 2 | 3 ");
});
