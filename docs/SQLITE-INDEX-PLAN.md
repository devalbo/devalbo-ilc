# The SQLite index — implementation plan (§6.2, §7.1)

**Status: PHASE 0 GREEN (2026-07-29).** The gate is passed and the plan proceeds as written; Phases 1–6
are unbuilt. Written in the shape of
[`EVENTS-PLAN.md`](./EVENTS-PLAN.md), [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md) and
[`ENVIRONMENT-PLAN.md`](./ENVIRONMENT-PLAN.md): design decisions first, phases that each leave the tree
green, and nothing claimed until it has been broken on purpose.

**The decision that gated the whole plan — whether a browser can answer a query *synchronously* — is
settled 🟢** ([`spikes/sqlite-sync/`](../spikes/sqlite-sync/README.md), `make spike-sqlite-sync`). D2 holds.
The spike also found work Phase 3 must do first, and it is not the work anyone expected: see D9.

How an app stops reading every file to answer a question — **without the answer depending on which tier it
is running on**.

---

## 1. Why now

The index is the last unbuilt item in Decision 12's capability set, and the two things that blocked it have
both landed.

- **`unavailable` is expressible.** §7.1 has always required the index to be survivable, and until the
  environment manifest there was no way to say "no index" — absence was a linking problem, not a data
  question. Now it is a field (Decision 32, `ENVIRONMENT-PLAN.md` D6 explicitly deferred it: *"the index
  lands with the index"*).
- **The fallback already exists and every tier runs it.** `example-apps/notes/engine/commands.go`
  `handleListRecords` is a full directory scan, and its comment says what it is for: *"When the index
  lands, this becomes the `unavailable` branch rather than being rewritten."* So the degraded path is not
  something this plan has to invent and then hope works — it is the only path today, exercised by every
  test in the repo. The index is the branch being added, which is the safer direction.

**What is actually forcing it.** Notes' `open` / `update` / `rebuild-index` are blocked on it
(`DEVALBO-DLC-GO-TASKS.md`), and `list-records` reads every file on disk to produce a list of titles. At
notes' size that is free. The reason to build it anyway is not performance — it is that **the index is the
first capability that can be present on one tier and absent on another while both tiers must still be
correct.** The filesystem could go away on the web tier, and the answer was to unregister the verbs; the
command simply stops existing. An index going away must change *nothing an app or a user can observe*,
only how long it took. That is a different and harder contract, and nothing in the repo tests it yet.

### 1.1 Settled: a host capability, not an engine-built index file

The obvious cheaper alternative deserves stating, because it is genuinely attractive and this plan rejects
it: **the engine could maintain its own index file** — one canonical-JSON or proto document holding the
projected fields — using nothing but the filesystem it already has. No new import, no new WIT, no
per-tier binding, and it would work on WAMR, where SQLite will not.

**Rejected, and the deciding argument is the one the task list already names:** a fallback must return
*identical* results to the fast path, because a fallback that answers differently is a second
implementation. A scan fallback is unavoidable — embedded has no SQLite and never will — so the moment an
index exists there are two implementations of every query. An engine-built index file would make it
**three**, and the third would be a query engine we wrote ourselves, in the engine, under TinyGo, without
reflection. Host-provided SQLite means one query *language* and one fallback to reconcile it with.

The cost is honest and worth writing down: a new import on every tier that has one, ~1 MB of
`sqlite-wasm` in the web bundle, and a capability that is permanently absent on the tier the architecture
exists to reach. See §4.

---

## 2. Design decisions

### D1 — The index is an IMPORT, and the app owns its schema

The platform owns transport and availability; **the app owns the tables, the SQL, and what a row means.**
The platform cannot know an app's columns, and a generic "entity/attribute" table that tried to would be a
schema nobody chose, imposed on every app.

So the boundary carries **SQL text and arguments**, not a query DSL:

```go
platform.HasIndex() bool
platform.IndexExec(sql string, args ...any) error            // DDL + writes
platform.IndexQuery(sql string, args ...any) (Rows, error)   // reads
platform.SetIndexRebuilder(func() error)                     // the app walks its own files
```

`SetIndexRebuilder` mirrors `SetVersion`: the platform owns the *verb* (`rebuild-index`, id 200), the app
supplies the *knowledge*. Only the app knows that its records live under `records/` and which fields are
worth projecting.

**Why SQL text and not something typed:** §6.2 keeps SQL rather than `wasi:keyvalue` for exactly one
reason — `ORDER BY`. A typed query API that can express ordering, filtering and limits is a query language
with extra steps, and it would be ours to specify and evolve. SQL is already specified.

### D2 — The host must answer SYNCHRONOUSLY — **CONFIRMED (2026-07-29)**

> **Measured, not assumed.** `spikes/sqlite-sync/` runs `@sqlite.org/sqlite-wasm` 3.50.1-build1 under the
> **`opfs-sahpool`** VFS in a dedicated worker: `select … order by` returns sorted rows, and a microtask
> queued immediately before the call has **not** run when it returns — so nothing reached the event loop.
> Init (wasm load, pool open) is async and that costs nothing; the worker's boot is already async.
> Falsified by inserting one `await Promise.resolve()` into the probe and watching it go red.
>
> Two things settled with it: **no cross-origin isolation is required** (`crossOriginIsolated === false`
> throughout, so the index imposes no COOP/COEP headers on any app hosting an ILC web tier — the `opfs`
> VFS would have), and the component half needs no new evidence, because Spike 5 already measured that a
> *sync* host import returning a value is green under sync jco. Its R1 failure was a **Promise** as the
> result, and nothing here returns one.


Events had no return value on purpose (Decision 33): a return invites the host to answer, and waiting on a
host answer means blocking the browser worker inside a synchronous component call. **A query has to
return.** There is no fire-and-forget version of "give me the rows".

This does not reopen Decision 33 — that rule was about *announcements*, where a return value would create
a dependency that has no reason to exist. It does put the full weight of the capability on one
unvalidated claim: **that `@sqlite.org/sqlite-wasm` can answer a query synchronously inside the worker
where the engine runs.** The OPFS SyncAccessHandle-pool VFS is designed for precisely this, and the worker
is already where everything happens — but this repo does not take toolchain claims on faith
(`AGENTS.md` §5), and jco's async-import path is refused (Decision 22, `WASI-UPGRADES.md`).

**Phase 0 is a spike, and it is a gate.** If a browser cannot answer synchronously, the options are JSPI
(refused, and it would make every query async on one tier only — the divergence this architecture exists
to prevent) or *no index on the web tier at all* (defensible: the manifest says absent, the scan runs, and
the capability ships native-only). What this plan will not do is make the engine's query API async
everywhere to accommodate one host.

### D3 — The boundary is flat bytes, mirroring `execute` and `events`

```wit
interface index {
  // request = IndexRequest proto, response = IndexResponse proto
  query: func(request: list<u8>) -> list<u8>;
}
```

Not §6.2's original sketch (`execute-query: func(sql: string, params: list<string>) -> result<string, string>`).
Two changes, both forced:

- **No `result<>`, no rich types.** A WIT variant requires the Component Model and strands WAMR
  (Decision 31). The error rides *inside* the response message, the way `command-result` already carries
  failure so response messages do not need an error field.
- **Rows are protobuf, not rows-JSON.** §7.2 is one serialization story, and rows-JSON would make the
  engine parse arbitrary JSON — which under TinyGo, without reflection, means writing a JSON parser or
  pulling in something that does not build. go-lite gives canonical JSON for *known messages*, which
  arbitrary result rows are not.

Values mirror SQLite's five storage classes and nothing more:

```proto
message IndexValue {
  oneof value { bool null = 1; int64 integer = 2; double real = 3; string text = 4; bytes blob = 5; }
}
message IndexRow { repeated IndexValue values = 1; }
```

A `oneof` here is fine and does not touch Decision 29: that decision banned a oneof for *command
dispatch*, and explicitly kept it for response variants.

### D4 — Absence removes ACCELERATION, never a verb

The filesystem's absence unregisters its block, because `export-fs` with no filesystem cannot do anything
at all. **The index is the opposite case:** `list-records` works perfectly well without one. So:

> **Register a verb from the manifest only when the verb cannot work at all without the capability.**

Under that rule, exactly one verb registers from the index: `rebuild-index` (id 200), which has nothing to
rebuild when there is no index. Every app verb stays registered on every tier. This is a genuinely
different policy from the filesystem's and the reason to write the rule down — the next capability will
be one or the other, and picking by imitation would be picking at random.

**Consequence for parity:** the command surface is *the same* with and without an index, apart from id
200. That is the correct answer and it is also a weaker check than the filesystem got — parity compares
results and the surface, and neither moves when an index appears. §D7 is how this capability gets a check
at all.

### D5 — The index is never authoritative, and "identical results" is a mechanical claim

§7.1 says the index is disposable and rebuildable. That is easy to say and easy to violate: the first
time a handler reads a *value* out of the index rather than a *key*, the index has become a second source
of truth, and a stale one will be served to a user.

**The rule:** an index query returns identifiers and ordering. The record itself is read from its file.

That sounds wasteful and mostly is not — the point of the index is to avoid reading files that were never
going to be in the answer. Where it genuinely bites (a list view showing titles for 10,000 records), the
projection is a *cache*, and a cache that disagrees with a file is a bug the rebuild fixes. Start with
identifiers only; widen when something measurably needs it, and only for fields a stale value cannot
mislead anyone about.

### D6 — Write order, and no lock file yet

§7.1's write flow is `create <id>.lock` → write `<id>.json` → update the index → remove `<id>.lock` →
emit. This plan builds it **without the lock file**, deliberately:

- Within one engine instance, commands are serialized. There is no concurrency to protect against.
- The lock guards a *second writer* — two tabs, or a CLI running against the same directory as a GUI.
  Neither exists today, and a lock file nothing can contend for is a branch nothing tests.
- What a crash between the JSON write and the index update leaves behind is a **stale index**, which is
  exactly the state `rebuild-index` exists to repair. The failure mode the lock prevents is a torn
  *file*, which is a different problem with a different fix (write-then-rename), and worth doing when
  something can actually tear.

Order still matters and is not negotiable: **file first, index second, event last.** A subscriber that
re-reads on the event must find both already consistent, and if the process dies in between, the truth is
on disk and the derived thing is what is behind.

### D7 — Index parity: same vectors, index on and off, results identical

The check this capability needs does not exist, and D4 explains why the existing ones cannot supply it:
the surface does not change, so surface parity is blind, and native/wasm parity compares two tiers that
would both have an index.

So `verify-index-parity.sh`, mirroring `verify-parity.sh`: **run the same method vectors twice against the
native engine — once with the index present, once with the manifest saying absent — and require
byte-identical `command-result` output.** Any divergence is a real bug by construction, because the only
difference between the runs is a capability the user is not supposed to be able to observe.

It will fire on its first run and the reason is worth predicting: SQLite's default `BINARY` collation and
Go's string comparison agree, but `ORDER BY title COLLATE NOCASE` and a Go `sort.Slice` on raw strings do
not, and neither does SQLite's placement of `NULL` versus a Go zero value. Those are the bugs this check
exists to catch, and they are invisible to every other check in the repo.

Falsifiable the way `verify-parity-selftest.sh` is: change the SQL's ordering, watch it go red.

### D8 — Native is `modernc.org/sqlite`; web is `sqlite-wasm`; embedded is absent forever

| Tier | Binding | Notes |
| --- | --- | --- |
| native (CLI/desktop) | `modernc.org/sqlite` — pure Go, no cgo | in the **host**, not the engine; the engine calls the seam |
| web | `@sqlite.org/sqlite-wasm` on OPFS, in the worker | D2's bet; ~1 MB added to `@devalbo/dlc-web` |
| WAMR / embedded | **absent** — the manifest says so | not a gap; the tier that proves the fallback is real |

The dependency goes in the **host**, never the engine. `modernc.org/sqlite` in the engine's module graph
would follow every app into a TinyGo build that cannot use it.

### D9 — The pool directory is excluded from the OPFS bridge, on BOTH sides

*Added 2026-07-29, from the Phase 0 spike — this was not in the original plan and is the largest piece of
Phase 3.*

The engine's WASI root **is** the OPFS root, and `dlc-platform/web/opfs.ts` mirrors a FileData tree onto
it: hydrate reads every file in at boot, flush writes every file back and deletes whatever the tree lacks.
The SAH pool puts its files in that same root, held open with SyncAccessHandles. Measured consequences:

| What the bridge does | What happens with an index installed |
| --- | --- |
| `loadTreeFromOPFS` reads every file | **succeeds** — ~48 KB of opaque blobs enter the engine's tree, and would ride along in `export-fs` bundles |
| `writeDir` calls `createWritable` on every file, every flush | **`NoModificationAllowedError`** — and the web host flushes after *every* command |
| `writeDir` prunes entries the tree lacks | **`NoModificationAllowedError`** |

So `opfs.ts` skips the pool directory on both sides. **The platform names it** — the directory is
`"." + name` from `installOpfsSAHPoolVfs({ name })`, so it is `.ilc-index` rather than the default
`.opfs-sahpool`, and the exclusion matches a name we chose instead of one a dependency picked.

Two things worth keeping from how this surfaced. The hydrate **succeeding** is the bad outcome — a
disposable index quietly becoming part of an app's exported state contradicts §7.1 far more seriously than
a thrown error would. And every one of these failures is **loud**: nothing silently corrupted the database,
and the query after a refused delete still worked. A version of this that quietly truncated a page of the
index would have been found in someone's data instead of in a spike.

---

## 3. Phases

Each leaves the tree green. **No phase is done until something can be broken on purpose and observed
going red** (`AGENTS.md` §5).

### Phase 0 — the synchronous-query spike (a GATE, not a phase of work) — **🟢 GREEN (2026-07-29)**

`spikes/sqlite-sync/` · `make spike-sqlite-sync` · [findings](../spikes/sqlite-sync/README.md).

Nine assertions in a worker: the SAH-pool VFS installs, writes work, `SELECT … ORDER BY` returns sorted
rows **with no microtask having run**, the data survives a reload, and three probes reproduce what the OPFS
bridge would do to a live pool. Falsified by inserting one `await` into the probe.

**Two things changed as a result**, neither of them the gate itself: D2 gained its evidence and D9 was
added — the collision between the pool and `opfs.ts` is now specified work in Phase 3 rather than a
surprise inside it. The jco half was deliberately **not** built: Spike 5 already covers a sync import
returning a value, and duplicating a live check is what `spikes/README.md` warns about.

*What would have stopped the plan:* a red S1.3, meaning web ships `index: absent` and the capability is
native-only, or §1.1 gets re-opened with new information. Not JSPI, and not an async engine API — D2.

### Phase 1 — the manifest field and the accessor

| File | Change |
| --- | --- |
| `dlc-platform/proto/devalbo/ilc/v1/platform.proto` | `Index { Availability availability = 1; }`, `Environment.index = 3` |
| `dlc-platform/environment.go` | `HasIndex()`, alongside `HasFilesystem()` |
| `dlc-platform/boot.go` | `BootOptions.Index` — a host states it, the platform never infers it |
| `dlc-platform/environment_test.go` | unset reads absent; present reads present |

Nothing queries anything yet. This is the smallest change that makes "there is no index" *sayable*, and it
is deliberately its own phase so the schema lands before two things depend on it.

*Falsify:* have `HasIndex()` read `UNSPECIFIED` as present → the absent-reads test goes red.

### Phase 2 — the seam and the native binding

| File | Change |
| --- | --- |
| `dlc-platform/wit/ilc.wit` | `interface index { query: func(request: list<u8>) -> list<u8>; }`, imported by `world engine` |
| `dlc-platform/proto/devalbo/ilc/v1/platform.proto` | `IndexRequest` / `IndexResponse` / `IndexRow` / `IndexValue`; `RebuildIndex` claiming **id 200** |
| `dlc-platform/index.go` | `HasIndex` / `IndexExec` / `IndexQuery` / `SetIndexRebuilder`; `handleRebuildIndex` |
| `dlc-platform/caps_native.go` · `caps_wasip2.go` | the second capability through the seam — direct call vs WIT import |
| `dlc-platform/commands.go` | `blockIndexLo/Hi = 200, 299`; `syncCapabilityVerbs` registers the block from `HasIndex()` |
| `hosts/native/…` (and the template's) | `modernc.org/sqlite` opened at boot, passed in `BootOptions` |
| `dlc-platform/proto/method-ids.lock` | one new line, re-blessed deliberately |

The seam is why this phase is cheap: `caps_native.go` / `caps_wasip2.go` already exist and already prove
the pattern with one capability. This adds a second and finds out whether the pattern generalizes — which
is worth knowing before display or network arrives.

*Falsify:* drop the `HasIndex()` guard in `syncCapabilityVerbs` → `rebuild-index` is registered on a host
with no index and fails as a runtime error rather than being absent; call `IndexQuery` with no index →
must return a clean "unavailable", not a nil-deref.

### Phase 3 — the web binding

| File | Change |
| --- | --- |
| `dlc-platform/web/index.ts` | sqlite-wasm init (`name: "ilc-index"`), the sync query path, the `devalbo:ilc/index` import |
| `dlc-platform/web/opfs.ts` | **D9** — skip `.ilc-index` in both `readDir` and `writeDir` |
| `dlc-platform/web/worker.ts` | probe → open the DB → **manifest with `index`** → commands, in that order |
| `hosts/web/vite.config.ts` (and the preset) | `optimizeDeps.exclude` for `@sqlite.org/sqlite-wasm` — its ESM entry locates its own `.wasm` relative to its module URL, which the dep optimizer breaks |
| `hosts/web/test/` | a query answers; the DB fails to open; **an `export-fs` bundle contains no index files** |

**D9 comes first in this phase, not last.** Without the exclusion, every command's flush throws once the
pool exists — so the order is exclusion, then VFS, then the import.

The probe mirrors the OPFS one and inherits its known gap: a Playwright stub cannot reach the worker's
global scope, so the *reaction* to an absent index is testable and the *detection* is one `try/catch`
(`ENVIRONMENT-PLAN.md` §6.4). Do not invent a production test seam to close it; it was declined once
already.

*Falsify:* make the DB open throw → the manifest reports absent, the verb unregisters, `list-records`
still answers.

### Phase 4 — notes uses it, and the fallback becomes a branch

| File | Change |
| --- | --- |
| `example-apps/notes/engine/commands.go` | schema DDL at rebuild; `handleCreateRecord` / `handleDeleteRecord` maintain the index; `handleListRecords` branches on `HasIndex()` |
| `example-apps/notes/proto/notes/v1/commands.proto` | `RebuildIndex` claiming reserved **10005** |
| `example-apps/notes/engine/commands_test.go` | both branches, over the same fixtures |

`handleListRecords` gets its index branch and **keeps the scan verbatim** — the comment in that function
has been waiting for this and says the scan becomes the `unavailable` branch rather than being rewritten.
Per D5 the query returns ids and ordering; the records are still read from their files.

*Falsify:* delete the index-maintenance call from `handleCreateRecord` → a created record is missing from
the indexed listing and present in the scanned one, which is Phase 5's check firing early.

### Phase 5 — index parity

| File | Change |
| --- | --- |
| `scripts/verify-index-parity.sh` | the same vectors, index on and off, byte-identical results |
| `verify/parity/method-vectors.json` | vectors that exercise ordering, empty results, and a deleted record |
| `scripts/ci.sh` | the new suite |

D7 in full, including watching it fail. Expect the collation mismatch on the first run; that is the check
paying for itself rather than a setback.

*Falsify:* `ORDER BY title COLLATE NOCASE` in the SQL against a case-sensitive Go sort → red.

### Phase 6 — write it down, and the dogfood pass

- `AGENTS.md` §3·7: the D4 registration rule, the D5 never-authoritative rule, the D6 write order.
- `DEVALBO-ILC-GO-PLAN.md` §6.2 / §7.1: replace the sketched WIT with what shipped; record the D3 shape
  change and the D6 deferral of the lock file.
- **Dogfood review** (the cadence is "when a capability lands"): `dlc` will *not* adopt this — it has no
  collection to index — and that is a legitimate answer rather than drift. Record it as one, because the
  next reviewer will otherwise re-derive it. notes is the consumer; tictactoe stays index-free
  deliberately, the same way it stays eager on registration.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| ~~**The web cannot answer synchronously**~~ | ~~the capability's whole shape assumes it~~ | **CLOSED 2026-07-29** — Phase 0 green, and it cost one afternoon rather than two phases |
| **The index rides along in `export-fs`** | a disposable index becomes part of an app's state, contradicting §7.1 — and it does so *silently*, because the hydrate succeeds | D9 exclusion, plus a browser test asserting a bundle has no index files |
| **The flush throws after every command** | the pool holds its files open; `writeDir` rewrites every file every time | D9, and it is the first thing Phase 3 does |
| **The index becomes authoritative** | one handler reads a value instead of an id, and a stale row is served as truth | D5; ids and ordering only, widen deliberately |
| **The fallback diverges from the fast path** | invisible — both paths return *a* plausible answer, on different tiers | D7 index parity, watched failing |
| **Collation and NULL ordering** | SQL and Go agree just often enough to look correct in tests | D7 predicts it; the first vectors target it |
| **A stale index after a crash** | the derived thing disagrees with the files and nothing notices | D6 order; `rebuild-index`; the index is disposable by contract |
| **`modernc.org/sqlite` reaching the engine's module graph** | it follows every scaffolded app into a TinyGo build that cannot use it | D8 — the dependency is host-side; `verify-scaffold.sh` already checks the graph |
| **1 MB of sqlite-wasm on every web app** | every app pays for a capability some do not use | accepted; revisit as a lazy import if an app complains |
| **A second writer arrives quietly** | D6 deferred the lock; two tabs would corrupt an index with no warning | the index is rebuildable, and cross-tab delivery is already an open task — do them together |
| **Apps branching on the tier** | `HasIndex()` is a legitimate branch that looks exactly like the illegitimate one | Decision 33 D3 — branch on what you must DO, never on who is listening |

---

## 5. What this plan does NOT do

- **Not embedded.** WAMR reports absent and runs the scan. That is the design working, not a gap.
- **Not `wasi:keyvalue`.** §6.6 keeps SQL for `ORDER BY`; revisit if the query surface stays trivial.
- **No query builder, no ORM, no migrations.** The app writes SQL; a schema change is a rebuild, because
  the index is disposable. That property is worth more than migration tooling.
- **No lock file, no write-then-rename** (D6) — until a second writer exists.
- **No sync.** §9 operates on the JSON documents and each node rebuilds locally; nothing here changes that.
- **No projection cache.** D5 — ids and ordering first.

---

## 6. Definition of done

1. [ ] `./scripts/ci.sh full` green, with `verify-index-parity` in it.
2. [x] Phase 0's spike answers the synchronous question in writing, either way —
       [`spikes/sqlite-sync/README.md`](../spikes/sqlite-sync/README.md), green, falsified.
3. [ ] An app can ask whether it has an index, and an unset manifest reads as absent.
4. [ ] Every app verb is registered on every tier; only `rebuild-index` follows the capability.
5. [ ] The same vectors produce byte-identical results with the index present and absent — **and that
       check has been watched going red.**
6. [ ] notes lists through the index natively and through the scan where there is none, with no
       user-visible difference.
7. [ ] `rebuild-index` reconstructs an index deleted out from under a running app.
8. [ ] No index query returns anything a handler treats as truth (D5), by inspection.
9. [ ] `AGENTS.md` carries the registration rule, the never-authoritative rule, and the write order.
