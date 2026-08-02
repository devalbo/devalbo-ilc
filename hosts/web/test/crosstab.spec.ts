// The stale-tab guard (§6.3 cross-tab follow-up), tested where nothing hides it.
//
// notes reacts to `onExternalChange` by reloading, which makes the guard
// invisible there — the tab is gone before it can refuse anything. dlc wires no
// such reaction, so its tabs go stale and stay stale, which is exactly the state
// this needs to observe.
//
// WHY A REFUSAL AND NOT A WARNING. The web tier hydrates all of OPFS into an
// in-memory tree at boot and mirrors it back after every command, PRUNING
// anything OPFS holds that the tree lacks (writeDir in opfs.ts). A tab whose
// snapshot predates another tab's write will therefore DELETE that write on its
// next command — including a command that only reads, because every execute
// flushes. Refusing is the only response that does not destroy data.
import { expect, test } from "@playwright/test";

test("a tab whose store changed elsewhere refuses to run commands", async ({
  context,
}) => {
  const first = await context.newPage();
  await first.goto("/");
  await first.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    for await (const [name] of (root as any).entries()) {
      await root.removeEntry(name, { recursive: true });
    }
  });
  await first.reload();
  await expect(first.getByTestId("files")).toContainText("empty");

  // Opened before the write, so it holds a snapshot of an empty store.
  const second = await context.newPage();
  await second.goto("/");
  await expect(second.getByTestId("files")).toContainText("empty");

  await first.getByTestId("name").fill("firsttab");
  await first.getByTestId("new").click();
  await expect(first.getByTestId("log")).toContainText("scaffolded firsttab", {
    timeout: 60_000,
  });

  // The second tab now knows it is behind. Its next command must be refused
  // rather than served from a snapshot that would prune `firsttab/` on flush.
  await second.getByTestId("name").fill("secondtab");
  await second.getByTestId("new").click();
  await expect(second.getByTestId("log")).toContainText("out of date", {
    timeout: 60_000,
  });

  // And the first tab's work is still on disk — read directly, bypassing both
  // engines, because this is a claim about the store and not about a UI.
  const names = await first.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const out: string[] = [];
    for await (const [name] of (root as any).entries()) out.push(name);
    return out.sort();
  });
  expect(names).toContain("firsttab");
  expect(names).not.toContain("secondtab");
});
