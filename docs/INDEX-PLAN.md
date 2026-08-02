# The derived index — implementation plan (§6.2, §7.1)

**Status: DONE 2026-08-02** — Phases 2–5 are complete and the index is live in notes. Phase 0 (the
synchronous-query gate) ran and is kept for its findings; Phase 1 shipped, was superseded, and is reverted.
Only **Phase 6** remains, deferred until embedded forces a real KV backend. Was `SQLITE-INDEX-PLAN.md`, and the reversal is §0. Written in the shape of [`EVENTS-PLAN.md`](./EVENTS-PLAN.md),
[`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md) and [`ENVIRONMENT-PLAN.md`](./ENVIRONMENT-PLAN.md): design
decisions first, phases that each leave the tree green, and nothing claimed until it has been broken on
purpose.

How an app stops reading every file to answer a question — **without the answer depending on which tier it
is running on**.

---

## 0. What changed, and why (2026-07-29)

The plan was SQLite: a host-provided database, imported per tier, absent on embedded, with every app
carrying an index path and a scan fallback that had to agree forever. It is now a **projection index the
engine owns**, stored behind a **`wasi:keyvalue`-shaped seam** that is file-backed today.

**The argument that turned it**, in the order it landed:

1. **`ORDER BY` was the whole case for SQL** (§6.2 rejects `wasi:keyvalue` in one sentence for it). But a
   KV store cannot order, so the sort would move into Go — and once the sort is in Go it is the **same
   sort** the fallback already uses. Under SQLite the two paths are SQL collation versus `sort.Slice`, two
   implementations of ordering that must agree forever; the old D7 existed to catch that and predicted its
   own first failure would be a collation mismatch. That entire bug class was self-inflicted.
2. **If Go does the querying, the "index" is a cache of projections** — and a cache of projections is a
   file. Which was §1.1's *rejected* alternative. The rejection argued that an engine-built index would be
   a third implementation of every query, written under TinyGo without reflection. That argument is sound
   only if the index does querying. It does not. **The argument was aimed at the wrong target.**
3. **The app-facing branch disappears.** This is the part that was not obvious from either option alone:
   with the index always present, `handleListRecords` has ONE implementation. There is no `HasIndex()`
   check, no fallback path in app code, and no "the fallback must return identical results" rule to
   enforce — because there is no second path to be identical to. The scan survives only inside
   `rebuild-index`.
4. **Storage is where `wasi:keyvalue` earns its place.** A whole-file projection index rewrites everything
   on every write, which is fine for bounded data on web and native and is exactly what §5.6 tells you not
   to do to flash. So the projection is written behind a narrow byte-KV seam — the standard's shape — and a
   host that has a real KV store binds it later without any app or query code changing.

**What survives from the SQLite plan:** the method-id block (200–299), `rebuild-index` at id 200, the
never-authoritative rule, the write ordering, the export exclusion, and both Phase 0 findings — the
synchronous-answer gate and the OPFS collision — which stop being urgent and start being **prerequisites
for the day a host-provided store lands** (D9).

**What it costs:** Phase 1's manifest field, which should be reverted (D8), and the sqlite-wasm dependency
that never got added. The Phase 0 spike is not wasted: it settled that a browser *can* answer a host
capability synchronously, which any host-provided store needs, and it found a collision any host-side
store in the OPFS root would hit.

---

## 1. Why now

- **`list-records` reads every file to show a list of titles.** At notes' size that is free; the shape is
  what is wrong, and it is the shape every app scaffolded from the template will copy.
- **Notes' `open` / `update` / `rebuild-index` are blocked on it** (`DEVALBO-DLC-GO-TASKS.md`).
- **§7.1 has promised a disposable index since the beginning** and nothing has ever built one, so the
  "disposable" half — rebuild, exclusion from bundles, never authoritative — is entirely untested prose.

### 1.1 Settled: the engine owns the projection and the query; only storage is a seam

| | Engine (portable, one implementation) | Seam (per tier, byte-shaped) |
| --- | --- | --- |
| what a projection contains | **app-defined proto message** | opaque bytes |
| ordering, filtering, paging | **Go, in the app's handler** | — |
| rebuild by scanning files | **platform + app rebuilder** | — |
| where projections are stored | — | **file today; host KV later** |

The load-bearing property: **a storage backend cannot change a result, only a duration.** Ordering and
filtering happen above the seam, so substituting the backend is a performance change by construction — not
a correctness one that a test has to chase. That is the difference from SQL, where the backend *was* the
query engine and every backend swap was a semantics risk.

---

## 2. Design decisions

### D1 — The engine owns the projection and the query; no query language crosses any boundary

The app defines its projection as an ordinary proto message in its own schema:

```proto
message RecordEntry {
  string id = 1;          // → records/<id>.json
  string title = 2;
  int64 created_at = 3;
}
```

…and queries it in Go: `sort.Slice`, a filter loop, a slice for paging. There is no SQL string, no query
DSL, and nothing for the platform to specify or evolve. The old D1 had SQL text crossing a byte boundary
because the app owned its schema and the platform could not know its columns; with the query in the app,
the platform does not need to know — it stores bytes.

**This is the decision that removes the most future work.** A query API is a language: it needs a grammar,
an evolution story, and a second implementation per backend. `sort.Slice` needs none of those.

### D2 — Storage is a narrow byte-KV seam, shaped like `wasi:keyvalue`

```go
// dlc-platform/index — the seam, not the API apps call
type Store interface {
    Put(key string, value []byte) error
    Delete(key string) error
    Scan() ([]Pair, error)   // unordered, like wasi:keyvalue's list-keys
    Clear() error            // rebuild starts from nothing
}
```

Four operations, all of which `wasi:keyvalue` already has (`set` / `delete` / `list-keys` + `get` /
—). §6.6's rule is **mirror the standard even when implementing it ourselves**, so adopting or bridging
to `wasi:keyvalue` later is wiring rather than redesign.

**Deliberate omissions, each with a reason rather than an oversight:** no `Get(key)` — a point lookup reads
the *record*, not the index (D6), and a list query wants everything anyway; no cursor on `Scan` — the
standard has one for unbounded stores, and §5.6 says assume bounded data, so a cursor now would be an
untested branch; no `exists`, no atomics, no batch — nothing needs them.

**Scan returns everything, and that is not a defect.** Every query materializes the collection to sort it.
The index's win is not avoiding *n* — it is avoiding *n* file opens, decodes of full records, and (on
embedded) *n* flash reads.

### D3 — The app has ONE code path; there is no fallback branch in app code

`handleListRecords` queries the index. Always, on every tier. No `HasIndex()`, no degraded mode, no
`unavailable` variant to handle.

This is a direct reversal of §6.2's "graceful degradation to a file scan", and it is a *simplification*
rather than a regression: the index is now always available, because its floor is a file on the filesystem
the app already has. Degradation was only ever necessary because the index was a host capability that
could be missing.

**The scan does not disappear — it moves.** It lives in `rebuild-index`, where it has always belonged: the
one operation whose job is to reconstruct the index from the source of truth.

**What this deletes:** the old D5 (fallback must return identical results) and most of the old D7 (index
parity), because there is no second query implementation to be identical to. What remains worth checking is
narrower and stated in D4.

### D4 — Rebuild is the correctness anchor, and the only check the index really needs

The invariant: **the maintained index equals the rebuilt index.** Mutate a collection through the app's
own verbs, then rebuild from the files, and the two must be byte-identical.

That one property catches the whole class this plan is exposed to — a create that forgets to index, a
delete that leaves a row, a projection that drifts from the record it projects — and it needs no second
tier, no second backend, and no golden file. It is also cheap enough to assert after every mutation test
rather than in one dedicated place.

The old plan needed a whole new verification script for this. This needs a helper.

### D5 — The index never travels

It is derived, so it must not be in an `export-fs` bundle (§7.1) and must not make two stores that differ
only in their index compare unequal.

One known path, excluded in one place, in Go — and testable natively *and* in the browser against the same
code. Worth contrasting with the SQLite version of this problem (old D9): there, the exclusion lived in the
web host's OPFS bridge, had to be repeated in any second host runtime, and was invisible to parity.

### D6 — The index is never authoritative

An index query returns **identifiers and ordering**. The record itself is read from its file.

The moment a handler renders a *value* out of the index, the index is a second source of truth and a stale
row gets served to a user. Where that genuinely bites — a list view showing titles for 10,000 records — the
projection is a **cache**, and a cache that disagrees with a file is a bug that D4's rebuild fixes. Start
with what a list view needs to render and widen deliberately, never with fields a stale value could
mislead someone about.

### D7 — Write order, and no lock file yet

**File first, index second, event last.** A subscriber that re-reads on the event must find both already
consistent; if the process dies in between, the truth is on disk and only the derived thing is behind —
which is what `rebuild-index` is for.

No lock file (§7.1 describes one), deliberately: commands are serialized within an instance, the lock
guards a second writer that does not exist yet, and a lock nothing can contend for is a branch nothing
tests. The failure it actually prevents is a torn *file*, which is a different problem with a different fix
(write-then-rename), worth doing when something can tear.

**Resolved (2026-08-02) by layout, not by locking — see below.**

**Revisit (2026-08-02): the second writer exists.** `Put` is a read-modify-write of the whole index file,
so two concurrent native processes — a script running `notes create` twice — can each read, add, and
rewrite, losing one entry. The records survive; the projection of one does not, so `list` under-reports
until `rebuild-index`. Three things make this bounded rather than urgent: only the derived thing is lost,
D4's invariant is what makes the repair trustworthy, and no tier but native can even have a second process.
The portable fix looked like a lock, and locking is where Windows hurts: `flock` is unix, `LockFileEx` is
Windows, and an `O_EXCL` lockfile wedges the app when a process dies holding it.

**So the layout changed instead: one file per key.** Different keys are different files and cannot conflict;
the same key is last-write-wins on one small file, which is exactly what the records themselves do. No lock,
no platform-specific code, and **D10 gets better rather than worse** — a `Put` now rewrites one small file
instead of the whole projection, so the flash-endurance pressure that was going to force a KV backend on
embedded is lower than when this plan was written.

The seam did not move. `Store` is unchanged, `Index` is unchanged, notes is unchanged — which is the first
real evidence that D2 drew the line in the right place, since this was exactly the kind of change the seam
existed to absorb.

**Keys are the filenames, validated rather than encoded.** Hex encoding was built first and reverted: it
made every Windows hazard impossible, and made `.dlc-index/records/` unreadable — which is the one thing
this project's storage model consistently refuses to trade. Instead `Put` refuses a key that cannot be a
filename anywhere (separators, Windows-illegal characters, control bytes, leading dot, trailing dot or
space, reserved device names, over 255 bytes) **and any key differing from an existing one only by case**,
on every platform including case-sensitive ones — because a rule that fires only on some machines lets an
app pass where it was written and fail where it runs. An app that genuinely needs opaque names implements
`Store`; encoding is a storage decision and the seam is where those live. What is left unfixed is a `Clear` racing another
process's `Put`, which can drop a concurrently-added entry mid-rebuild — rare, explicit, and repaired by
running rebuild again.

### D8 — The KV capability is DEFERRED, and the manifest field goes with it

Nothing in the repo has a host-provided KV store, so binding one now would be a capability with no
consumer — and `ENVIRONMENT-PLAN.md` D6 is explicit that a manifest field nothing sets is a branch nothing
tests.

**Therefore Phase 1 should be reverted:** `Environment.index`, `HasIndex()`, `BootOptions.Index`, and the
TS encoder field. Under this design the index is always present, so the field has nothing to say. When a
host does bring a KV store, the honest field is not "is there an index" but "does this host provide a
key-value store" — a different question, in a different message, added with its first consumer.

The falsifications Phase 1 produced stay recorded in §3; they cost an afternoon and the D4 registration
rule they exercised is still true.

**Where a host store would actually pay off, when it comes:** native at scale and embedded flash
endurance. Notably **not** web — the browser tier hydrates the entire OPFS into memory at boot, so a
file-backed index there is already memory-speed. That is the inverse of the SQLite story, where web was
the hard part.

### D9 — What the SQLite spike still binds, for the day a host store lands

Both Phase 0 findings survive as prerequisites rather than as current work:

- **A host-provided store must answer synchronously.** Measured green for sqlite-wasm under
  `opfs-sahpool` ([`spikes/sqlite-sync/`](../spikes/sqlite-sync/README.md)); the same requirement applies
  to any KV store bound as an import, and rules out IndexedDB on the web tier, which is async.
- **Anything storing files in the OPFS root collides with the bridge.** Hydrate pulls its files into the
  engine's tree (so they would ride along in bundles), and the flush then fails on every command with
  `NoModificationAllowedError`. A host-side store needs an exclusion on both sides of `opfs.ts`.

### D10 — Write amplification is the thing that pulls the KV backend forward

The file backend rewrites the whole projection file on every `Put`. At 10k entries × ~100 bytes that is
~1 MB per write — fine on web (an in-memory tree) and native, and precisely what §5.6 lists under *avoid*
for embedded flash.

So the ordering of future work is set by physics rather than preference: **embedded is the first tier that
needs a real KV backend**, and it is also the tier with the least demanding one to write (littlefs, a small
append-and-compact store). Naming this now is what keeps the seam honest — a seam whose second
implementation is hypothetical tends to grow file-shaped assumptions.

---

## 3. Phases

Each leaves the tree green. **No phase is done until something can be broken on purpose and observed going
red** (`AGENTS.md` §5).

### Phase 0 — the synchronous-query gate — **🟢 GREEN (2026-07-29), and now a prerequisite rather than a gate**

`spikes/sqlite-sync/` · `make spike-sqlite-sync` · [findings](../spikes/sqlite-sync/README.md).

Ran against sqlite-wasm because that was the plan at the time. What it established outlives the plan: a
browser **can** answer a host capability synchronously (no COOP/COEP required), and anything that stores
files in the OPFS root collides with the engine's bridge in two specific, loud ways. Both are D9.

*Keep or retire?* Keep until something binds a host-provided store, since it is the only evidence for D9.
Retire it then, per `spikes/README.md`'s rule about spikes that duplicate a live check.

### Phase 1 — the manifest field — **LANDED, SUPERSEDED, then REVERTED (2026-07-29)**

Shipped: `Index { availability }`, `Environment.index = 3`, `HasIndex()`, `BootOptions.Index`, the TS
encoder field, five tests, three falsifications (`UNSPECIFIED`-as-present, blank-instead-of-`ABSENT`, and a
D4-registration violation).

**Reverted** — D8. `Environment.index` is `reserved 3` (reserved rather than reused: the field a host store
would want is a *different* question, and giving an old number new semantics is how a stale decoder reads
the wrong fact), `HasIndex()` and `BootOptions.Index` are gone, and `environment.go` carries a comment
saying why there is no `HasIndex` rather than leaving the next reader to re-derive it. The index is now
always present, so the field had nothing to say. The one
finding worth carrying forward: `Boot` states availability in both directions so `UNSPECIFIED` can keep
meaning "no manifest has arrived", and the *TS encoder* had to move with it, because the two host runtimes
would otherwise have disagreed in bytes with no check in the repo able to see it — the parity vectors are
hand-built requests, not host-generated ones. That asymmetry will recur for every manifest field.

### Phase 2 — the seam and the file backend — **✅ DONE (2026-08-02)**

| File | Change |
| --- | --- |
| `dlc-platform/index/store.go` | the `Store` seam (D2) + the file-backed implementation |
| `dlc-platform/index/index.go` | `Put` / `Delete` / `Entries` / `Rebuild`, over a store |
| `dlc-platform/proto/devalbo/ilc/v1/platform.proto` | `RebuildIndex` claiming **id 200** (index block) |
| `dlc-platform/commands.go` | `handleRebuildIndex`; `SetIndexRebuilder`, mirroring `SetVersion` |
| `dlc-platform/fs.go` | **D5** — `IndexDir`, excluded from `ReadTree` |
| `dlc-platform/proto/method-ids.lock` | one new line, re-blessed deliberately |
| `dlc-platform/index_test.go`, `index/index_test.go` | round-trip, rebuild equivalence, exclusion, registration |

`SetIndexRebuilder` is the same shape as `SetVersion`: the platform owns the *verb*, the app supplies the
*knowledge*, because only the app knows its records live under `records/` and what is worth projecting.

*Falsified, all watched going red:* `Clear` as a no-op → `TestRebuildDropsStaleRows`; the exclusion disabled
→ `TestIndexIsExcludedFromABundle`; `write` that never reaches the filesystem → four store tests including
the reopen; `SetIndexRebuilder` not syncing → every registration and verb test; the index block shifted to
201–299 → `TestBlocksCoverEveryHandler` names method 200 as unregisterable.

**Registration turned out to be an APP fact, not a host fact.** D3 says there is no capability to branch on,
and that is true of the *host* — but an app with no collection (dlc, tictactoe) must not be handed a verb
that could only fail. So the condition is "did this app supply a rebuilder", `SetIndexRebuilder` performs
the registration, and it works in either order relative to `RegisterAll` because init ordering is invisible
and getting it wrong costs a silently missing command.

**Five findings, three of them about the repo rather than the index:**

1. **`dlc-platform`'s own tests had never run in CI.** It is a separate module, so `go test ./...` from the
   root never reached it — six test files covering dispatch, path containment, BFT and the manifest, none of
   them executed. Now `go test -C dlc-platform ./...` in both `ci.sh` and `test-b2.sh` (T-B2.0b). Same class
   as the `hosts/` gap Phase 1 of the host-layer plan found: vetted but not tested.
2. **One inherited verb costs a renderer in every host — seven edit sites.** dlc, notes and tictactoe ×
   {native, web}, plus the template. That is the missing-renderer rule working as designed (a declared
   command with no renderer is a startup error), but it means the cost of an inherited verb is *linear in
   apps*, and it will only grow. Worth a decision before the next capability lands: a default renderer for
   inherited verbs would make this one edit instead of seven.
3. **`unavailable`'s wording assumes the cause is the host.** It says "this host does not provide the
   capability X needs" — true for `export-fs` without a filesystem, wrong for `rebuild-index`, where the
   app simply keeps no index. The mechanism is right (unregistered → marked); the sentence needs a second
   case. Recorded for Phase 5 rather than fixed here, because it touches tested CLI behaviour.
4. **One line was written, then deleted for being unfalsifiable.** `RegisterAll` called `syncIndexVerbs`;
   removing it broke nothing, because `SetIndexRebuilder` already syncs in either order. A branch no test
   can reach is what D6 of `ENVIRONMENT-PLAN.md` argues against, so it went rather than acquiring a
   contrived test.
5. **A CLI test asserted more than its name.** `TestAvailableCommandIsNotMarked` checked that the *whole
   help text* contained no "unavailable" marker, so the first honestly-unavailable command broke it. Now
   scoped to `export-fs`'s own line — the thing it was always about.

**Deliberately not done here:** no parity vector for `rebuild-index`. dlc registers no index verb, so its
command surface is unchanged and there is nothing new for the surface vectors to compare — the vectors
arrive with notes in Phase 4, driven by an app that actually has one.

### Phase 3 — notes uses it, and its scan moves into rebuild — **✅ DONE (2026-08-02)**

| File | Change |
| --- | --- |
| `example-apps/notes/proto/notes/v1/commands.proto` | `RecordEntry` projection; `ListRecordsResponse` returns entries, not records |
| `example-apps/notes/engine/commands.go` | create/delete maintain the index; **`handleListRecords` queries it, with no branch**; the scan becomes `rebuildIndex`, wired through `SetIndexRebuilder` |
| `example-apps/notes/engine/commands_test.go` | D4 after every mutation; rebuild repairs a deleted index; `open` reads the file, not the index |
| both slots + `slot-driver.ts` | render the projection |
| `hosts/web/test/files.spec.ts` | the index is on disk **and** absent from a bundle — D5 on the tier where the bridge is |

**`RebuildIndex` did NOT claim notes' reserved 10005**, which the plan assumed it would. The verb is
inherited from the platform at id 200 and the app supplies only the scan, so the app-side id was never
needed — the reservation was deleted rather than kept and explained. That is the design working: an
inherited verb costs an app no id at all.

*Falsified, both watched going red:* deleting the index write from `handleCreateRecord` → `index drifted:
maintained [], rebuilt [apple zebra]`; deleting it from `handleDeleteRecord` → `maintained [apple zebra],
rebuilt [apple]`. In the app's own tests, with no new harness, exactly as D4 predicted.

**Three things Phase 3 decided that the plan left open:**

1. **`ListRecordsResponse` returns the PROJECTION, not records.** D6 says a list must not render values out
   of the index, and the structural way to guarantee that is to make the response incapable of carrying
   one: there is no body field to serve stale. `open` still reads the record's own file, and a body longer
   than the projection's cap is what proves the two are different reads.
2. **The preview is capped in the ENGINE (200 bytes), truncated in the SLOT.** Bounding what is *stored* is
   a storage decision — an index holding whole bodies would be the whole store, which is why full-text
   search is ruled out rather than nearly-built. Where to cut a line for a 24-column table is presentation
   and stays in the slot (Decision 34).
3. **The index is VISIBLE in the web file browser, and that stayed.** One note is now two files, and the
   obvious tidy-up — hide it — would make the one view whose whole job is "the files are the truth" tell a
   neater story than the disk does. The browser test now asserts both halves: on disk, absent from a
   bundle.

### Phase 4 — the checks that are worth a script — **✅ DONE (2026-08-02), and smaller than planned**

| File | Change |
| --- | --- |
| ~~`verify/parity/method-vectors.json`~~ | **not done, and should not be** — see below |
| `example-apps/notes/hosts/web/test/files.spec.ts` | ✅ (landed in Phase 3) the index is on disk **and** absent from an exported bundle, in Chromium against the real OPFS bridge |
| `engine/execute_test.go` | ✅ `TestDlcOffersNoIndexVerb` — dlc offers no index verb, and it is undispatchable rather than merely unlisted |

**No `verify-index-parity.sh`** — the old plan needed one because SQL and Go had to agree; D4's rebuild
equivalence replaces it and lives in unit tests.

**And no index vectors either, which was not the plan.** Two facts collide:

1. The vectors run against **dlc's** engine, and dlc keeps no collection — so there is nothing there to
   create, list, or rebuild. Index vectors would need a second engine in the harness, which is a structural
   change to `cmd/parity-runner` for a capability whose engine-side code is already covered.
2. **The vectors carry requests only.** Parity compares native against wasm with no expected output, so two
   tiers agreeing on a *wrong* surface is green. That is fine for its actual job — catching divergence — but
   it means a vector can never pin "this verb should be absent".

So the honest check for the decision made in Phase 2 (*registration is an app fact*) is a test where an
expectation can live, and `TestDlcOffersNoIndexVerb` is it. Falsified by giving dlc a rebuilder:
`dlc registered rebuild-index (200) — it has no collection to project`.

**What parity still covers for free:** the index is engine-side Go over ordinary files, so the moment an app
in the harness writes one, the existing filesystem diff compares its bytes across tiers. That is why the
format is sorted and deterministic — the check is already waiting for it.

### Phase 5 — write it down, and the dogfood pass — **✅ DONE (2026-08-02)**

- `AGENTS.md` **§3·7** — the three rules that are easy to break quietly: never authoritative (D6), write
  file → index → event (D7), never travels (D5), plus the rebuild invariant as a *standing instruction*
  ("if you add a mutating command, add that assertion to its test") and the "do not add a manifest field"
  warning with the reason it was reverted.
- `DEVALBO-ILC-GO-PLAN.md` — **§6.2 is now the derived index** rather than a SQLite capability; **§7.1**
  carries the write order, the never-authoritative rule, the exclusion and the rebuild invariant, and drops
  the lock file with the reason; **§6.6's** index row says outright that "needs `ORDER BY`" was the sentence
  this overturned. Decisions **7**, **9**, **12** and **20** updated; `sqlite-host` is gone from the WIT
  sketch, the world's imports, the component diagram, the environment matrix, the toolchain table, the
  `dlc.toml` example and the pitfalls list.
- **Dogfood review:** `dlc` does not adopt the index — it has no collection to list — and that is recorded
  as a legitimate answer *and pinned by a test* (`TestDlcOffersNoIndexVerb`, Phase 4). notes is the
  consumer; tictactoe has no persistence at all.

**One consequence worth its own line: Decision 33's strict/lenient knob is now UNOWNED.** It was explicitly
parked on "a capability that can genuinely be missing at runtime (the SQLite index)" — and the index turned
out never to be absent, so the deferral lost its home. D33 now says so, and hands the question to the next
capability that can actually go missing, which is the only way it gets answered with a real consumer rather
than in the abstract.

### Phase 6 (deferred, not scheduled) — a host-provided KV store

Only when a tier needs it, which D10 says will be embedded first. Binds `wasi:keyvalue`-shaped imports
behind the same `Store` seam, and inherits D9's two constraints. **The app and its queries do not change**
— that is the test of whether this design was right.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| **The index drifts from the files** | a create forgets to index; the list is quietly wrong forever | D4 rebuild equivalence, asserted after every mutation test |
| **The index becomes authoritative** | one handler renders a value from it and a stale row reaches a user | D6; ids and ordering only, widen deliberately |
| **Write amplification** | a whole-file rewrite per `Put`; on flash it is an endurance problem, not a speed one | D10 names it; bounded data until the KV backend lands |
| **The index travels in a bundle** | two identical stores compare unequal; a disposable thing becomes state | D5, one path, one exclusion, tested on both tiers |
| **The seam grows file-shaped assumptions** | its only implementation is a file, and the second one is hypothetical | D2 mirrors `wasi:keyvalue` deliberately; D10 says which tier forces it |
| **`Scan` returning everything stops being viable** | ~10k entries is fine, ~100k is 10 MB materialized per query | the same wall split storage hits from the other side; a cursor is a known, deferred addition |
| **Apps branching on the tier** | the old plan's biggest hazard | mostly **designed out** — there is no capability to branch on (D3) |

---

## 5. What this plan does NOT do

- **No SQLite.** Not natively, not in the browser. Decision 12's `sqlite-host` is dropped; §6.6's
  `ORDER BY` justification is overturned (D1).
- **No query language on any boundary** — no SQL text, no DSL, nothing to specify or evolve.
- **No host capability, no new WIT import, no manifest field** (D8), until a tier needs one (Phase 6).
- **No full-text search.** The projection would have to hold every body, which is the whole store.
- **No lock file, no write-then-rename** (D7) — until a second writer exists.
- **No sync.** §9 operates on the JSON documents and each node rebuilds locally; unchanged.

---

## 6. Definition of done

1. [x] `./scripts/ci.sh full` green.
2. [x] The synchronous-answer question is settled in writing — Phase 0, and it now binds Phase 6.
3. [x] `list-records` answers from the index on every tier, with **no branch in app code**.
4. [x] The maintained index equals a rebuilt one, asserted after every mutation, **watched going red** —
   falsified from both sides, a create that does not index and a delete that leaves a row.
5. [x] An exported bundle contains no index file, asserted natively and in the browser —
   `TestIndexIsExcludedFromABundle` plus the two cases that path suggested (asking for the index directory
   *itself*, and a user's own directory that happens to share the name), and `files.spec.ts` in Chromium.
6. [x] `rebuild-index` reconstructs an index deleted out from under a running app — `Clear` is
   unconditional and `Scan` of a missing store is empty rather than an error, so a deleted index rebuilds
   from the files with no special case.
7. [x] No handler renders a value out of the index (D6) — structural rather than by inspection:
   `ListRecordsResponse` carries a projection with no body to serve stale, and `open` reads the file.
8. [x] The `Store` seam has been read as if implementing it on littlefs, and nothing in it assumes a file —
   checked by `memStore` in `index/index_test.go`, which has no filesystem anywhere in it and deliberately
   scans in reverse insertion order to prove ordering lives above the seam.
9. [x] `AGENTS.md` carries D5, D6 and D7 (§3·7); the plan's §6.2/§6.6/§7.1 record that SQL was
   reconsidered and why, and Decisions 7/9/12/20/33 are updated.
