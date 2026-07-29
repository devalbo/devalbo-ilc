# The environment manifest — implementation plan (§6.4a, Decision 32)

**Status: planned, pre-Phase-1 questions settled (2026-07-28).** Written in the shape of
[`EVENTS-PLAN.md`](./EVENTS-PLAN.md) and [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md): design decisions
first, phases that each leave the tree green, and nothing claimed until it has been broken on purpose.

How an app learns what the host it is running on can actually do — **without a second boundary**.

---

## 1. Why now

Decision 32 is designed and unbuilt. Its original headline justification has since evaporated, and a better
one has taken its place.

**The reason it was designed:** so a handler could branch on display facts — resolution, colour format,
which render paths exist — and target a 320×240 TFT and a browser canvas from one piece of logic.

**That reason is gone.** Decision 34 made Display optional and added the semantic render path: an app emits
what is *true* and each host decides what it looks like. An app on that path **never learns there is a
screen**, so the manifest's headline consumer disappeared.

**The reason it is next anyway** — better, because it came from hitting the wall rather than from design:

- **An app cannot ask whether it has a filesystem.** `engine/platform` exposes no availability API, so an
  app calls `WriteTree` and either it works or it returns an error it had no way to anticipate. §6.5
  *promises* graceful degradation when a capability is absent; that promise is currently false.
- **`dlc.toml`'s `capabilities` list has one writer and zero readers** — still decorative, as
  `EVENTS-PLAN.md` Phase 5 found. It describes build-time composition; the manifest describes runtime facts.
- **The SQLite index is next** and §7.1 requires `unavailable` to be survivable. There is no way to express
  "no index" today.

### 1.1 Settled: standalone, with a real consumer

The open question was whether to build this standalone or as phase 0 of the index, and the objection was
that **nothing that exists can currently be absent** — a manifest whose "capability missing" branch nothing
can take is decoration, the same way a parity check nobody has watched fail is.

**That objection does not survive contact with the web tier: OPFS can fail today.** Storage denied,
private-browsing modes, older Safari. The web host has no answer for it right now, so "no filesystem" is a
shipping gap on a tier we already ship — not a hypothetical, and not something a test double has to
manufacture. The absent branch has a genuine consumer on day one.

**Standalone, then**, and the index (which is close behind) supplies the second consumer soon enough to
catch a wrong schema shape before two things depend on it. The synthetic withheld capability the earlier
draft proposed is unnecessary and has been dropped.

---

## 2. Design decisions

### D1 — It is DATA on the existing boundary, not a new import

`SetEnvironment` is a platform command in the core-lifecycle block (id 2, already reserved in
`method-ids.lock`). The host calls it like any other command.

**Why not a `describe()` import per capability:** Decision 31 consolidated to one engine entry point, and a
per-capability import reopens a second — one that every WAMR-portable tier would have to mirror. A manifest
is bytes on a boundary already proven to cross every tier, and `buf breaking` versions it like everything
else.

### D2 — An unset manifest means "assume nothing"

If no host ever calls `SetEnvironment`, every capability reads as unavailable.

**This is not the silent default this repo keeps deleting**, and the difference is worth stating because it
looks identical from a distance. A defaulted *tier list* is a guess about intent — the caller might have
wanted either answer, and choosing scaffolds a layout nobody picked. Assuming a capability is *absent* is
the conservative direction: the app does what works everywhere, and being wrong costs performance rather
than data. A default is dangerous when the safe option and the convenient one differ; here they are the
same.

**D7 reaches the same place by a second route** — no manifest means no capability verbs registered — and
the two agreeing is a useful redundancy rather than a duplication.

### D3 — An app may branch on what it must DO, never on who is listening

Decision 33 says app code must not be able to tell whether anyone is listening to events. The manifest
exists precisely so an app CAN tell things about its host. Not a conflict; the line between them is the
useful part:

- **Events carry nothing back.** Absence is unobservable, so branching on it changes nothing observable —
  which is how one tier quietly behaves differently from another.
- **A filesystem either accepts a write or does not.** Absence changes what the app must *do*, and
  pretending otherwise means a crash the app had no way to anticipate.

**The test: does the capability return something the app consumes?** If yes it belongs in the manifest; if
no, its absence must be a no-op. Events fail that test, which is why Decision 33 forbids declaring them and
why they must not appear here either.

### D4 — Sent before any other command; volatile facts are re-pushed, never polled

The host sends at launch, as phase 1 of the two-phase launch (§5.5), and **re-sends whenever a fact
changes**. D7 upgrades "should" to **must**: a command dispatched before the manifest arrives sees a
partial command surface, so ordering is now a correctness requirement rather than a convention.

**The one failure mode, named because it will happen:** a host that forgets to re-send leaves the engine
reasoning from stale facts, and nothing detects it. Carry a revision number so staleness is at least
*visible*, and emit `ilc.environment-changed` so a slot re-reads instead of polling.

**Why push rather than a `describe()` the engine calls.** A query is an *import*, and imports are the
expensive direction: WIT has no optional ones, so every tier — including WAMR-portable embedded targets
that do not exist yet — must supply it or the component will not link. Worse, a query returns a value and
is therefore synchronous, while probing OPFS is inherently async and jco only supports async imports under
`--async-imports` (refused, Decision 22). A synchronous `describe()` in the worker could only answer from a
pre-computed cache — **the pull collapses into a push plus a cache, having paid for an import to get
there**. And a push is bytes on a boundary: recordable, diffable, pinnable in a parity vector, whereas a
query makes engine behaviour depend on host code running mid-command, which is exactly the blindness D3
exists to prevent.

**What pull would genuinely win is staleness**, which is structurally impossible when every read is fresh.
The escape hatch that recovers most of it without a new import: **the engine emits `ilc.environment-stale`
and the host answers with `SetEnvironment`.** A pull-shaped flow over two boundaries that already exist and
are already parity-visible. This is the "`describe()`-style live query for anything too volatile to push"
that Decision 32 held open, minus the WIT surface.

**Adopted provisionally.** The risk is that it becomes a habit — an engine that pokes the host whenever it
is unsure, turning a push model into a chatty pull with extra steps. If it starts appearing anywhere other
than genuine staleness recovery, remove it; nothing else in this plan depends on it.

### D5 — The manifest is runtime facts; `dlc.toml` is build-time composition

- `dlc.toml` `capabilities` — what gets LINKED for a tier. A build-time choice, read by the toolchain.
- the manifest — what the host running right now can actually DO. A launch-time fact, read by the engine.

An app can be built with the index linked and still find none at runtime. That is exactly what §7.1 calls
`unavailable`, and it is unrepresentable if the two are conflated.

### D6 — Start with the facts that have a consumer

- **filesystem**: present, and its KIND — the root grant made this concrete (cwd, `./.<app>/`, OPFS,
  littlefs). Whether it is ephemeral matters to an app deciding what to cache.
- **index**: present or not. The §7.1 fallback switch. Lands with the index.
- **display**: absent for now. Decision 34 made it optional, and a field for an unbuilt capability is how a
  schema grows fields nobody sets.
- **events**: NOT PRESENT, by D3.
- **revision**: `uint32`, **required and non-zero** — see D11.

### D11 — A required, non-zero revision

Every manifest carries a revision the host increments on each send. Zero is invalid and rejected, so
"forgot to set it" cannot masquerade as "revision 0" — the same no-silent-defaults rule that made `tiers`
required.

**It is not a field with no reader, which D6 would otherwise forbid.** D7 makes `SetEnvironment` trigger
capability registration, so a re-send is not free — it re-runs registration and churns the command surface.
The revision is what lets the engine treat an unchanged manifest as a no-op and re-register only when the
facts actually moved. Two secondary readers follow: a slot can tell whether what it rendered from is
current, and the inspector can show which revision is in force.

**Include it now rather than later.** It is the one field whose absence is expensive: adding it once hosts
exist means every host starts sending a field they previously did not, with no way to tell an old host from
a buggy one.

### D7 — Two-phase registry: core verbs at init, capability verbs when the environment lands

An app registers what its host can actually support, and it cannot know that at `init()` — the manifest
arrives *as a command*, and `SetEnvironment` must already be registered to be dispatchable. So the registry
splits:

- **at init:** the core-lifecycle verbs — `SetEnvironment`, `version`. This is what the core block is *for*,
  and why id 2 lives there.
- **on `SetEnvironment`:** capability-dependent verbs, through a platform hook the app opts into.
  `RegisterAll()` stays the common case; a filesystem-less host gets a surface without `export-fs` /
  `import-fs` / `reset-fs`.

**This makes the registry mutable after init, which it currently is not** — the main structural cost of this
plan, and the thing most likely to bite. Two consequences follow immediately:

- **The ordering in D4 becomes mandatory.** A command between init and `SetEnvironment` sees only core
  verbs.
- **Parity must compare the command SURFACE, not only results.** Given the same manifest, both tiers must
  register identically. Registration that varies per tier is invisible to every check we have today, and it
  is precisely the divergence this architecture exists to prevent.

### D8 — `Root()` keeps panicking, and that is now cheap

§3·5 makes `Root()` panic without a grant. That stays exactly as it is.

**Because of D7 it costs nothing.** An app on a filesystem-less host never registers the verbs that touch
the filesystem, so nothing reaches `Root()` in the first place. The panic keeps its original meaning — *a
host that FORGOT to grant a root* — instead of becoming an expected runtime state that every caller has to
defend against. An app that consults the manifest, ignores it, and writes anyway has a bug, and a panic is
how it finds out.

### D9 — Absent verbs stay VISIBLE, marked unsupported

`clispec` is generated statically from the `.proto` and lists every command regardless of what is
registered. Rather than filtering, a command absent from the live registry still appears, marked
unavailable on this host.

**Correction found while building Phase 1:** the earlier draft said to reuse `Command.Unsupported`. That
field is something else — it names request FIELDS the CLI cannot express (nested messages, maps), which is
a property of the schema and identical on every tier. Host availability is a property of the runtime and
varies per tier, so it needs its own carrier, computed at query time rather than generated. Reusing
`Unsupported` would have conflated "you cannot type this argument" with "this host cannot do this",
producing one message for two unrelated conditions.

**Why not filter:** a user who read the docs would find a command silently missing with no explanation, and
the generated spec and the runtime surface would disagree with nothing reconciling them. Marking is that
reconciliation, in one place, and it is computed at query time because D7 means the answer changes when the
manifest lands.

### D10 — The platform reports; the app decides

The platform's job ends at making the fact available. Refusing to start, degrading, or ignoring it is app
code — which is why there is no inherited "graceful degradation" mechanism in this plan, only an inherited
way to ask.

---

## 2.5 The startup sequence

D7 turns ordering from a convention into a correctness requirement, so it is written down rather than
inferred from whichever host was read last.

**Native**

```
1. guest init()   platform.RegisterCore(); app.RegisterCore()   ← version + SetEnvironment only
2. host main()    platform.SetVersion(dlcconfig.Version)
3. host           platform.SetRoot(platform.AppRoot(name))      ← the §3·5 grant
4. host           subscribe(sink)                               ← before anything can emit
5. host           Execute(2, Environment{…})                    ← D4 / D7
     └─ engine    Env() answers; capability verbs register
6. host           build the CLI surface from clispec + the live registry   ← D9
7. host           parse argv → Execute(method, request)
```

**Web**

```
1. worker   probe navigator.storage.getDirectory()        ← may FAIL
2. worker   install the OPFS preopen (skipped if it did)
3. worker   instantiate the component → guest init() registers core only
4. worker   hydrate OPFS → preopen
5. worker   SetRoot (a no-op on wasm — the grant happened at step 2)
6. worker   subscribe(sink); onFlush(…)
7. worker   execute(2, Environment{filesystem: OPFS | absent})
8. worker   execute(method, request) … flush
```

**The constraints that actually bind:**

- **Root before manifest.** The manifest *describes* the root, so the host must have chosen it first. On
  wasm this is forced anyway: a preopen has to exist at instantiation.
- **Subscribe before the manifest**, because `SetEnvironment` may itself emit `ilc.environment-changed`. A
  host that subscribes afterwards misses the first one.
- **Manifest before the CLI surface is built.** New, and easy to miss: `cli.Run` today builds the spec and
  parses in one step, but D9's `Unsupported` marks come from the live registry, so native step 6 cannot
  move above step 5.
- **The manifest goes through `Execute` on BOTH tiers**, even natively where the engine is linked
  in-process (Decision 26) and a direct `platform.SetEnvironment(…)` would work — `SetRoot` sets a
  precedent for a direct call that this must not follow. If native skips the command path, the native tier
  never dispatches id 2 and parity is comparing two different sequences.

**The OPFS-absent path falls out of this with no special case**, which is the main evidence the shape is
right: no preopen → `SetRoot` no-ops → the manifest says absent → filesystem verbs never register →
nothing reaches `Root()`, so D8's panic never fires.

### 2.5a `platform.Boot(…)` owns the sequence — DECIDED (2026-07-28)

**Steps 1–5 are boilerplate every scaffolded app repeats, and getting the order wrong fails in ways that do
not look like ordering bugs** — a missed first event, a CLI surface built before the registry settled, a
manifest sent before the root grant. That is the profile of something that should be inherited rather than
copied: §3 says templates depend on the platform and never inline it, and five ordered lines in a template
are inlined logic wearing a different hat. A misordering fixed in `Boot` reaches every app; the same fix in
a template reaches only apps scaffolded afterwards.

An **options struct**, not a long signature — version, root, sink, manifest — so adding a step later is not
a breaking change to every host. A host that must deviate calls the pieces directly; `Boot` is the
inherited default, not the only door.

Lands in Phase 2, before the sequence would otherwise be written into `templates/`.

---

## 3. Phases

Each leaves the tree green. **No phase is done until something can be broken on purpose and observed going
red** (`AGENTS.md` §5).

### Phase 1 — the message, the command, the accessor — **LANDED (2026-07-28)**

| File | Change |
| --- | --- |
| `proto/devalbo/ilc/v1/platform.proto` | `Availability`, `FilesystemKind`, `Filesystem`, `Environment` (with `revision`); `SetEnvironment` claiming reserved id 2 |
| `proto/devalbo/options/v1/options.proto` | **`cli_hidden`** (50011) — see below |
| `cmd/protoc-gen-dlc-registry/{main,cli}.go` | honour `cli_hidden`: dispatchable, but not in the surface |
| `engine/platform/environment.go` | `Env()` (never nil), `HasFilesystem()`, `applyEnvironment` |
| `engine/platform/registry.go` | `registerBlock` / `unregisterBlock`; `MethodSetEnvironment` |
| `engine/platform/commands.go` | `handleSetEnvironment`; `RegisterCore` / `RegisterDiscovered` alongside `RegisterAll` |
| `engine/platform/environment_test.go` | 8 tests |
| `engine/platform/cli/cli_test.go` | the hidden command is absent, the visible ones are not |

**`RegisterAll` was not split — a third variant was added instead**, which is closer to what the design
actually calls for. Splitting it would have changed what every existing app and host does, and eagerly
registering is *correct* for an app whose hosts always have a filesystem (today, every native app). So:
`RegisterAll` (eager, unchanged), `RegisterCore` (lifecycle only), `RegisterDiscovered` (core now,
capability verbs when the manifest lands). An app picks a policy; the platform does not pick for it (D10).

**`cli_hidden` was not planned and turned out to be required.** Adding the rpc put `set-environment` into
the generated command-line surface, and `verify-bundle-xtier` failed with `command "set-environment"
(method 2) has no renderer registered`. That is the generated-surface design working as intended (§3b: the
CLI comes from the schema, so a new rpc *is* a new subcommand) meeting a case it had not had before — a
verb a HOST sends and a person never types. Generating a subcommand for one would ask a user to
hand-write a capability manifest on a command line and oblige every host to render a response with nothing
in it. Cosmetic like `cli_name`: dispatch is on `method_id`, so a hidden verb stays perfectly dispatchable.

*Falsified, each watched going red:* accept revision 0 → the rejection test fails; ignore the revision on
re-apply → the no-op test fails; return nil from `Env()` when unset → the absent-reads test fails;
`RegisterCore` registering the filesystem block → the core-only test fails; `registerBlock` not skipping
existing ids → re-sync panics; `syncCapabilityVerbs` never unregistering → the capability cannot go away;
drop `cli_hidden` from the rpc → `set-environment` reappears in the surface. The id lock also fired
unprompted when the reservation became an rpc, and was re-blessed deliberately.

### Phase 2 — the hosts send it, and the web host detects OPFS failure — **LANDED (2026-07-28)**

| File | Change |
| --- | --- |
| `engine/platform/boot.go` + `boot_test.go` | `Boot(BootOptions{…})` owning §2.5 steps 1–5; 5 tests |
| `engine/commands.go` | `dlc` switches to `RegisterDiscovered` |
| `hosts/native/main.go` | `Boot`, root kind `CWD` |
| `hosts/web/environment.ts` | the manifest encoder and the OPFS probe |
| `hosts/web/worker.ts` | probe → hydrate → instantiate → **manifest** → commands |
| `example-apps/{notes,tictactoe}/hosts/native`, `templates/…` | `Boot`, root kind `APP_DIR` |
| `cmd/{parity-runner,scaffold-golden}`, `engine/execute_test.go` | every in-process caller is a host |
| `verify/parity/method-vectors.json` | 21 → 27 vectors |

**`Version` was dropped from `BootOptions`.** Every app calls `SetVersion` from its ENGINE's init, which
runs on every tier; a host-supplied version would exist natively and be missing in a browser tab, where no
Go host runs at all.

**Making the manifest mandatory broke three callers, exactly as predicted** — `engine/execute_test.go`,
`cmd/scaffold-golden`, and the parity runner all granted a root without sending a manifest and got
`unknown method_id 100`. The lesson worth keeping: *every in-process caller is a host*, and `Boot` is what
they all now share.

#### What the falsifications actually showed

**The manifest had to become parity vector #1, and proving why was the most valuable thing in this phase.**
Removing the `set-environment` vectors leaves parity **green** — 21 vectors, "native == component", "70
files written, trees identical" — while every filesystem verb on both tiers answers `unknown method_id
100`. Two equally broken engines agreeing perfectly. Sending the manifest as vector #1 puts the ordering
inside what is compared instead of underneath it.

**And parity cannot check the absent branch either.** Breaking `unregisterBlock` so a capability never goes
away leaves parity green as well: both tiers break identically. The absent-branch vectors (revision 2
removes the filesystem, `export-fs` is then unregistered, revision 3 restores it) are still worth having —
they prove the real wasm component behaves like native — but **they pin agreement, not correctness**. What
pins correctness is `TestDiscoveredRegistrationFollowsTheManifest`, which does go red under that patch.
This is D7's blind spot showing up twice in one phase, in both directions.

#### Not done: the OPFS probe is untested end-to-end

The plan said to stub `navigator.storage.getDirectory` from Playwright. **That cannot work here.** The probe
runs in the WORKER, and page-level init scripts do not touch a worker's global scope — the stub was
installed, the worker never saw it, and `export-fs` happily returned an empty tree. The test was removed
rather than left passing for the wrong reason.

So the *engine* half of absence is proven (Go tests + parity vectors, on the real component) and the
*probe→manifest* link is not. Two ways to close it, neither done:

- **move the probe to the main thread** and pass the fact into the worker — stubbable from Playwright, no
  production test seam, at the cost that the thread doing the probing is not the one using the filesystem;
- **a worker-visible switch** (`?ilc-no-fs=1`) — the production test seam previously declined.

### Phase 3 — the surface reflects the environment

| File | Change |
| --- | --- |
| `engine/platform/clispec` | `Unsupported` driven by the live registry, computed at query time (D9) |
| `frontend/src/commands.ts`, `hosts/web/inspector.ts` | render unsupported commands as present-but-unavailable |
| parity | compare the registered command SURFACE, not only results (D7) |

*Falsification:* boot with OPFS denied and confirm `export-fs` appears marked unsupported rather than
vanishing or failing with "unknown method"; then register different verb sets on two tiers and confirm the
new surface comparison goes red.

### Phase 4 — volatile facts and the re-send contract

| File | Change |
| --- | --- |
| `hosts/web/*` | re-send on change; `ilc.environment-changed` so a slot re-reads |
| `AGENTS.md` | the re-send contract, the mandatory ordering (D4/D7), and the D3 line |

*Falsification:* change a fact without re-sending and confirm the engine is demonstrably stale — the failure
Decision 32 predicts, reproduced once so its shape is known.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| **A mutable registry** | the single largest structural change here; a command arriving before the manifest sees a partial surface | D7: core verbs always at init; D4 ordering becomes mandatory; parity compares the surface |
| **Registration diverging per tier** | invisible to every check we have — both tiers pass while offering different commands | Phase 3 surface comparison, falsified deliberately |
| **Stale facts** | a host forgets to re-send; the engine is confidently wrong and nothing notices | revision number; contract in `AGENTS.md`; falsify in Phase 4 |
| **Growing fields nobody sets** | display/network added "while we are here", each an untested branch | D6 — only facts with a consumer |
| **Apps branching per tier** | the manifest makes tier-detection *possible*, which is the divergence this repo exists to prevent | D3 — branch on what you must DO, never on who is listening |
| **A stubbed OPFS is not a denied OPFS** | the test exercises the host's reaction to a faked API | accepted (Phase 2); revisit if a real denial ever behaves differently |
| **Startup order copied per app** | five ordered steps whose misordering fails far from the cause | §2.5 written down; `platform.Boot` owns it (§2.5a), template calls it rather than copying |
| **`environment-stale` becoming a habit** | an engine that pokes the host whenever unsure is a chatty pull with extra steps | provisional (D4); remove if it appears outside staleness recovery — nothing depends on it |

---

## 5. What this plan does NOT do

- **Not Display.** Decision 34 made it optional; a field for an unbuilt capability is speculative.
- **Not the SQLite index.** This is its prerequisite, not its implementation.
- **No `describe()` import.** Decision 31 stands: one boundary.
- **No capability NEGOTIATION.** The host states facts; the engine does not request or refuse them.
- **No inherited degradation policy.** D10 — the platform reports, the app decides.

---

## 6. Definition of done

1. `./scripts/ci.sh full` green.
2. An app can ask whether a capability is available, and an unset manifest reads as absent.
3. Both hosts send it; parity shows the same manifest AND the same command surface on both tiers.
4. **The web host has been run with OPFS denied**, reaching a filesystem-less surface through real host
   code — the absent branch watched running, not reasoned about (§1.1).
5. An unsupported command is visible and marked, not missing and not "unknown method".
6. A stale manifest has been reproduced once, so the failure Decision 32 predicts has a known shape.
7. Hosts and the template call `platform.Boot` rather than repeating the §2.5 sequence.
8. `AGENTS.md` carries the re-send contract, the mandatory ordering, and the D3 line.
