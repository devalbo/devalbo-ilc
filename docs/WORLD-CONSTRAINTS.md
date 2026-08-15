# World constraints — what an app can rely on, per world

**Status: LIVING DOCUMENT.** The specification lives in `dlc-platform/names/`: `RULES.json` (the rules),
`WORLDS.tsv` (the registry), and `VECTORS.tsv` / `COLLISIONS.tsv` (what those rules must mean).
**This document explains them; it does not define them.** When the two disagree, the files win and this page
is the bug.

**The Go and Rust validators are GENERATED from `RULES.json`** by `cmd/gen-names`, so they cannot disagree
about a character class, a limit, or a reserved name — the drift the vectors used to *detect* is now
impossible. The vectors still run, because they answer a different question: codegen makes the two
implementations identical, and cannot make them correct.

Written for someone building an ILC app who needs to know what will still be true on a tier they have not
run on yet.

---

## 0. The one-paragraph version

Your business logic runs unchanged on every tier. **What differs is what the host can do for you**, and the
range is wider than it looks: a laptop has a terminal, a real filesystem and gigabytes; a badge has one
serial line, 520 KB of SRAM, and a screen it may not be showing text on. The rules below are the intersection
— follow them and you never find out which host you got. Where you need to know, the host **tells you**
(§4), rather than leaving you to guess from what happens to work.

**The failure mode this document exists to prevent is not a crash.** It is a file that quietly has a
different name than the one you asked for, a save that silently overwrote another save, or output nobody
ever sees. Those do not throw. They are found by someone plugging a badge into a Windows machine six months
later.

---

## 1. What a world is

A **world** is a host slot — a place an app runs. A **profile** is a rule set (filenames, limits). They are
not the same axis: several worlds share one profile, which is the entire point of having profiles.

**These are not WIT worlds.** A WIT world is a component's import/export set, and forking one would fork the
artifact — the constraint the whole architecture is built to preserve. Every world below instantiates the
**same component with the same imports**. What differs is what the host does with them.

| world | what it is | name profile |
| --- | --- | --- |
| `native` | CLI / desktop: a terminal, a real filesystem | portable |
| `browser` | a DOM slot and OPFS | portable |
| `badge-normal` | RP2350 badge showing your text | fat |
| `badge-minimal` | RP2350 badge showing one status colour | fat |

### The two non-worlds, which are different

| | meaning | when you see it |
| --- | --- | --- |
| `undefined` | **Nobody declared one.** The default and the common case — an app that runs everywhere has no reason to name a world. | almost always |
| `unknown` | **Declared, and not recognised.** Something was said and could not be understood. | an app or payload built against a newer registry meeting an older host |

Collapsing these loses real information: absence and incomprehension are different facts, and only one of
them means somebody meant something. The badge makes `unknown` routine rather than exotic — **payloads
outlive the firmware that reads them**, because a payload is dragged onto a board whose firmware was flashed
months earlier.

**Both fail closed** onto the strictest profile. A host *asked* for a world it does not recognise must
refuse rather than substitute one: guessing is how an app written for a richer world silently loses its
output.

---

## 2. The constraint matrix

Measured where a number appears. `hello` is the reference app throughout.

| | `native` | `browser` | `badge-normal` | `badge-minimal` |
| --- | --- | --- | --- | --- |
| **your text is shown** | ✅ terminal | ✅ DOM | ✅ UART (screen: planned) | ❌ **never** |
| **status colour** | — | — | ✅ | ✅ |
| **filesystem** | real | OPFS | RAM now, FAT planned | RAM now, FAT planned |
| **files visible to a person** | yes | no | **yes, over USB** | yes, over USB |
| **case-sensitive names** | depends on host FS | yes | **no** | no |
| **persistence across restart** | yes | yes | not yet | not yet |
| **wall clock** | real | real | **epoch zero** — no RTC wired | epoch zero |
| **monotonic clock** | real | real | tick counter | tick counter |
| **randomness** | OS | OS | **xorshift, not entropy** | xorshift |
| **stdin** | yes | no | **closed** | closed |
| **memory for your app** | GBs | hundreds of MB | ~5 MB of PSRAM after the runtime | same |
| **execution** | native / JIT | JIT | **Pulley interpreter** | Pulley |

### The badge's numbers, since they are the binding ones

Measured under QEMU at the badge's pointer width (`EMBEDDED-PLAN` Phase 0d):

| | |
| --- | --- |
| SRAM / PSRAM / flash | 520 KB / 8 MB / 16 MB |
| loading a component | **81 KB** — the artifact stays in flash, borrowed not copied |
| instantiating it | **2911 KB** — 2048 KB guest linear memory + **863 KB of runtime structures** |
| firmware partition | 4 MB (`0x10000000`) |
| payload region | 12 MB (`0x10400000`) |
| payloads per badge | **16** (`catalog::MAX_ENTRIES`) |
| `hello` as a component / `.cwasm` | 1.48 MB / 869 KB |

**863 KB is the decisive figure.** Wasmtime's own structures — outside your app's memory entirely — already
exceed the RP2350's SRAM. No amount of shrinking your app changes that, which is why PSRAM is a
prerequisite rather than an optimisation, and why "make the app smaller" is rarely the useful lever.

---

## 3. Rules for app developers

### 3.1 Names and paths

**Lowercase `a-z`, digits, `-`, `_`, `.`. Relative, `/`-separated. Nothing else.**

```
✅  save.json        logs/day-1.txt      board-state       2026-08-15.log
❌  Save.json        my save.json        .hidden           save.
❌  /etc/passwd      a/../b              con.txt           café.txt
```

Each rejection is a rule with a reason:

- **No uppercase.** FAT and Windows are case-**in**sensitive; Linux is case-sensitive; macOS APFS is
  case-insensitive by default but can be formatted either way. So an app that creates `Save.json` and
  `save.json` works on a laptop and **silently loses one on a badge**. "Use lowercase" is a rule you can
  follow; "never create two names differing only in case" is one you cannot check locally.
- **No leading `.` or trailing `.`/`-`.** Windows silently *strips* some of these — the name that comes back
  is not the name that went in.
- **No Windows device names** (`con`, `nul`, `com1`…`lpt9`, with or without an extension). Only matters
  because the badge's files get read on a PC, which is exactly the kind of constraint that stays invisible
  until someone plugs the thing in.
- **Relative only.** A tier decides where your files live. An absolute path is a claim you are not entitled
  to make — and `platform.SafeJoin` enforces the containment this only describes.

**This is checked, not hoped for:** `platform.CheckNamePath` in Go, `names::check_path` in Rust — both
generated from the same `RULES.json`, and both tested against `VECTORS.tsv`.

> **Where the rule came from.** A payload called `hello.pulley32` mounted as `HELLO.PU.CWA` — an 8.3
> directory entry has no dot, the extension is the last three bytes positionally. Nothing failed. The name
> was just wrong, and would have stayed wrong until somebody noticed.

### 3.2 Do not assume anyone reads your output

Every world **provides** `wasi:cli/stdout` — it must, because TinyGo acquires it during `_initialize` and a
component whose stdout is missing never instantiates. **So its presence tells you nothing.** On
`badge-minimal` the bytes go nowhere a person can see.

Check the advertisement (§4). If text will not be read, **emit an event instead** — a semantic event is
rendered by whatever the tier can do, which on a badge is a colour and in a browser is a whole DOM.

### 3.3 Do not assume time or randomness

The badge's wall clock is **epoch zero** until an RTC is wired, and its randomness is **xorshift** standing in
for a hardware RNG. Neither is a placeholder that will fail loudly — both return plausible values.

- Never use `wasi:random` for anything security-bearing on any tier without checking what you got.
- Never persist a wall-clock timestamp as an identity or an ordering key. Use a monotonic counter you own.

### 3.4 Do not assume persistence

The badge has **no filesystem granted today**. A command that persists fails with an app-level error naming
the operation (`mkdir /: errno 2`) — not a trap, not a wrong answer. That is the capability model working:
an absent capability degrades, it does not lie.

### 3.5 Method ids

**Yours start at 10000.** 1–9999 is reserved for ILC, including capabilities not yet shipped. Ids are
generated from your proto and locked; changing one is a breaking change that `buf breaking` cannot see.

### 3.6 Stay TinyGo-safe

No `encoding/json`, no `text/template`, no `reflect`. Not a style preference — a reflection-heavy dependency
does not merely bloat, it **fails to build** for wasm. Generics are fine.

---

## 4. How a world tells you what it is

Through **`wasi:cli/environment`** — an interface every component already imports, so no new capability is
needed for a fact an existing channel can carry.

```
ILC_TIER=rp2350
ILC_WORLD=normal
ILC_STDOUT=uart        # or `none` — the signal to emit an event instead
ILC_STATUS=color
```

**Values say what is true today, never what is planned.** `ILC_STDOUT` becomes `display` when the TFT
actually renders text and not before: an advertisement that runs ahead of the hardware is the one kind of lie
this channel makes expensive, because it is silent and believed.

**Capabilities nest.** `badge-minimal`'s set is a strict *prefix* of `badge-normal`'s, checked by a `const`
assertion that fails the build. So "trim a world down" means dropping capabilities off the end and landing on
a world that already exists — not inventing a variant with its own quirks.

---

## 5. Validators

### 5.1 Implemented

| validator | where | enforced at |
| --- | --- | --- |
| **portable name/path profile** | `names.go`, `names.rs`, `VECTORS.tsv` | `payload-image` build time; `wasi:filesystem` planned |
| **world registry** incl. `undefined`/`unknown` | `WORLDS.tsv` + both implementations | parse time, fails closed |
| **method id lock** | `make gen` | codegen; `DLC_ID_LOCK_UPDATE=1` to re-bless |
| **UF2 family gate** | `make badge-uf2` | build — a wrong family boots nothing and says nothing |
| **payload compiler match** | `deserialize_raw` | load time — "compilation settings are not compatible" |
| **catalog bounds** | `catalog::scan` | boot — truncated or blank flash yields fewer entries, never a fault |
| **set-level name collisions** | `CheckNameSet` / `check_set`, `COLLISIONS.tsv` | `payload-image` build time, exit 2 |
| **generated rules are current** | `cmd/gen-names -check` | `go test ./cmd/...`, `make verify-names` |

### 5.2 Candidates worth considering

Ordered by **how badly the absence bites**, not by effort. Nothing here is built.

#### Tier 1 — silent data loss

1. ~~**Case-collision across an app's file set.**~~ **BUILT** — and it found a live bug rather than a
   hypothetical one. See §5.3.
2. **Storage quota / file count per world.** The badge's region is 12 MB with 16 catalog slots; nothing
   tells an app it is about to exceed either. Failure today is a write that fails late, or a catalog whose
   tail is silently unreachable.
3. **Max file size.** No tier states one. A 900 KB save is fine on a laptop and competes with payloads on a
   badge.
4. ~~**Duplicate payload names in a catalog**~~ — **BUILT**, subsumed by §5.3: identical names are a
   case-fold collision, and near-identical ones are the more dangerous short-name case.

#### Tier 2 — output nobody sees

5. **Event topic naming.** Topics are strings crossing the boundary and get rendered as UI labels; they
   deserve the same profile as filenames (lowercase, bounded length). Currently unvalidated.
6. **Event payload size.** Decision 33 chose flat scalars + bytes so the import lowers to a pointer/length
   pair. Nothing bounds the length, and a badge collecting events into a `Vec` has 5 MB.
7. **Command request/response size.** `execute(u32, list<u8>)` is unbounded by its signature.
8. **Text encoding.** UTF-8 is guaranteed by the type; **renderability is not**. A badge with a bitmap font
   shows ASCII. An app emitting `café` gets mojibake or blanks, on the tier least able to report it.

#### Tier 3 — fits and budgets

9. **Memory budget per tier.** An app's instantiated cost is measurable (2911 KB for `hello`); a validator
   could refuse to build a payload for a tier it cannot fit, at build time, on a laptop — rather than
   discovering it over a UART.
10. **Artifact size against the region.** A `.cwasm` larger than a catalog slot's remaining space.
11. **Pointer-width / profile match.** `pulley32` vs `pulley64` fails loudly at load; catching it in
    `payload-image` would fail *earlier*, where a human is present.

#### Tier 4 — semantics that differ quietly

12. **Filesystem semantics an app must not assume**: no symlinks, no permission bits, and **no atomic
    rename on FAT** — the write-temp-then-rename idiom is not crash-safe there. Worth a documented lint.
13. **Power-loss safety.** FAT is not power-safe and littlefs was (D11 traded it away for PC visibility).
    An app doing multi-file updates has no transaction to lean on.
14. **Foreign files.** A PC writes `.DS_Store`, `.Spotlight-V100`, `._name` onto any volume it touches. An
    app enumerating its own directory **will** see files it did not create, on the badge specifically.
15. **Blocking capabilities.** The badge's executor spins and is only sound because every pollable is
    *always ready*. The day a genuinely blocking capability arrives, this becomes firmware that hangs with
    no clue why — a validator that no registered capability can block would keep that honest.
16. **Determinism.** Parity vectors already catch cross-tier disagreement; a narrower check for the usual
    causes (map iteration order, time dependence) would localise a failure faster.

### 5.3 Set-level collisions — the bug a per-name check cannot see

Every rule in §3.1 judges **one** name. Two names can each be perfectly legal and still be unable to
coexist, and the host does not report it: one file becomes the other, or shadows it in a listing.

```
board-state-1  +  board-state-2   ->  short-name collision
save.json      +  Save.json       ->  case-fold collision
a.b            +  a_b             ->  short-name collision
```

**The truncation case was live, not theoretical.** The badge's USB volume renders a catalog name as its
first EIGHT characters, so `board-state-1` and `board-state-2` both appear as `BOARD-ST` — two payloads,
one filename, the second unreachable through the drive. Nothing fails; a payload is simply missing.

Two things make this validator trustworthy rather than decorative:

- **It calls the renderer's own truncation.** `short_stem_83` is shared: `fatview` builds directory entries
  with it and the validator predicts collisions with it. A validator that reimplemented the rule would be
  checking something nothing does, which is worse than not checking at all.
- **It names both entries**, not just "invalid" — a user needs to know which two, to rename one.

Note the third example: **sanitisation can create a collision the original names did not have.** `a.b` and
`a_b` are distinct until the dot is replaced. That is why comparing the input strings is not enough.

Enforced in `payload-image` (exit 2) because that is the last point a human is present to retype an
argument. The same check applies to an app's own file set once the FAT data volume lands (D11).

---

## 6. What this document does not cover

- **Path containment** — that is `SafeJoin`, and it refuses an *attack*. Everything here refuses a
  *portability bug*. You want both; they fail differently.
- **The WIT world / capability list** — see `AGENTS.md` §2–3 and Decision 33.
- **Why the badge runs Pulley at all** — `docs/EMBEDDED-PLAN.md`.
