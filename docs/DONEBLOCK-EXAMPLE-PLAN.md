# DoneBlock — an example app plan (App #4)

**Status: PLAN (2026-07-29). Nothing is built.**

**Audience: a coding agent building this app**, plus whoever reviews what it produces. It is written to be
executable without further design work: the domain is specified, the schema decided, the build order fixed,
and the rules that are easy to violate are stated with their reasons. Where something is genuinely open,
§9 says so and says to **ask rather than guess**.

A CLI version of [doneblock.com](https://www.doneblock.com/): **tasks are nodes in a DAG, and an edge means
one task blocks another.** The question the tool exists to answer is *what can I actually work on right
now* — everything else is bookkeeping in service of that.

**This app also exists to produce feedback on the framework** (§10). Friction is a deliverable here, not an
obstacle: if something is awkward, record it rather than routing around it silently.

---

## 1. Read these first

| Document | Why |
| --- | --- |
| [`AGENTS.md`](../AGENTS.md) | the rules. §1 method ids · §2 the engine · §3 the platform boundary · §3a the host layer · §3b the generated command surface · §5 verification · §6 working style |
| [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) §7.1, §8 | split storage; how a command reaches a handler |
| [`example-apps/notes/`](../example-apps/notes/) | the closest existing app — copy its layout, not its domain |
| [`example-apps/tictactoe/`](../example-apps/tictactoe/) | the app that renders from **events** across two host slots |
| [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md) | what host code may and may not do — §5 below is the short version |

**Two rules that this app will tempt you to break**, so they are here rather than buried:

1. **The engine decides; a host renders** (Decision 34). Readiness, cycle rejection and ordering are engine
   answers. A browser host with a graph library will be *very* tempted to compute a topological sort
   itself. It must not: two hosts that each derive readiness will eventually disagree on one tier only,
   with every check green.
2. **Never write a `method_id` in Go or TypeScript.** They are generated. Reserve unused ones in the
   `.proto` with `reserved_method_id`, never in a comment.

---

## 2. The domain

### 2.1 Nouns

**Project** — a named, independent graph, **with its own metadata** (title, description). You have several
at once (work, home, a side project). Which one a command acts on is resolved by the host (D11).

**Task** — a unit of work, belonging to exactly one project. Has an id, a title, optional notes, a status,
and a creation time.

**Edge** — "task A blocks task B, **for a reason**". A record in its own right, **not** a field on either
task (D1). The reason and its arguments are D8. Both endpoints are in the same project (D10).

### 2.2 Status, and what is derived from it

```
TODO → DOING → DONE
  ↘        ↘
     DROPPED
```

| Status | Meaning |
| --- | --- |
| `TODO` | not started |
| `DOING` | in progress |
| `DONE` | finished |
| `DROPPED` | abandoned — will not happen |

**`BLOCKED` is not a status.** It is derived from the graph, every time it is asked for. A stored blocked
flag is a second source of truth that goes stale the instant an upstream task completes, and nothing would
catch it.

**A task is READY when:** its status is `TODO` or `DOING`, **and** every task that blocks it is `DONE` or
`DROPPED`.

**`DROPPED` satisfies its dependents** — a dropped task unblocks whatever it was blocking. The alternative
strands the graph: an abandoned task would block its dependents forever with no way to move on except
deleting it. This is a rule the engine owns and a host must never re-derive.

### 2.3 Storage — split storage (§7.1), one file per record

```
<root>/projects/<project>/project.json           Project { name, title, description, created_at }
<root>/projects/<project>/tasks/<task-id>.json   Task
<root>/projects/<project>/edges/<edge-id>.json   Edge
```

Canonical JSON, proto-schema'd, and the file is the source of truth. No database, no index — see §8.

**Nothing about users or preferences appears here**, deliberately (D11). A default project is host-managed
state living outside this tree entirely, so the app's area holds app data and only app data — which is also
what makes an `export-fs` bundle portable between people without carrying anyone's working context.

**A project exists because its directory does — `project.json` describes it, it does not constitute it.**
The distinction matters and is easy to blur:

- There is **no central registry** listing projects. A list that can drift from what it describes is a
  second source of truth, for the same reason there is no id counter (D3). `projects` reads directories.
- **`project.json` is optional.** A directory without one is a valid project whose title defaults to its
  name and whose description is empty. So metadata is genuinely additive: a store written before this file
  existed still opens, and a project created by `mkdir` still works.
- Which makes `name` inside it **derived, not authoritative** — the directory name is the identity.
  `check` asserts they agree (§4.4 #11) rather than trusting the file.

---

## 3. Decisions already made

Do not re-litigate these. If one turns out to be wrong while building, say so and stop — do not quietly
adopt a different design.

### D1 — Edges are independent records, not fields on tasks

An edge is `edges/<id>.json` holding `{from, to}`. It is **not** a `blocked_by: []` list inside the blocked
task.

**Why, in the order the reasons matter:**

- **Deleting a task would otherwise mean rewriting every task that references it** — an N-file write that
  is not atomic, where a crash leaves a task record naming a task that no longer exists. With independent
  edges, deleting a task deletes its own file plus the incident edge files, each independent and
  idempotent, and a leftover edge is *detectable and repairable* rather than a corrupted record.
- **Both directions become symmetric.** With edges stored on the blocked task, "what blocks X" is free and
  "what does X block" is a scan of everything. Stored independently, both are a scan of the same
  collection — no privileged direction, and one thing to index later.
- **Concurrent edits collide far less.** §9 of the main plan syncs plain files last-writer-wins. Two people
  adding different edges to the same task write two *different* files; with embedded edges they would both
  rewrite one task file and one edge would be silently lost.
- **An edge can grow its own fields** (a dependency type, a note, a creation time) without touching the
  task schema.

**The cost, accepted:** every graph question reads the whole edge collection. At the scale a person's task
graph reaches, that is a directory scan of small files, which is what notes already does for every list.

### D2 — An edge's id is derived from its endpoints

`<from>--<to>` — for example `edges/t3--t7.json`.

Deterministic, so **adding the same edge twice is idempotent by construction** rather than by a check, and
duplicate edges cannot exist. It is also legible in a directory listing and in a BFT bundle, which is the
whole point of files-as-truth.

### D3 — Task ids are short handles: `t1`, `t2`, …

Derived as "highest existing number + 1" by scanning `tasks/`, with no counter file — a counter is state
that can disagree with the records, and the records are the truth.

**Why not notes' title-slug:** you type these ids constantly (`block t3 t7`), and `block auth-refactor
login-page` is unpleasant to type and wraps in a terminal. Legibility is served by `show` and `list`, which
print titles.

Single-writer assumption: two concurrent `add`s would derive the same id. Consistent with the rest of the
platform today (there is no lock file anywhere), and worth a line in the feedback log rather than a
solution.

**Cheap to change before any data exists — after that, not.** If you disagree, raise it in §9 before
Phase 1.

### D4 — The engine validates; nothing gets written until everything checks out

`block` must **reject a cycle**, and both endpoints must exist. `add --after` creates a task *and* edges;
validate every endpoint first, then write, so a partial failure never leaves half a command applied.

### D5 — Dangling edges are tolerated on read, reported by `check`

An edge can reference a missing task despite D4 — someone hand-edits the store, or an `import-fs` bundle
arrives from another machine. So:

- **Readiness ignores a dangling edge** rather than treating it as unsatisfied. A task blocked by a
  nonexistent task would otherwise be stuck forever with nothing explaining why.
- **`check` reports them**, along with any cycle that arrived the same way.

An engine that crashes on a store it did not write is an engine that cannot survive `import-fs`.

### D6 — Events announce that the graph moved; they do not carry the graph

One topic, `doneblock.graph-changed`, emitted after any mutation. The payload does **not** contain the
graph or a diff (Decision 33: the files are the truth, and a payload a subscriber could act on without
re-reading becomes a second source of truth).

**Consequence, stated so it is not mistaken for an oversight:** this app does **not** exercise Decision
34's semantic-render path the way tictactoe does. A board is small enough to put in every event; a task
graph is not. Hosts here re-query — which is the *invalidation* pattern, and the one no app has stressed
with a large state.

### D7 — Two host slots, and the web one does not draw a graph yet

`hosts/native/` (terminal) is the deliverable. `hosts/web/` renders the **same answers as a list**, not as
a node-and-edge diagram: graph layout is a real project and would dominate the app.

That is enough for host parity to be meaningful — two slots rendering the same engine answers, neither
deciding anything — which is the point of having a second slot at all.

### D8 — Every edge carries a blocking REASON, with reason-specific arguments

Not every block is the same kind of stuck, and the difference is what makes the tool useful: a task waiting
on a person is a nudge, a task waiting on work you could do yourself is a decision about what to pick up.

```proto
enum BlockingReason {
  BLOCKING_REASON_UNSPECIFIED = 0;  // treat as DEPENDENCY (the BundleFormat precedent)
  BLOCKING_REASON_DEPENDENCY  = 1;  // plain: the blocker's work must finish first
  BLOCKING_REASON_WAITING_ON  = 2;  // a person or team          → who
  BLOCKING_REASON_SCHEDULED   = 3;  // not before a date         → not_before
  BLOCKING_REASON_RESOURCE    = 4;  // a thing that is not free  → resource
  BLOCKING_REASON_DECISION    = 5;  // someone must decide       → note
}

message Edge {
  string from = 1;
  string to = 2;
  BlockingReason reason = 3;
  string who = 4;         // WAITING_ON
  string not_before = 5;  // SCHEDULED — an opaque string; see below
  string resource = 6;    // RESOURCE
  string note = 7;        // free text, valid with ANY reason
}
```

**Flat fields, not a `oneof`, and this is a framework constraint rather than a modelling preference.** A
`oneof` would make invalid states unrepresentable, which is what this codebase normally reaches for. But
the generated command surface (`protoc-gen-dlc-registry`) understands scalars, enums, bytes and repeated
fields; a message or `oneof` field becomes `KindUnsupported` and cannot be set from a command line at all.
So the schema is flat and **the engine validates the pairing instead**:

> Supplying an argument that does not belong to the chosen reason is an **error**, not silently ignored —
> the same stance `manifest.go` takes on an unknown key.

**Record this in the feedback log** (§10): "make invalid states unrepresentable" and "the CLI is generated
from the schema" pull against each other here, and this app is the first place it has bitten.

**`not_before` is an opaque string to the engine**, stored and displayed, never compared. See D9.

**One edge per ordered pair** (D2 derives the id from the endpoints), so "A blocks B" has exactly one
reason. If a block genuinely has two causes, `note` carries the nuance. Two edges between one pair would
need a second id scheme and would make readiness ambiguous, for a case nobody has hit yet.

### D9 — A reason NEVER changes readiness

Reasons explain; they do not decide. The readiness rule in §2.2 is unchanged: every blocker must be `DONE`
or `DROPPED`, whatever the reason says. In particular a `SCHEDULED` edge does **not** become satisfied when
its date passes.

Two independent reasons, either of which would be sufficient:

- **The engine has no clock.** There is no clock capability (§4 notes the same gap for `created_at`), so
  "has the date passed" is not a question the engine can answer. A host could supply the time, but then
  readiness would be a fact the host contributed — which is exactly the line Decision 34 draws.
- **It would make parity non-deterministic.** Native and wasm runs happen at different instants, so a
  time-dependent answer would differ between tiers for reasons that have nothing to do with correctness,
  and the parity check would either flake or have to stop looking at readiness.

So `SCHEDULED` is a note-to-self that `show` and `ready --all` display. **If that feels insufficient while
using the tool, that is a finding about the missing clock capability** — record it rather than working
around it.

### D10 — Projects are directories inside the granted root, and every command takes `--project`

**Not separate filesystem roots.** The tempting alternative is to make a project a different root and let
the host grant one — the engine would never learn projects exist. It is rejected because *simultaneously*
is the requirement: `ready` across every project, and `check` over the whole store, both need to see more
than one graph at a time, and the platform's root is process-global (`platform.Root()`, one manifest, one
`Boot`). One root, projects inside it.

**A current project, persisted in `current.json`, and always overridable** — §5.4's rule for host modes
applied to app state: detect a default, never make it ambient. Every command that touches the graph takes
`--project/-p`; empty means "the current one".

**Consequence worth stating, because it is friction and it is the point:** the generated command surface
builds flags **per request message**, so "a flag every command accepts" means a `project` field on **every
request message** — eleven of them. There is no global flag, no persistent flag, no inherited flag. That is
the first thing this app wants that the framework has no shape for. Do not invent one; add the field
eleven times, and **record it** (§10).

**Ids are unique within a project**, so `t1` exists in each. Wherever a command accepts a task id it also
accepts a **qualified** `project/id`, which is what cross-project output prints — so anything you can read
you can paste back into the next command. A bare id means the selected project.

**Edges may not cross projects.** `block work/t3 home/t7` is refused with a clear error. Cross-project
dependencies are real, and this is a deliberate simplification: it keeps cycle detection, `check` and the
DAG itself scoped to one project. **If the restriction chafes in real use, that is a finding** (§10) — it
is exactly the kind of thing a plan should not decide by guessing.

### D11 — The ENGINE has no user concept; the host owns identity, defaults, and `use`

**The app does not know what a user is.** Not "it looks one up in a manifest", not "it reads an env var" —
it has no notion at all. Identity, per-user preferences, and the storage those preferences live in are
**host/tier concerns**, coordinated by infrastructure the engine never sees.

What that means concretely:

| Concern | Who | Where |
| --- | --- | --- |
| who is invoking this | **host** | OS user, browser profile, device — the host already knows |
| this person's default project | **host** | host-managed storage, **outside the app's granted area** |
| `use <project>` | **host-side verb** (Decision 30) | never reaches the engine |
| which projects exist, and everything about them | **engine** | the app's granted area |

**`--project` becomes ordinary required input.** The engine takes a project name, and errors clearly when
it does not get one. The convenience — not typing it — is the host filling the flag in from what it
remembered, which is squarely Decision 28's "each tier builds the request its own way". Choosing *what to
ask* is host work; computing *the answer* stays engine work, so Decision 34 is untouched.

**`use` becomes a host-side verb**, alongside `build`/`gen` in Decision 30's split. **No scaffolded app has
ever had one** — `dlc` does, but notes and tictactoe are pure engine surfaces — so this is new ground and a
likely source of findings (§10): a generated runner owns argv today, and an app-specific host verb has to
be handled before it.

**"Out of the app area" is the load-bearing half.** Host-managed user info must live where the app cannot
reach it, which the granted root already guarantees: the engine's preopen *is* the app area, so anything
outside it is unreachable by construction rather than by agreement. Natively that is `~/.config/doneblock/`
and needs nothing from the platform — a host is an ordinary program.

**On the web the mechanism now exists** (built 2026-07-29): `dlc-platform/web/opfs.ts` exports
`HOST_RESERVED` and skips the top-level **`.ilc-host`** prefix on both hydrate and flush, so a browser host
may keep state there and the engine will never see it, and it will never appear in an `export-fs` bundle.
Use that prefix; do not invent another. (The flush guard is the one that matters — `writeDir` mirrors, so
without it the host's own state is deleted by the next command that writes.)

**The app may STORE what the host tells it; it may never resolve or reason about users.** If DoneBlock ever
wants `created_by`, the host injects a string and the engine stores it opaquely — exactly as `created_at`
already works. That is provenance, not a user model, and the distinction is what keeps this rule
checkable.

**DoneBlock is a single-user app — declared in its README, not in `dlc.toml`.**

There is **no `users` key in `dlc.toml` yet and nothing reads one**, so do not add it: `manifest.go` errors
on unknown keys by design, and adding a key the platform does not know would be an app changing the
platform from the inside (§9). The mode is a *design statement* about this app's data model, and it belongs
in prose until something acts on it. The taxonomy itself lives in `DEVALBO-DLC-GO-TASKS.md`:

| Mode | What it means for the app |
| --- | --- |
| **`single`** | the data model has no "who" in it — no `created_by`, no assignment, one writer assumed |
| **`multi`** | records may carry host-supplied provenance, and the app may not assume it is the only writer |

**This is not the same question as "does the host have users".** A host may serve many users and grant each
a partitioned root; every one of those partitions is a `single` store, and the app cannot tell — which is
the whole point. `single` is a statement about *this app's data model*, not about the world it runs in.

**Do not call `platform.Isolated()`.** The platform gained it on 2026-07-29 (`AGENTS.md` §3·5): a host can
now declare whether the root it granted belongs to one person. It exists for apps holding **private** data
that need to know whether privacy is their own problem — a game host with per-player hidden state is the
motivating case. DoneBlock is your own task graph, so the question does not arise, and an app checking a
fact it cannot act on is noise. It is named here only so it is not cargo-culted in from another app.

So DoneBlock stores no `created_by`, names no actor in any output, and assumes one writer. **If that turns
out to be the wrong call** — if the first thing you want on a shared graph is who added a task — that is a
finding worth recording (§10), and the change is a declared mode plus one opaque string field, not a
redesign.

---

## 4. The schema and the command surface

`proto/doneblock/v1/commands.proto`. Package `doneblock.v1`, `go_package` ending `;doneblockv1`.

**In this framework the command surface IS the schema** (`AGENTS.md` §3b): flags, help text, required-ness,
positionals and enum menus are all generated from the `.proto`. So §4.2 is not documentation of a CLI
written elsewhere — it is a specification of the proto that produces it.

### 4.1 Method ids

**10000+ is yours** (1–9999 is the framework's). Every id below is permanent once written;
`proto/method-ids.lock` is committed and the build fails if one changes.

| id | rpc | CLI |
| --- | --- | --- |
| 10000 | `AddTask` | `add` |
| 10001 | `ListTasks` | `list` |
| 10002 | `ShowTask` | `show` |
| 10003 | `SetStatus` | `set` |
| 10004 | `Done` | `done` |
| 10005 | `Block` | `block` |
| 10006 | `Unblock` | `unblock` |
| 10007 | `Ready` | `ready` |
| 10008 | `Graph` | `graph` |
| 10009 | `Check` | `check` |
| 10010 | `DeleteTask` | `rm` |
| 10011 | `InitProject` | `init` |
| 10013 | `ListProjects` | `projects` |
| 10014 | `DeleteProject` | `rm-project` |
| 10015 | `SetProject` | `set-project` |

**`use` has no id and is not in this table** — it is a host-side verb (D11, Decision 30) and never reaches
the engine.

Reserve, do not implement: **10012** (held: `use` was designed as an engine verb before D11 moved it
host-side — reserved so the number cannot be quietly reused for something else), **10016** `UpdateTask`
(edit title/notes), **10017** `RebuildIndex` (§8).

```proto
option (devalbo.options.v1.reserved_method_id) = 10012;
option (devalbo.options.v1.reserved_method_id) = 10016;
option (devalbo.options.v1.reserved_method_id) = 10017;
```

### 4.2 Command reference

Conventions: `<required>` · `[optional]` · a **task id** is `t3` or the qualified `work/t3` (D10).

**`-p, --project <name>` is on every graph command** and omitted from the tables below purely to keep them
readable — it is a real field on every one of these request messages. The engine **requires** it; the host
fills it in from the default it remembered when you do not type it (D11).

Remember the parser puts **flags before positionals** (§11.1).

#### Tasks

| Command | Positional | Flags | Notes |
| --- | --- | --- | --- |
| `add` | `<title>` | `-n, --notes <text>`<br>`--after <id>` (repeatable)<br>`--before <id>` (repeatable) | creates a task; `--after`/`--before` also create edges, validated first (D4) |
| `list` | — | `-s, --status <status>`<br>`--all-projects` | `--status` is an enum → a menu on an interactive host |
| `show` | `<id>` | — | the task, what blocks it, what it blocks, and **why** each block exists (D8) |
| `set` | `<id>` `<status>` | — | `set t3 doing` |
| `done` | `<id>` | — | the common case; delegates to `set` |
| `rm` | `<id>` | `--force` | deletes the task **and its incident edges**; `--force` required if it has any |

#### Edges

| Command | Positional | Flags | Notes |
| --- | --- | --- | --- |
| `block` | `<blocker>` `<blocked>` | `-r, --reason <reason>`<br>`--who <name>`<br>`--not-before <date>`<br>`--resource <text>`<br>`--note <text>` | rejects cycles, missing endpoints, cross-project pairs, and args that do not match the reason (D4, D8, D10) |
| `unblock` | `<blocker>` `<blocked>` | — | idempotent: removing an edge that is not there succeeds |

#### Queries

| Command | Positional | Flags | Notes |
| --- | --- | --- | --- |
| `ready` | — | `--all-projects`<br>`--blocked` | **the daily driver**. `--blocked` also lists what is *not* ready, with the reason it is stuck |
| `graph` | — | `-f, --format <text\|dot>`<br>`--all-projects` | `dot` is for piping into Graphviz |

#### Projects

| Command | Positional | Flags | Notes |
| --- | --- | --- | --- |
| `projects` | — | `-v, --verbose` | every project: name, title, task count. `--verbose` adds descriptions. **The host annotates which is your default** — the engine does not know |
| `init` | `<name>` | `-t, --title <text>`<br>`-d, --description <text>` | create a project and write its metadata |
| `use` | `<name>` | — | **host-side (D11)** — remembers your default; never reaches the engine. Fails if the project does not exist, which it checks by asking the engine |
| `set-project` | `[name]` | `-t, --title <text>`<br>`-d, --description <text>` | edit metadata; no name means the current project. Only the flags you pass are changed |
| `rm-project` | `<name>` | `--force` | refuses a non-empty project without `--force` |

#### Integrity

| Command | Positional | Flags | Notes |
| --- | --- | --- | --- |
| `check` | — | `--all-projects`<br>`--fix` | §4.4 — the required integrity command |

### 4.3 Messages

`Task { id, title, notes, status, created_at }` · `Edge { from, to, reason, who, not_before, resource,
note }` (D8) ·  `Project { name, title, description, created_at }` ·
`ProjectSummary { project, task_count, ready_count, is_current }` for `projects` output ·
request/response pairs · `GraphChangedEvent` carrying the topic option.

**`Project` is the stored record; `ProjectSummary` is an answer.** Counts are computed per call and never
written to disk — a stored count is a derived value that goes stale on the next `add`, which is the same
trap as a stored `BLOCKED` flag (§2.2).

`BlockRequest` carries the reason and its arguments, so the CLI reads:

```
block t3 t7                                   # plain dependency
block t3 t7 --reason waiting-on --who alice
block t3 t7 --reason scheduled --not-before 2026-08-01
```

**An enum field becomes menu choices** on an interactive host — `EnumValues` in the generated `clispec`.
No app has exercised that yet, so watch whether `--reason waiting-on` or
`--reason BLOCKING_REASON_WAITING_ON` is what the runner actually accepts, and record it.

**`Done` is deliberately redundant with `SetStatus`** — `done t3` is the single most common thing you will
type, and the generated CLI gives one name per rpc, so a convenience verb *is* a second rpc. The handler is
one line delegating to shared code. A schema-driven surface makes aliases cost an rpc; record it (§10).

**`created_at` comes from the host**, as it does in notes: the engine has no clock capability. Keep that
comment; it is a real gap the example apps exist to surface.

### 4.4 `check` — what integrity actually means here

The required integrity command, and the app's answer to "the files are the truth, so what happens when the
files are wrong?" A store can be hand-edited, restored from a stale backup, or written by `import-fs` from
another machine — **so every invariant the engine maintains on write must also be checkable after the
fact.** The engine must survive all of these on read (D5); `check` is what names them.

Per project, or across all with `--all-projects`:

| # | Invariant | How it breaks |
| --- | --- | --- |
| 1 | every edge endpoint exists | a task was deleted by hand, leaving `t3--t7.json` |
| 2 | the graph is acyclic | two edges hand-written, or merged from two machines |
| 3 | no self-edge (`t3 → t3`) | a trivial cycle, worth naming separately because the message can be better |
| 4 | an edge's id matches its endpoints (D2) | `t3--t7.json` containing `{from: t2}` |
| 5 | a task's id matches its filename | `t3.json` containing `{id: "t9"}` |
| 6 | every record decodes | a truncated write, or hand-edited invalid JSON |
| 7 | `status` is a known enum value | an older or newer version of the app wrote it |
| 8 | an edge's arguments match its reason (D8) | hand-edited, or written before a reason was renamed |
| 9 | no stray files where records belong | `tasks/notes.txt`, or a directory |
| 10 | `project.json`'s `name` matches its directory | a project directory was renamed by hand |
| 11 | no edge crosses projects (D10) | two stores merged, or a hand-written edge |

**Output names the record and the problem**, and exits non-zero if anything was found — it is the kind of
command that ends up in a script.

**`--fix` is deliberately narrow: it removes dangling edges (1) and nothing else.** Every other invariant
needs a judgement a tool should not make — which of two conflicting ids is right, which edge to drop to
break a cycle, what a corrupt record was meant to say. A `--fix` that guessed would turn a diagnosable
problem into a silent data change, which is worse than the problem.

**This command is also the app-level rehearsal for `rebuild-index`** (§8): both re-derive truth from the
files, and if an index ever lands, `check` is where its agreement with the files gets asserted.

---

## 5. What the engine owns, and what a host may do

| Engine (portable, one implementation) | Host slot (per tier) |
| --- | --- |
| readiness, ordering, cycle detection, validation | drawing a list, a table, a tree, colours |
| what `check` found | how a problem is displayed |
| every answer the CLI prints | how it is laid out |

A host may highlight what the engine named. It may not compute what the engine did not send. If a slot
needs a fact, the answer is a new engine command, not a host-side derivation.

**Mechanically enforced by host parity**: both slots render the same engine answers, and their normalized
output is compared. See tictactoe's `hosts/native/projection_test.go` and `hosts/web/test/parity.spec.ts`.

---

## 6. Build order

The app lives at **`example-apps/doneblock/`**, alongside notes and tictactoe, and is picked up
automatically by `scripts/verify-example-apps.sh` (it iterates `example-apps/*/` and requires a
`dlc.toml`).

Each phase leaves the tree green, and **no phase is done until something is broken on purpose and observed
going red** (`AGENTS.md` §5).

### Phase 0 — scaffold, and prove it runs before writing any domain code

**This phase was dry-run on 2026-07-29 and found three framework bugs before a line of the app existed.**
They are recorded in §11 so you do not rediscover them. Use the invocation below exactly — the obvious
spelling does not work.

```bash
# from the repo root, inside devbox
BIN="$(mktemp -d)"
go build -buildvcs=false -o "$BIN/dlc" ./hosts/native

cd example-apps
"$BIN/dlc" new --tiers native --tiers web \
  --module github.com/devalbo/devalbo-ilc/example-apps/doneblock \
  --platform-path ../.. \
  doneblock

cd doneblock && PATH="$BIN:$PATH" make gen && go mod tidy && make verify
```

Note three things about that command, each of which cost a failed attempt:

- **Flags come BEFORE the positional name.** `dlc new doneblock --module …` fails with
  `unexpected argument "--module"` — the flag parser stops at the first non-flag argument. The tool's own
  `--help` prints `dlc new <name> --tiers <tiers>`, which is the order that does not work (§11.1).
- **`--tiers` is repeatable, not comma-separated.** `--tiers native,web` fails with
  `tier "native,web" is not supported yet`.
- **`--platform-path` is written into the generated `dlc.toml` verbatim**, so it must be relative to the
  *new project*, not to your shell. From `example-apps/`, `../..` is correct because the app lands at
  `example-apps/doneblock/`.

**Run `make gen` from a shell that has the repo's toolchain on PATH** (the repo's `devbox shell`), not the
scaffolded project's own devbox environment — that environment is missing `protoc-gen-es-lite` and cannot
generate (§11.2).

Do not proceed until `make verify` passes.

### Phase 1 — projects exist, and the host remembers which one

Engine: `init`, `projects`, `set-project`, `rm-project`, `project.json`, and `--project` as required input
with a clear error when absent.

Host (native slot): `use` as a **host-side verb** (D11), the store for it **outside the app's granted
area** (`~/.config/doneblock/`), and filling `--project` into the request when the user did not type it.

**This is the phase with unknowns in it**, because no scaffolded app has ever had a host-side verb — the
generated runner owns argv, so intercepting one before it is new ground. Expect a finding here; if it turns
out to need a framework change, stop and record it rather than inventing one (§9).

*Falsify:* delete a project's `project.json` → the project still opens, titled with its own name (§2.3).
Run a command with no default remembered and no `--project` → an error naming `use`, **not** a guess at the
only project. Point the host's remembered default at a project that no longer exists → a clear failure, and
note where it is caught: the host asks the engine, so the engine's answer is what makes it detectable.
Finally, `export-fs` the store and confirm **no trace of a default or a user is in the bundle** — that is
the property "out of the app area" buys.

### Phase 2 — tasks exist

`add`, `list`, `show` (no graph relations yet), `set`, `done`, `rm`. Ids per D3, unique within a project.
`--project` on every request, and qualified `project/id` accepted wherever an id is (D10). Emit
`doneblock.graph-changed` on every mutation.

*Falsify:* remove the id-derivation scan → two `add`s collide and the second overwrites the first. Then
create `t1` in two projects and confirm they do not see each other.

### Phase 3 — edges exist, with reasons

`block`, `unblock`, edges under `edges/` with derived ids (D2). Cycle rejection and endpoint validation
(D4). Blocking reasons and their arguments (D8), including engine-side rejection of an argument that does
not match its reason, and of a cross-project pair (D10). `show` gains both directions and prints why each
block exists.

*Falsify:* drop the cycle check → `block t1 t2 && block t2 t1` succeeds and `ready` never lists either.
Then pass `--reason scheduled --who alice` → it must be refused, not quietly stored.

### Phase 4 — readiness

`ready`, including `--blocked` and `--all-projects`. `DROPPED` satisfies dependents (§2.2); a reason never
changes readiness (D9). **This is the phase the app exists for.**

*Falsify:* treat `DROPPED` as unsatisfied → a dropped blocker strands its dependents, and the test says so.

### Phase 5 — `graph` and `check`

`graph` (text tree, and `--format=dot`). `check` in full — all eleven invariants of §4.4, `--all-projects`,
and `--fix` limited to dangling edges.

**Write `check` last on purpose:** by now there are four phases of invariants to check, and every one of
them was written as a rule the engine enforces on write. `check` is where they become properties of the
*store* rather than of the code path that happened to create it.

*Falsify:* hand-edit the store to break each invariant in turn — a truncated JSON file, a renamed project
directory, an edge whose filename disagrees with its endpoints — and confirm `check` names each one and
exits non-zero. Then confirm `--fix` removes a dangling edge and refuses to touch anything else.

### Phase 6 — the web slot and host parity

The browser renders the same answers as a list (D7). A parity test comparing normalized renderings of the
two slots over a fixed set of graphs, following tictactoe's pattern.

*Falsify:* have one slot sort differently → parity goes red.

### Phase 7 — the feedback pass

Write §10 up properly. This is a deliverable, not a postscript.

---

## 7. Mechanics

Run everything inside `devbox shell`, or prefix with `devbox run --`.

| Task | Command |
| --- | --- |
| regenerate proto + registry + `dlcconfig` | `make gen` (wraps `buf lint` and `dlc gen`) |
| build the native CLI | `make build` |
| unit tests | `go test ./...` |
| native smoke test | `make verify` (the binary exists and answers) |
| build the web tier | `make build-web` (needs `dlc` on `PATH`) |
| serve the web tier | `make dev-web` |
| browser tests | `cd hosts/web && npx playwright test` — there is **no** app-level `make verify-web`; the repo runs these via `scripts/verify-example-apps-web.sh` |
| the whole repo | `./scripts/ci.sh full` from the root |
| just the example apps | `./scripts/verify-example-apps.sh` (native) · `./scripts/verify-example-apps-web.sh` (browser) |
| re-bless a changed id lock | `DLC_ID_LOCK_UPDATE=1 make gen` — deliberately, never reflexively |

`dlc` must be on `PATH` and current: `make gen` runs `dlc gen`, and templates are embedded in the binary,
so a stale `dlc` checks a stale shape.

**Never run `git`** — the maintainer runs every git command. Suggest them instead.

**Never `go build ./cmd/x` bare** — Go writes the binary into the current directory. Use
`go build -o "$(mktemp -d)/x" ./cmd/x`, or `go vet` when you only want to know it compiles.

---

## 8. No index — and what to do when the scan feels slow

Every graph question reads every task and edge file. **That is deliberate**: the derived index is designed
but not built ([`INDEX-PLAN.md`](./INDEX-PLAN.md)), and this app is one of the things that should decide
what it becomes.

So: **do not build an index, a cache, or a "just this once" in-memory map that outlives a command.** If the
scan is annoying, that is the finding — write down *which query* hurt and at *what size*, because that is
exactly the input the index plan is missing. `RebuildIndex` is reserved at 10012 for when it arrives.

---

## 9. Ask, do not guess

Genuinely open. Raise them before the phase that needs them:

1. **`DOING` — worth having?** §2.2 includes it and `ready` shows it. If it feels like ceremony while
   building Phase 3, say so; removing an enum value before any data exists is free.
2. **Should `ready` show *why* something is not ready** — i.e. does `ready --all` list blocked tasks with
   their blockers? Useful, but it is a second query shape, so it needs a reason.
3. **Ordering of `ready`.** Creation order is the obvious default; "most downstream tasks unblocked first"
   is more useful and more opinionated. Pick creation order unless told otherwise, and record the question.
4. **Should a project be archivable** rather than only deletable? `rm-project --force` is destructive and
   a finished project is worth keeping. An `archived` flag on `Project` would hide it from `projects` and
   `ready --all-projects`. Left out because it is one more state to test and nobody has wanted it yet —
   raise it if using the tool makes you want it.
5. **In-repo vs its own repository.** This plan assumes `example-apps/`. A separate repo would be a more
   honest test of "an app depends on the platform, not on `dlc`" and would immediately hit the unpublished
   module friction (`--platform-path`, `replace`, `file:` npm deps). That was raised and not settled.

Anything else that requires inventing domain behaviour: ask. Anything requiring a **framework capability
that does not exist**: stop, record it in §10, and ask — do not add a capability to the platform from
inside an app.

---

## 10. What this app is for — the feedback log

Keep a running log **while building**, not afterwards. One entry per friction point:

> **What I wanted to do** · **what the framework made me do instead** · **the workaround** · **whether this
> should become a task or be accepted**

Put it in `example-apps/doneblock/FEEDBACK.md`, and copy anything that becomes a framework task into
[`DEVALBO-DLC-GO-TASKS.md`](./DEVALBO-DLC-GO-TASKS.md).

**Questions this app is specifically positioned to answer**, because no existing app can:

- **What does a real index need?** Every query here is a traversal, and "what does this block" is a reverse
  lookup. notes only ever wanted titles sorted by name — a much weaker signal than this
  ([`INDEX-PLAN.md`](./INDEX-PLAN.md) is waiting on exactly this).
- **Does "the engine decides" survive a genuinely tempting case?** A browser graph library would happily
  compute readiness. Whether that temptation is resisted — and whether parity catches it if not — is the
  first real test of Decision 34.
- **Is the schema-driven CLI pleasant at a dozen verbs?** notes has four. Watch what happens to `done` vs
  `set`, and to flags that want to be positional.
- **Does relational data fit files-as-truth?** Two collections referencing each other, with referential
  integrity the engine has to maintain by hand. If that is painful, say how.
- **Is `single` the right call, and does the binary hold?** The mode claims an app's data model either has
  a "who" in it or does not. The first time you want to know who added a task on a shared graph, that
  claim is under test — and the fix should be one declared mode plus one opaque string, not a redesign.
- **What breaks with no clock, no lock file, and no update verb?** All three are known gaps; this is the
  first app that will feel all of them in one sitting. D9 already turns one of them into a design
  constraint before the app exists.

---

## 11. Findings already produced (2026-07-29, before any code)

Phase 0 was dry-run into a scratch directory and the scaffold was deleted afterwards. Three real bugs, all
reproducible, none of which any existing check catches. **They are framework tasks, not app work** — do not
fix them from inside this app (§9).

### 11.1 `dlc new`'s own help prints an invocation that does not work

`dlc new --help` prints `USAGE: dlc new <name> --tiers <tiers>`, and that order fails:

```
$ dlc new doneblock --module github.com/x/y --tiers native
dlc: new: unexpected argument "--module"
```

The flag parser stops at the first non-flag argument, so every flag after the positional name is treated as
a stray argument. The generated usage line puts the positional first because that is how the command reads;
the runner requires the opposite. Either the runner should permit flags after positionals, or the usage
line should stop advertising a spelling that fails.

Note that the error message is good — it names the argument — which is why this costs one attempt rather
than a debugging session.

### 11.2 A scaffolded project cannot run `make gen` in its own devbox environment

The very first instruction `dlc new` prints is `cd doneblock && devbox shell` / `make gen`. In that
environment it fails:

```
Failure: plugin protoc-gen-es-lite: exec: "protoc-gen-es-lite": executable file not found in $PATH
```

The scaffolded `devbox.json` installs `protoc-gen-go-lite` in its `init_hook` but never installs
`protoc-gen-es-lite`, which the generated `buf.gen.yaml` requires as soon as a `web` tier exists. The repo's
own `devbox.json` installs it (`npm i -g @aptre/protobuf-es-lite`); the template's does not.

### 11.3 Nothing verifies a scaffold in its own declared environment — which is why 11.2 survives

`verify-scaffold.sh` scaffolds a project and runs `make gen` **inside the repo's devbox shell**, where
`protoc-gen-es-lite` is already on PATH. So the scaffold's own `devbox.json` — the environment every real
user of `dlc new` would actually be in — is never exercised by any check.

This is the same class of gap as the two `verify-platform-gen.sh` was written for: a thing that is only
broken from *outside* the repo, and is therefore invisible from inside it. The fix is a check that runs a
scaffolded project's `make gen` in its own environment, which would have caught 11.2 on the day the web
tier was added to the template.
