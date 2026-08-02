# AGENTS.md — rules for working on `dlc` / ILC

For anyone (human or agent) changing this repo. These are the rules that are **not** discoverable by
reading the code — each one exists because breaking it fails somewhere far from the cause.

Architecture and rationale live in [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md); current
state lives in [`README.md`](README.md). This file is only the rules.

---

## 1. Method ids

**Reserve ids in the `.proto`, never in a comment.** A comment is a hope; a declaration is checked.

```proto
service PlatformService {
  option (devalbo.options.v1.reserved_method_id) = 3;   // held for SupportedAbis
}
```

`protoc-gen-dlc-registry` fails the build if an rpc claims a reserved id, and records reservations in the
committed `proto/method-ids.lock`. Proto's own `reserved` keyword covers field numbers only — it does not
reach custom-option values, which is why this option exists.

**Never write an id in Go or TypeScript.** They are generated (`*.registry.pb.go`, `*.registry.pb.ts`).
Hand-mirroring is how an id ends up living in two places that silently disagree.

**Bands are permanent** (Decision 32 / `dlc-platform/registry.go`):

| Range | Owner |
| --- | --- |
| 1 – 599 | ILC's inherited verbs, in per-capability blocks (core 1–99, filesystem 100–199, …) |
| 600 – 8999 | held for future capability verbs — **over-provisioned, see below** |
| 9000 – 9099 | **`dlc`'s own engine-served verbs** (`new`, `echo`) |
| 9100 – 9999 | **`dlc`'s own host-local verbs** (`gen`, `build`, `run`) |
| 10000 + | **the app, and only the app** |

**`dlc` moved out of the app band on 2026-07-29**, reversing Decision 29's "claims no privileged block". The
old argument still holds as far as it went — `dlc` and a scaffolded app never share a registry, so collision
was impossible either way — but `10000` meant "some app's first command, *or* dlc's `New`", and neither a lock
file nor a wire trace told you which. The gain is legibility, not safety.

**Why the block was affordable:** 600–9999 was reserved for capability verbs, and capabilities turned out not
to be command-shaped. Events is an import (Decision 33), a shared render is a pulled *app* command
(Decision 35), network is `wasi:http` — none consume method ids. **Seven framework ids exist in total.** The
realistic future claimants are more *inherited* verbs (§7.3's open file verbs at 103+), and those have a home
already.

**Changing an id is a breaking change.** `buf breaking` cannot see it (it validates message wire compat,
not an option's *value*), so the lock is the only guard. Re-bless deliberately:
`DLC_ID_LOCK_UPDATE=1 make gen`.

## 2. The engine

**One entry point:** `execute(method, request)`. Do not add a second boundary. Capability facts arrive as
*data* on this boundary (the environment manifest, §6.4a) rather than as new WIT imports.

**Reflection-free and TinyGo-safe.** No `encoding/json`, no `text/template`, no `reflect`. The engine
compiles to wasm for the browser and eventually to embedded targets; a reflection-heavy dependency does
not merely bloat, it fails to build. Generics are fine and are how the typed-handler adapter works.

**Never `os.WriteFile` a caller-supplied path.** Use `platform.SafeJoin` / `platform.WriteTree`, so path
containment is inherited rather than re-implemented per command.

**Portability traps already paid for** — do not re-introduce:
- `os.IsNotExist` / `errors.Is(err, fs.ErrNotExist)` do **not** match TinyGo's WASI errno
- `os.RemoveAll` fails under wasip2 (errno 52) even on a plain file — walk with `ReadDir` + `os.Remove`
- WASI has no working directory; anchor paths at `platform.Root()` — which a host must GRANT (§3·5)

## 3. The platform boundary

`dlc-platform/` is what every app **inherits**; `engine/` is `dlc`'s own app code. It **is** a separate Go
module — `github.com/devalbo/dlc-platform`, named for where it is going and resolved by `replace` until it
gets there (§16.4). So:

**Templates depend on the platform; they never inline it.** Code copied into a scaffold is frozen there
forever — a path-containment fix inlined into a template could never reach an already-generated app. The
one deliberate exception is `options.proto`, vendored because a generated project cannot otherwise resolve
`method_id`; it is a byte-copy guarded by a test.

**`dlc` is an app like any other.** If `dlc` needs special treatment, the template is teaching something
`dlc` does not do.

**Dogfood drift is one-directional and invisible**, so it is reviewed rather than assumed: a capability
lands, the example app and the template adopt it, and `dlc` quietly keeps the old shape — leaving the tool
that teaches the pattern as the one app not following it, with every check still green. **When a capability
or a plan phase lands, run the dogfood checklist** in the tasks doc (does `dlc` use the generated CLI
surface, a `dlc.toml`, `hosts/<tier>/` slots, a port seam?) and either fix the gap or record it as
deliberate debt, as `hosts/README.md` does for `frontend/`.

**Events (`platform.Emit`) — three rules, each of which fails far from its cause:**
- **A host must never call `Execute` from inside a sink.** The engine is on the stack; re-entering it
  mid-command is how you get corrupt state or a deadlock. The web host is safe by construction (Comlink is
  a message boundary); a native host must defer explicitly.
- **`emit` stays synchronous and returns nothing.** An `async` web-side import returns a Promise, and jco
  only supports that under `--async-imports`, which we refuse (Decision 22). The failure surfaces as a jco
  type error nowhere near the edit.
- **Emit AFTER the write, once per command.** A subscriber re-reads on the event and must find the new
  state there. Once per command, never per record — a 1000-file import must not become 1000 messages.

**Host facts and app facts are different signals, and neither side learns the other's vocabulary.**
`subscribe` carries what the ENGINE announces about its own domain — `notes.record-changed` means something
only if you know notes. `onFlush` carries a HOST fact: the tree is persisted. Which one to use follows from
what the listener is watching:

- watching the **application** (a record list, a view of app state) → `subscribe`. The payload says *which*
  record and *why* (the causing `method_id`), which is enough to ignore your own writes or update one row.
- watching the **filesystem** (a file browser, a future sync) → `onFlush`. It knows nothing about what a
  command means, and should not have to: the host publishes that something persisted, and the watcher
  infers what that implies for it.

**They are not interchangeable, in both directions.** A filesystem watcher driven by events is inferring
"files moved" from "the app said something happened" — app-coupled reasoning that misses a command which
writes without emitting, since a flush happens after *every* `execute` while an event fires only when a
handler chooses. And a view of app state driven by flushes has to re-read everything on every command,
including reads, because a flush carries no payload — tolerable while the store is small and wrong at any
real size.

**A capability's absence is a no-op, never an error** (Decision 33). App code must not be able to tell
whether anyone is listening, because on some tier nobody is, and code that branches on it behaves
differently per tier — the divergence this architecture exists to prevent.

**`dlc.toml` capabilities declare what an app can REACH, not what it can ANNOUNCE.** Console, filesystem,
display, network are privileges a host could refuse. The index is not among them — it is engine-owned (§3·7). Emitting carries nothing back and cannot be
refused, so it is not declared. See Decision 33 before adding the next capability to that list.

## 3·5 The filesystem root is GRANTED, never assumed

**A host must call `platform.SetRoot` before the engine touches a file**, exactly as a browser host installs
a WASI preopen before instantiating the component. `Root()` panics without a grant, deliberately: the old
behaviour was a bare `"."`, meaning "wherever the user happened to be standing", and that is not a root.

**Why it panics rather than defaulting.** `reset-fs` is an INHERITED verb — every ILC app has it and no
author writes it — and with the working directory as root it recursively clears whatever folder the app was
run in. It deleted a bundle during development; in a user's home directory it is data loss from a command
the app never opted into. A default here fails as destruction, not as an error.

**The convention is `./.<app>/`** (`platform.AppRoot(dlcconfig.Name)`) — project-local like git, so two
projects keep two stores, but confined, so `reset-fs` can only ever clear the app's own subtree.
**`dlc` overrides to `"."`** because its data IS the user's project: `dlc new` must scaffold where you are
standing. The rule generalises — an app whose output belongs to the *user* takes `"."`; one that keeps a
private store takes the convention.

On wasm `SetRoot` is a no-op: the grant already happened when the host installed the preopen, and the guest
cannot rebind it. Both tiers call it anyway, so the startup sequence reads the same everywhere.

**A narrower root is how per-user data partitioning works, and it is exactly as trustworthy as the host.**
Nothing stops a host from granting `.myapp/users/alice` instead of `.myapp/` — the engine cannot tell, and
that is the design: an app never learns what a user is, so it cannot leak across one. But the guarantee is
the *host's*. A host that grants the wrong person's directory is undetectable from inside the engine, and no
engine-side check is possible even in principle. That is consistent with the capability model — the host
*is* the environment — and it is worth stating rather than leaving "the filesystem enforces it" to sound
stronger than it is.

**A host SAYS whether it isolated the store, and `platform.Isolated()` is how an app asks.** The manifest
carries `Filesystem.isolation` (`UNSPECIFIED | SHARED | PER_USER`), and it exists for one kind of app: one
that holds **private** data and needs to know whether privacy is its own problem. A per-user root means
everything visible belongs to one person; a shared root means access control is the app's responsibility,
and an app that assumed otherwise leaks quietly.

**Unset means no claim, and reads as NOT isolated.** That is why the field cost no host a change: silence is
never a promise of privacy, so an app that requires it refuses to run rather than assuming it, and a host
that forgot to declare fails loudly instead of exposing data. Contrast `FilesystemKind`, which `Boot`
*refuses* when unset — there the wrong guess points `reset-fs` at a user's directory, so there is no safe
default to fall back on.

**It is the host's word, not a boundary.** A host that grants a shared root and reports `PER_USER` is lying,
and nothing engine-side can detect it. Use it to learn what an honest host is offering; never as the thing
that enforces privacy.

**A host may keep its own state, and it must keep it outside the granted root.** Who is driving, what they
last selected, anything remembered between sessions: host-owned, app-invisible. Natively that is free
(`~/.config/<app>/`). In a browser the host and engine share one OPFS, so `dlc-platform/web/opfs.ts`
reserves the top-level `.ilc-host` prefix and skips it on **both** hydrate and flush — the flush being the
half that matters, since it mirrors and would otherwise delete state the engine never hydrated.

## 3·6 The environment manifest is PUSHED, and the order is load-bearing

**A host states what it can do before it does anything else** — `platform.Boot` natively, the worker's boot
sequence on the web. The manifest is a command (`SetEnvironment`, id 2), not a query: an import would be a
second boundary, and on the web a synchronous one that could not await an OPFS probe anyway.

**Ordering is CORRECTNESS, not convention.** An app on `RegisterDiscovered` registers its capability verbs
FROM the manifest, so a command sent first meets a half-registered engine and answers `unknown method_id`.
Every in-process caller is a host — a test, a golden-tree generator and the parity runner each learned this
by breaking. Call `platform.Boot` rather than re-deriving the sequence
([`docs/ENVIRONMENT-PLAN.md`](docs/ENVIRONMENT-PLAN.md) §2.5).

**Re-send whenever a fact changes, with a NEW revision.** The engine treats a repeated revision as a
deliberate no-op, because applying re-runs registration and would rebuild the command surface underneath a
host that only repeated itself. So a host that reuses a number silently fails to update the facts and never
learns it. The host owns that counter; nothing else may assign it.

**Absence is reported, never inferred.** `Availability` distinguishes "nobody said" from "there is none",
and both read as unavailable — the conservative direction. An app branches on what it must DO, never on
who is listening (§3, Decision 33): a filesystem either accepts a write or does not, whereas emitting
carries nothing back.

**A capability can come BACK.** Marking, unregistering and rendering all have to be reversible, or a
browser that regains a storage grant is stuck with a surface that no longer matches it.

**Parity cannot see registration.** It compares results, so two tiers offering different commands both pass
— demonstrated twice, once with every filesystem verb missing on both sides and the run still green. That
is why `GetCommandSurface` exists and why the surface is a parity vector. When you add a capability, add it
to the surface vectors too.

## 3·7 The derived index is DERIVED — three rules that are easy to break quietly

The index (§6.2, `dlc-platform/index`) is a projection the engine owns, stored behind a
`wasi:keyvalue`-shaped seam. It is not a capability, it cannot be absent, and app code never branches on
it. What follows is what makes "derived" true rather than aspirational.

**1. The index is NEVER authoritative.** A query returns identifiers and ordering; the record itself is
read from its own file. The moment a handler renders a *value* out of the index, the index is a second
source of truth and a stale row reaches a user. Where a list view genuinely needs values, the projection is
a CACHE of exactly what that view renders — widen it deliberately, never with fields a stale value could
mislead someone about. notes makes this structural: `ListRecordsResponse` carries a projection with no body
field, so a list cannot serve a stale body even by accident.

**2. Write FILE first, INDEX second, EVENT last.** A subscriber that re-reads on the event must find both
already consistent. If the process dies in between, the truth is on disk with only the derived thing
behind — which is what `rebuild-index` repairs. Emitting first races every listener against the write it is
announcing.

**3. The index never travels.** It is excluded from `export-fs` bundles (`platform.IndexDir`, matched by
full path in `ReadTree`). Two stores that differ only in their index must not compare unequal, and a
restored bundle must not carry a projection built from someone else's files. Note it IS visible in a file
browser — it is a real file, and the view whose whole job is "the files are the truth" must not tell a
tidier story than the disk does.

**The invariant that replaces most of the machinery: the maintained index equals a rebuilt one.** Assert it
after every mutation test (`assertIndexMatchesFiles` in notes). It catches a create that forgot to index, a
delete that left a row, and a projection that drifted — with no second tier, no second backend, and no
golden file. If you add a mutating command, add that assertion to its test; it is the only check the index
really needs.

**One file per key, and that is a concurrency decision, not a layout preference.** A whole-file index made
`Put` a read-modify-write, so two native processes creating a record at once each read, added, and rewrote —
and one entry was silently lost. A file per key removes that with no lock: different keys are different
files, and the same key is last-write-wins on one small file, which is exactly what the records themselves
do. It also shrinks D10's write amplification instead of trading against it.

**Index keys ARE the filenames, and the platform validates them out loud.** `.dlc-index/records/buy-milk`,
not an encoded blob — the store stays readable, which is the same argument canonical JSON and slugged record
names already make. The price is that a key must be a legal filename *everywhere*, so `Put` refuses:
separators and Windows-illegal characters (`: * ? " < > |`), control bytes, a leading dot (reserved for
housekeeping, and what lets a stray `.DS_Store` be skipped), a trailing dot or space (Windows strips them
silently, so `a.` and `a` would become one file), reserved device names (`CON`, `NUL`, `COM1`… — extension
or not), anything over 255 bytes, and **any key differing from an existing one only by case**.

**That last rule is checked on every platform, including case-sensitive ones where it would work.** Windows
and macOS are both case-insensitive by default, so `a` and `A` would silently merge two records'
projections; a check that only fires on some machines lets an app pass on the developer's box and fail on a
user's, which is the failure this project writes checks to prevent. The cost is a directory listing per
`Put` — the same listing `Scan` already does — and it is worth it.

An app needing opaque names does not get a flag: it implements `Store`. Encoding is a storage decision, and
the seam is where storage decisions belong.

**`rebuild-index` is INHERITED at method_id 200**, and registration follows the APP, not the host: an app
gets the verb by calling `platform.SetIndexRebuilder`, and an app with no collection never sees it. Do not
add a manifest field for the index — there was one for an afternoon, and it was reverted because a field
reporting a fact nothing can vary is what `ENVIRONMENT-PLAN.md` D6 exists to prevent.

## 3·8 The web tier holds a SNAPSHOT, and two tabs is the case that proves it

The browser host hydrates all of OPFS into an in-memory tree at boot, runs commands against it, and mirrors
it back after a write. That mirror **prunes**: anything OPFS holds that the tree lacks is deleted
(`writeDir` in `opfs.ts`). One tab, that is invisible. Two tabs, it is data loss — a tab that hydrated
before another tab's write deletes that write on its next write. Measured: with the guard below removed,
two tabs creating one note each leave **one note on disk**.

**Three rules follow, and each was learned by a test rather than by reasoning.**

**Broadcast on the WRITE, not on an event.** A staleness signal built on engine events is silent for every
handler that writes without emitting — `dlc new` writes 35 files and emits nothing. Same argument `onFlush`
already makes for filesystem watchers, one origin wider.

**Flush only when the tree CHANGED** (`treeFingerprint`). The host flushes after every command because the
engine cannot say whether it wrote; without a change check, a second tab's list-on-load broadcasts and
invalidates the first tab for *reading*. A read must never invalidate anybody.

**A stale tab REFUSES commands, and the app reloads.** It cannot catch up in place: the component captured
its preopen at instantiation and cannot be rebound (`worker.ts`). Serving reads from a stale snapshot is not
a softer option — every `execute` flushes, so a read is a write as far as the disk is concerned.

`onExternalChange` is deliberately **not** part of `subscribe`: an app that re-reads on an engine event
reads its own tree, which is right for its own writes and wrong for someone else's.

**This is not sync.** Nothing merges.

**Cross-PROCESS is not the same hazard, and does not currently exist.** It was worth checking rather than
assuming: the only host holding a snapshot is the browser, whose store is `navigator.storage.getDirectory()`
— OPFS, browser-private, unreachable from any other process. The native host holds no snapshot at all
(plain `os` straight through to the granted root), so a second CLI process always reads current disk. The
snapshot-clobber therefore cannot cross a process. It **would** the day the web tier accepts an
FSA-granted real directory, which the plan describes and the code does not implement.

## 3·9 Windows is a supported native target

**Above `SafeJoin`, every path is "/"-separated.** App-relative paths are the engine's currency: they go
into BFT bundles, into error messages the parity check compares, and into an app's own layout, and all of
those must read identically on every tier. `filepath.Join` would emit `records\a.json` on Windows and a
bundle exported there would stop matching one exported on Linux — the cross-tier interchange claim would
quietly become false. Use `platform.JoinPath` for root-relative paths; conversion to the host separator
happens once, at the bottom, in `SafeJoin`.

**A filename is not a string.** Before turning any app-supplied value into a filename, remember Windows is
case-insensitive, rejects `: * ? " < > |`, reserves `CON`/`PRN`/`AUX`/`NUL`/`COM1`-`LPT9` whatever the
extension, and silently strips a trailing dot or space. The index validates keys against exactly that list
(§3·7) and refuses uniformly on every platform. Anything else facing the same problem should reuse those
rules rather than inventing a second, subtly different set.

**Two checks, and they cover different things.** `ci.sh` cross-builds `GOOS=windows` on every push, which
catches a unix-only import for the price of a compile. Only the `windows` job in the GitHub workflow catches
BEHAVIOUR — separators, case-insensitivity, reserved names — because those need the code to actually run. A
cross-build going green proves nothing about the filesystem.

**No library, deliberately.** `gofrs/flock` and `natefinch/atomic` are the sane choices if locking or atomic
replace is ever genuinely needed, and both would be native-host-only — but the engine compiles to wasm and
embedded, where neither exists. Prefer a design that needs no platform API: one file per key beat a
cross-platform lock, and it was less code.

**Developing this repo on Windows is a different question** and the answer today is WSL: devbox is nix-based
and does not run natively there. What is supported is the native tier an app SHIPS.

## 3a. The host layer

**`hosts/` splits the same way `engine/` does** (Decision 34): inherited **host runtime** (`hosts/web/` —
`@devalbo/dlc-web`) versus an app's **tier slot**, `hosts/<tier>/`, holding that app's presentation and
input for one tier. Every tier in `dlc.toml` names its slot as `root`, and `dlc` refuses a manifest whose
slot is missing — the one field that file actually gates.

**A tier slot renders; it never decides.** Parity compares command results, the written filesystem, and the
event stream — all engine-side — so a slot is invisible to it *by construction*. Two hosts that each
compute the same conclusion will eventually disagree on one tier only, with every check still green. The
engine decides `winner`; a slot may highlight the line the engine named and may not find one.

**`hosts/` in this repo holds `dlc`'s own slots and nothing else**, since the runtime moved out to
`dlc-platform` (§16.4). `dlc`'s web slot is `hosts/web/`, the same name every app uses — it was `frontend/`
while that name was taken.

## 3b. The command surface is generated, not hand-written

**A native host does not `switch args[0]`.** `protoc-gen-dlc-registry` emits a `clispec` surface from the
`.proto` — rpcs become subcommands, request fields become flags — and `platform/cli` turns that into a
parser. A hand-written switch is a second place for the command surface to live and a second place for it
to be wrong.

**What a host still writes is presentation only:** a `Render` per method, and `Fill` for values the user
should not type (the clock). Both are the tier slot's business (§3a).

**The CLI spelling is declarable in the schema** — `(cli_name)` on an rpc, `(cli_flag)` / `(cli_source)` /
`help` / `required` / `default` / `short` on a field, and an rpc's **doc comment becomes its `-h`
summary**. All cosmetic: dispatch is on `method_id` and encoding is by field *number*, so renaming a
command or flag is not a breaking change and never touches the id lock.

**`cli_source` exists because a value's SOURCE is not its type.** `bytes` is almost always a file, a long
string may be piped; declaring it means every host resolves values identically instead of each inventing
an `@file` convention. It is why an inherited `import-fs` is usable from a command line without the app
hand-writing a file read.

**Parsing divergence is invisible to parity.** Parity compares command results, written filesystems and
event streams — all *downstream* of a request that already exists. Two tiers that turn the same argv into
different requests are each internally consistent and every check stays green. So the invariant to protect
is **`argv → request bytes`**, not "one parser": keep the semantic half (`cli/encode.go` — required,
defaults, enum names, sources, encoding) portable and shared, and let only argv tokenizing differ.

**Generated code may only import leaf packages.** It lives in the message package, which `platform`
imports, so anything it references must not reach back — hence `clispec` (data, no imports) being separate
from `platform/cli` (the runner, which pulls in ffcli and `google.golang.org/protobuf` and must never
enter the engine's TinyGo build).

## 4. Templates

**Every file under `templates/` ends in `.tmpl` (substituted) or `.raw` (verbatim).** Without a suffix:
a file named `go.mod` makes the directory a nested module and `go:embed` refuses it; `.go` files are not
valid Go and break `go build ./...`, `go vet`, and gopls; and `gofmt -l .` reports parse errors. An
`_`-prefixed directory is **not** sufficient — it hides files from the build but not from gofmt.

**Tokens come from the command.** `scaffoldVars()` maps `NewRequest` fields to template variables; an
unknown token is a render-time error, not a passthrough.

**Templates are embedded in the binary** — editing one requires rebuilding `dlc` before it takes effect.

## 5. Verification

**Every claim needs a check, and every check needs to be falsifiable.** `verify-parity-selftest.sh`
exists because a parity check nobody has seen fail is indistinguishable from a broken one. When you add a
check, break something on purpose and confirm it goes red.

**Prefer invariants to exhaustive lists.** Pinning every scaffold path made three consecutive template
additions look like regressions. Assert what must be true.

**`./scripts/ci.sh full` is what CI runs and what you run.** Nothing in the suites may know about a CI
provider; `.github/workflows/ci.yml` is a thin adapter.

## 6. Working style

**Run the toolchain through devbox** — `devbox run -- make …`, or work inside `devbox shell`.

**Do not run `git` in this repo.** The maintainer runs all git commands. Suggest them instead.

**Never `go build ./cmd/x` bare.** Go writes the executable into the current directory, named after the
package folder — that is how `engine.test`, `protoc-gen-dlc-registry`, and `native` each turned up at the
repo root, two of them inside a commit. Use `go build -o "$(mktemp -d)/x" ./cmd/x`, or `go vet` when you
only want to know it compiles. The `.gitignore` has an entry per main package as a backstop, but the
backstop only works for names someone remembered to add.

**Generated code is not committed** (`/gen/` is ignored) — with ONE exception: **`dlc-platform/gen/` is
committed**, because a consumer of that module cannot run `buf` or `wit-bindgen`. The **id locks are
committed too**, and there are now two — `dlc-platform/proto/method-ids.lock` for the framework band
(1–9999) and `proto/method-ids.lock` for dlc's own app ids.

**Anything a scaffolded app needs belongs in `dlc-platform`.** An app's MODULE GRAPH contains the platform
and not `dlc` — `verify-scaffold.sh` asserts exactly that with `go list -m all`, and builds the scaffold
with `GOPROXY=off` so a dependency that is only satisfiable by fetching cannot hide. If you find yourself
adding a `devalbo-ilc` import to the template, the thing you are importing is in the wrong module.

**Read that claim precisely:** an app still needs the `dlc` BINARY as a build tool (`make gen` runs
`dlc gen`; `dlc build web` supplies the WIT world). What it does not need is the `dlc` Go module.

**`dlc-platform/gen/` is checked by `verify-platform-gen.sh`** for both ways it rots — stale bytes, and
files that exist locally but were never committed. Neither is visible from inside this repo, because we
always regenerate; both ship broken to anyone who does not.
