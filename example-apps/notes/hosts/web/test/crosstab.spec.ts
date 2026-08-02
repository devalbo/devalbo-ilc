// Two tabs, one OPFS store (§6.3 follow-up).
//
// WHAT WAS ACTUALLY BROKEN, and it was worse than a missing notification: the
// web tier hydrates all of OPFS into an in-memory tree at boot and mirrors that
// tree back after every command, PRUNING anything OPFS has that the tree lacks
// (writeDir in opfs.ts). Two tabs hold two snapshots. The stale tab's next write
// therefore did not merely fail to show the first tab's note — it deleted it.
// Nothing could notice, because nothing crossed between tabs.
//
// That is measured rather than argued: with the staleness guard removed, this
// exact scenario leaves ONE note on disk after two tabs each created one.
// notes is the app that exposes it, because it lists on load and so has already
// hydrated when the other tab writes.
//
// So these tests assert the two halves of the fix in the order they matter:
// a tab that goes stale STOPS, and a tab that reloads sees the other's work.
import { expect, test, type Page } from "@playwright/test";

async function create(page: Page, title: string): Promise<void> {
  await page.getByTestId("title").fill(title);
  await page.getByTestId("create").click();
}

// The headline: a note written in one tab shows up in the other, with nobody
// pressing refresh. notes wires `onExternalChange` to a reload, so what is
// really being asserted is that the second tab learned it had to.
test("a note created in one tab appears in the other", async ({ context }) => {
  const first = await context.newPage();
  await first.goto("/");
  await expect(first.getByTestId("count")).toHaveText("0", { timeout: 30_000 });

  // Opened BEFORE the write, which is the whole point — a tab that loaded
  // afterwards would see the note by hydrating, and would prove nothing.
  const second = await context.newPage();
  await second.goto("/");
  await expect(second.getByTestId("count")).toHaveText("0", { timeout: 30_000 });

  await create(first, "From tab one");
  await expect(first.getByTestId("count")).toHaveText("1", { timeout: 30_000 });

  await expect(second.getByTestId("count")).toHaveText("1", { timeout: 30_000 });
  await expect(second.getByTestId("title-from-tab-one")).toBeVisible();
});

// And the other tab's work SURVIVES — the failure this whole mechanism exists
// to prevent. Both tabs write; each note must still be there afterwards.
//
// Without the staleness guard the second tab's create flushes a snapshot that
// never contained the first note, and the mirror prunes it off the disk. The
// note vanishes from a store nobody deleted it from.
test("a write in the second tab does not delete the first tab's note", async ({
  context,
}) => {
  const first = await context.newPage();
  await first.goto("/");
  await expect(first.getByTestId("count")).toHaveText("0", { timeout: 30_000 });

  const second = await context.newPage();
  await second.goto("/");
  await expect(second.getByTestId("count")).toHaveText("0", { timeout: 30_000 });

  await create(first, "Written first");
  await expect(first.getByTestId("count")).toHaveText("1", { timeout: 30_000 });

  // The second tab has now reloaded, so it is current and may write.
  await expect(second.getByTestId("title-written-first")).toBeVisible({
    timeout: 30_000,
  });
  await create(second, "Written second");
  await expect(second.getByTestId("count")).toHaveText("2", { timeout: 30_000 });

  // Both notes, in a third tab that hydrates from disk — the store itself, not
  // either tab's opinion of it.
  const third = await context.newPage();
  await third.goto("/");
  await expect(third.getByTestId("count")).toHaveText("2", { timeout: 30_000 });
  await expect(third.getByTestId("title-written-first")).toBeVisible();
  await expect(third.getByTestId("title-written-second")).toBeVisible();
});
