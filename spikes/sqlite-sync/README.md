# Phase 0 gate — synchronous SQLite over OPFS

`make spike-sqlite-sync` → [`scripts/spike-sqlite-sync.sh`](../../scripts/spike-sqlite-sync.sh).
The gate for [`docs/SQLITE-INDEX-PLAN.md`](../../docs/SQLITE-INDEX-PLAN.md) **D2**, run 2026-07-29.

**Result: 🟢 GREEN.** A query returns rows with **no `await` in the call path**, so a synchronous
component import can answer one. The plan proceeds as written — no JSPI, no async engine API.

**Pins measured:** `@sqlite.org/sqlite-wasm` **3.50.1-build1** · SQLite **3.50.1** · Chromium via
Playwright 1.54 · VFS **`opfs-sahpool`** (`installOpfsSAHPoolVfs`), **not** the `opfs` VFS.

---

## Fixture

One page, one dedicated worker, no engine. `worker.js` is the spike; `index.html` exists only to own the
worker and expose an RPC to Playwright. **Everything runs in the worker** — which is where the engine runs
in the real host (`dlc-platform/web/worker.ts`) and the only place `createSyncAccessHandle` is available.

---

## Test execution matrix (one row = one assertion)

| ID | Assertion | Result (2026-07-29) |
| --- | --- | --- |
| **S1.1** | the build exposes `installOpfsSAHPoolVfs`, and reports a 3.x library version | 🟢 PASS (3.50.1) |
| **S1.2** | `create table` + three `insert`s, synchronously | 🟢 PASS |
| **S1.3** | **THE GATE** — `select … order by` returns the rows sorted, **and a microtask queued immediately before the call has not run when it returns** | 🟢 PASS |
| **S1.4** | `crossOriginIsolated` during all of the above | 🟢 **false** — no COOP/COEP needed |
| **S2** | after `page.reload()` — new page, new worker, new pool — the rows are still there, still read synchronously | 🟢 PASS |
| **S3.1** | what the VFS creates in the OPFS root | 🟢 `[".ilc-spike"]` — one dot-prefixed directory, named from the `name:` option |
| **S3.2** | a `loadTreeFromOPFS`-shaped hydrate of the whole root | 🟢 reads 6 opaque files (~48 KB), **zero failures** |
| **S3.3** | `createWritable` on one of those files, which is what `writeDir` does to every file on every flush | 🔴 **`NoModificationAllowedError`** |
| **S3.4** | a `flushTreeToOPFS`-shaped mirror-prune of an entry the tree lacks | 🔴 **`NoModificationAllowedError`**; the DB itself survived |

| Roll-up | Rule | Result |
| --- | --- | --- |
| **GATE** | S1.3 passes | 🟢 **GREEN** |
| **INTEGRATION** | S3.\* describe work Phase 3 must do first | 🔶 two required exclusions, below |

---

## Findings

### 1. The gate: init is async, the query is not — and that is all D2 needed

Loading the wasm and opening the SyncAccessHandle pool are both `await`ed. That costs nothing: the
worker's boot sequence is already async (probe → hydrate → instantiate → manifest). Only the **query** has
to be synchronous, because that is what happens inside a component call, and it is.

**How it is proven, rather than asserted:** a microtask queued immediately before `db.exec` cannot run
until the current synchronous execution completes. If it had run by the time `exec` returned, control
reached the event loop, which means something awaited. It had not run. This is the same trick the async
spike used from the other side, where a tick counter had to be **greater** than zero.

**What this spike did NOT re-prove:** that a synchronous JS host import can return a value into a
jco-transpiled guest. Spike 5 already measured that — *"Sync host control (string return, no Promise) is
GREEN under sync jco"* — and the R1 failure there was specifically a **Promise** as the result. Nothing
here returns a Promise, so the two halves compose. Building a component just to watch it happen again
would have been the spike-that-duplicates-a-check that `spikes/README.md` warns about.

### 2. No cross-origin isolation, which is why the VFS choice matters

The other OPFS VFS (`opfs`) needs `SharedArrayBuffer` and therefore COOP/COEP headers on every page. The
SAH-pool VFS does not: **S1.4 measured `crossOriginIsolated === false` while every query passed.** This is
the difference between "the index adds a dependency" and "the index imposes cross-origin isolation on
every app that ever hosts an ILC web tier" — the second would have been a much bigger decision than the
one the plan describes, and it would have arrived in Phase 3.

### 3. The pool and the OPFS bridge collide, and the collision is *loud* — twice

This is the finding worth the whole spike, and it is Phase 3 work discovered before Phase 2 starts.

The engine's WASI root **is** the OPFS root, and `dlc-platform/web/opfs.ts` mirrors an in-memory FileData
tree onto it: `loadTreeFromOPFS` reads every file into the tree at boot, and `flushTreeToOPFS` writes every
file back and **deletes anything OPFS has that the tree does not**. The pool lands right in the middle of
that:

- **Hydrate reads the pool's files** (S3.2) — successfully, which is the bad outcome. ~48 KB of opaque
  blobs enter the engine's filesystem tree, become visible to `list`-shaped code, and would be **carried
  into `export-fs` bundles** — a disposable index travelling as if it were state (§7.1 says it must not).
- **Flush then tries to rewrite them** (S3.3) and gets `NoModificationAllowedError`, because the pool holds
  them open with SyncAccessHandles. The web host flushes **after every command**, so this is not an edge
  case: with an index installed and no exclusion, every command that writes would throw.
- **The mirror-prune is refused too** (S3.4) if the tree somehow lacks them.

**Required in Phase 3, and now specified rather than discovered late:** `opfs.ts` skips the pool directory
on **both** sides. One name, chosen by us — the directory is `"." + name` from
`installOpfsSAHPoolVfs({ name })`, so the platform picks it (`.ilc-index`) rather than inheriting the
default `.opfs-sahpool`.

**The good news is the failure mode.** Every collision throws. Nothing silently corrupted the database, and
the query after the refused delete still worked (S3.4, `queryError: null`). A silent version of this —
a flush that quietly truncated a page of the index — would have been a bug found much later, in someone's
data.

### 4. Test isolation: OPFS is per browser context

The durability check (S2) was first written as its own `test()` and failed with `no such table: notes`.
Playwright gives each test a fresh context, and OPFS is per-context, so the second test started with an
empty filesystem. The reload has to happen **inside** the test that seeded the data.

Not specific to this spike — it constrains any browser test that wants to observe persistence, including
the ones Phase 3 will write.

---

## Layout

| Path | What |
| --- | --- |
| `worker.js` | the spike — init, the sync query path, the microtask probe, the three OPFS-collision probes |
| `index.html` | owns the worker, exposes `window.spike(op, …)` |
| `test/sync.spec.js` | the matrix above, one `test()` because OPFS is per-context |
| `vite.config.js` | why sqlite-wasm must be excluded from dep optimization, and why there are no COOP/COEP headers |

```bash
make spike-sqlite-sync
```
