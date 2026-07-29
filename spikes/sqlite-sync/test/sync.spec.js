import { test, expect } from "@playwright/test";

// One row of SQLITE-INDEX-PLAN.md's Phase 0 matrix per step, in the shape
// spikes/async/README.md uses. The gate is S1.3: if a query cannot be answered
// without reaching the event loop, a synchronous component import cannot return
// rows and the capability's whole shape is wrong.
//
// ONE test, not three, and the reason is itself a finding: each Playwright test
// gets a fresh browser context, and OPFS is per-context — so a durability check
// in its own test starts with an empty filesystem and fails with "no such
// table". The reload has to happen inside the test that seeded the data. The
// real web tier's browser tests share this property.

test("synchronous sqlite over OPFS, in the worker where the engine runs", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (err) => errors.push(String(err)));
  await page.goto("/");

  // S1.1 — the build has the SAH-pool VFS at all.
  const init = await page.evaluate(() => window.spike("init"));
  expect(init.version).toMatch(/^3\./);

  // S1.2 — writes work.
  const seeded = await page.evaluate(() => window.spike("seed"));
  expect(seeded.count).toBe(3);

  // S1.3 — THE GATE. Rows come back ordered, and no microtask ran during the
  // call, so nothing awaited.
  const ordered = await page.evaluate(() => window.spike("ordered"));
  expect(ordered.rows.map((r) => r[1])).toEqual(["apple", "banana", "cherry"]);
  expect(ordered.ranMicrotask).toBe(false);

  // S1.4 — recorded, not required: false here plus everything above passing
  // means the VFS needs no cross-origin isolation, and therefore imposes no
  // COOP/COEP headers on an app hosting an ILC web tier.
  console.log(`S1.4 crossOriginIsolated=${init.crossOriginIsolated}`);

  // S2 — the database survives a reload: a new page, a new worker, a new pool,
  // reading what the old one wrote. No seeding here.
  await page.reload();
  await page.evaluate(() => window.spike("init"));
  const reopened = await page.evaluate(() => window.spike("reopened"));
  expect(reopened.rows.map((r) => r[1])).toEqual(["apple", "banana", "cherry"]);
  expect(reopened.ranMicrotask).toBe(false);

  // S3.1 — what the VFS created in the OPFS root. `flushTreeToOPFS` mirrors the
  // engine's in-memory tree onto this same directory, and that tree can never
  // contain these entries: no Go code wrote them.
  const names = await page.evaluate(() => window.spike("listOpfs"));
  console.log(`S3.1 OPFS root entries: ${JSON.stringify(names)}`);
  expect(names.length).toBeGreaterThan(0);

  // S3.2 — what today's hydrate would do with it (log label S3.2): the host reads every file
  // under the root into the FileData tree at boot, and these files are held
  // open by the pool.
  const hydrate = await page.evaluate(() => window.spike("hydrate"));
  console.log(`S3.2 hydrate: ${JSON.stringify(hydrate)}`);

  // S3.3 — the write half of the flush. `writeDir` calls `createWritable` on
  // every file in the tree on every flush, and S3.2 just showed the pool's files
  // are IN that tree.
  expect(hydrate.read.length).toBeGreaterThan(0);
  const write = await page.evaluate(
    (path) => window.spike("write", path),
    hydrate.read[0].path,
  );
  console.log(`S3.3 write to ${hydrate.read[0].path}: ${JSON.stringify(write)}`);

  // S3.4 — what today's mirror flush would do to it. Either outcome is a
  // finding: the flush deletes a live index, or the flush ITSELF fails because
  // the files are locked — and the web host flushes after every command.
  const hazard = await page.evaluate(() => window.spike("hazard", ["records"]));
  console.log(`S3.4 hazard: ${JSON.stringify(hazard)}`);
  expect(hazard.attempted.length).toBeGreaterThan(0);
  expect(hazard.removed.length + hazard.refused.length).toBe(
    hazard.attempted.length,
  );

  expect(errors).toEqual([]);
});
