# Packaging `dlc` for generated projects

**Status: PLAN (2026-07-29).** Nothing built. Written in the shape of
[`ENVIRONMENT-PLAN.md`](./ENVIRONMENT-PLAN.md) / [`INDEX-PLAN.md`](./INDEX-PLAN.md): decisions first,
phases that each leave the tree green, and nothing claimed until a check can fail.

**Goal:** a scaffolded app’s `devbox.json` provisions **`dlc` itself**, so `make gen` / `make build-web`
work inside that project’s shell without a one-time `go build -o ~/.local/bin/dlc` from this repo
([`INSTALL-DLC-STEPS.md`](./example-tutorials/INSTALL-DLC-STEPS.md) §5–§6).

---

## 1. Why now

Today the install story is two environments that do not meet:

| Layer | What it provides | Gap |
| --- | --- | --- |
| Framework repo `devbox.json` | Go, TinyGo, buf, Node, wasm tools, plugins | Builds `dlc`; does not *ship* it |
| Scaffolded app `devbox.json` | Go, buf, Node, plugins | Assumes `dlc` is already on `PATH` |

That assumption is honest for dogfooding and wrong for anyone following the tutorials: “install once”
still means cloning this tree forever ([INSTALL §6](./example-tutorials/INSTALL-DLC-STEPS.md)), and
every `dlc gen` fails mysteriously if the binary is missing or stale.

The destination is already written into the product story: **`dlc` is a build tool**, like `buf` — a
dependency of every app’s Makefile, not a Go `require` of the app module (scaffolds must depend on
`dlc-platform`, never on `devalbo-ilc` — checked by `verify-scaffold.sh`).

---

## 2. What “supported in the project’s `devbox.json`” means

Not “listed on Nixhub tomorrow.” Three nested claims, in order of usefulness:

1. **`devbox shell` in a scaffolded app puts `dlc` on `PATH`** — same quality as `buf` today.
2. **That `dlc` is version-pinned** — bumping the pin is deliberate; template edits do not silently
   change old apps until they upgrade.
3. **No `--platform-path` / local `replace` / `file:` deps** — apps resolve `dlc-platform` and
   `@devalbo/dlc-web` from the network. Claim 3 is a **prerequisite for claim 1 to be enough**; without
   it, having `dlc` on PATH still strands the app on a laptop checkout.

This plan delivers **1 + 2** via a flake first; **3** is the publish follow-up already named in
[`DEVALBO-DLC-GO-TASKS.md`](./DEVALBO-DLC-GO-TASKS.md) (§ Platform & tooling). Nixpkgs/Nixhub is a later
polish of 1+2, not a gate.

---

## 3. Design decisions

### D1 — Near-term vehicle is a **Nix flake**, not Nixpkgs

Devbox can install flake packages today
([flakes guide](https://www.jetify.com/docs/devbox/guides/using-flakes)):

```json
"packages": [
  "github:devalbo/devalbo-ilc/<ref>#dlc",
  "go@latest",
  "buf@latest",
  "nodejs@24"
]
```

There is **no separate Devbox registry to submit to** — [Nixhub](https://www.nixhub.io) indexes
Hydra builds of [Nixpkgs](https://github.com/NixOS/nixpkgs). Getting `dlc` into `devbox search` means a
Nixpkgs PR → unstable → daily index (days to weeks). That is Phase 5. Phase 1 does not wait on it.

### D2 — The flake builds the **same binary** `make build-host` builds

`go build -o dlc ./hosts/native` with templates `go:embed`’d (Decision 26, in-process engine). No second
codepath, no stripped “toolchain-only” fork. If the flake and `make build-host` disagree, the flake is
wrong.

**Binary name is part of the package contract.** `go install ./hosts/native` yields `native`
([INSTALL §3](./example-tutorials/INSTALL-DLC-STEPS.md)). The flake’s `pname` / install name is
`dlc`. Optionally later: a `cmd/dlc` main package so `go install` matches — nice, not required for D1.

### D3 — Pin a **git ref** in the scaffold, never `@latest` of the flake tip

A scaffolded `devbox.json` records something like:

```text
github:devalbo/devalbo-ilc/v0.x.y#dlc
```

or a full commit. `devbox.lock` then freezes the flake input. Upgrading the framework for an app is
editing that pin (and re-running `devbox update` / lock), not hoping tip moved.

**Template regeneration** (`make scaffold-golden`) must see the pin as content of `devbox.json` — same
as the go-lite banner lesson: unpinned generators make T-B2.4 / T-B2.6 lie.

### D4 — Publishing `dlc-platform` (and `@devalbo/dlc-web`) is a **hard prerequisite** for dropping `--platform-path`

The flake can ship `dlc` while apps still use `replace` + `file:` — that is an intermediate state
(Phase 2). The install tutorial’s “do not delete the repository” warning only ends when:

- `github.com/devalbo/devalbo-ilc/dlc-platform` is a tagged, proxy-fetchable module (committed `gen/` travels with it);
- `@devalbo/dlc-web` is on npm (or an equivalent registry);
- the template stops writing `PlatformPath` / `replace` / `file:` (tasks already sketch vendoring
  `options.proto` vs BSR — prefer vendoring; offline codegen stays).

Until then, document the intermediate honestly: **devbox provides `dlc`; you still pass `--platform-path`
once per machine** (or once per clone).

### D5 — Apps do **not** `require` the `devalbo-ilc` Go module

`dlc` on PATH is a **host tool**. The app’s `go.mod` continues to depend only on `dlc-platform`.
`verify-scaffold.sh` keeps asserting that. Packaging must not collapse the two modules to make install
easier — that would teach every scaffold the wrong graph.

### D6 — TinyGo / jco / wasm-tools stay in the **app** (or framework) shell as needed

`dlc build web` shells out to / expects a wasm toolchain. Packaging `dlc` does not mean stuffing TinyGo
into the same flake derivation on day one. Options, in order of preference:

1. Keep TinyGo + friends in the **scaffolded** `devbox.json` (and/or document `dlc build web` requires
   them) — mirrors today’s split.
2. Later: a `dlc-web` flake output that is a wrapped env, if one derivation proves painful.

Phase 1 only guarantees `dlc` itself.

### D7 — Release tags are the unit of trust

Cut annotated tags (`v0.1.0`, …) that mean: this commit’s `dlc` binary + embedded templates + the
platform module that tag’s apps should use. The flake ref and (later) the Go module version should be
the **same tag story**, even if `dlc-platform` lives in a nested path or a moved repo.

---

## 4. Current state (baseline)

| Fact | Implication |
| --- | --- |
| `make build-host` → `./dlc` from `./hosts/native` | Flake build target is known |
| Templates `go:embed`’d into the engine / binary | Flake `src` must be the full repo (or a release tarball that includes `templates/`) |
| `dlc-platform` path is already `github.com/devalbo/devalbo-ilc/dlc-platform` but unpublished | `replace` / `--platform-path` remain until Phase 3 |
| Scaffold `devbox.json` has no `dlc` | Phase 2 edits the template |
| Install tutorial teaches global `~/.local/bin/dlc` | Remains valid as a fallback; becomes optional after Phase 2 |
| Nixhub = Nixpkgs index | Phase 5 only |

---

## 5. Phases

Each phase ends green under existing suites plus the new checks it names. No phase claims the next.

### Phase 0 — Inventory and naming (docs + layout only)

**Do:**

- Decide the flake output name (`packages.dlc` / `apps.dlc`).
- Decide whether `cmd/dlc` (rename for `go install`) is in scope for Phase 1 or deferred (D2).
- Document in this file the exact `devbox.json` line the template will grow (D3).
- Wire a tasks checkbox under Platform & tooling pointing here.

**Exit:** this plan accepted; no runtime change. `make test-b0` still green.

### Phase 1 — Flake builds `dlc` 🟢 gate

**Do:**

- Add root `flake.nix` (and `flake.lock`) that:

  - uses a pinned `nixpkgs`;
  - builds `dlc` with the project’s Go version (or nixpkgs go matching `go.mod`);
  - sets install name to `dlc`;
  - includes `templates/` in `src` so embed works;
  - does **not** need network at runtime for templates.

- CI job (or `scripts/verify-dlc-flake.sh`): `nix build .#dlc` (or `devbox run` equivalent) and
  `./result/bin/dlc version` / `dlc --help` smoke.
- Optionally: `nix run .#dlc -- new …` smoke against a temp dir **with** `--platform-path` still
  pointing at the checkout (publish not required yet).

**Exit:**

- `nix build .#dlc` (documented command) produces a working binary on the CI platforms we care about
  (at least `x86_64-linux` for Actions; document darwin as best-effort until green).
- Byte-identical policy is **not** required vs `make build-host` (different toolchains); **behavior**
  smoke is required (`new` + `gen` on a throwaway app with local platform path).

### Phase 2 — Scaffold `devbox.json` installs the flake 🟢 product

**Do:**

- Change `templates/component-model/devbox.json.tmpl` to include the pinned flake ref (D3).
- Re-bless `verify/scaffold/golden.txt`.
- Update [`INSTALL-DLC-STEPS.md`](./example-tutorials/INSTALL-DLC-STEPS.md):

  - preferred path: clone nothing for day-to-day app work — `devbox shell` in the app gets `dlc`;
  - framework contributors still build from source;
  - **still** document `--platform-path` until Phase 3.

- Update tic-tac-toe / other tutorials that say “install dlc first” to “enter the project’s
  `devbox shell`” where accurate.

**Exit:**

- `verify-scaffold.sh` + golden green.
- Fresh scaffold: `devbox shell` → `command -v dlc` → `make gen` works **without** a pre-existing
  `~/.local/bin/dlc` (CI must unset/hide any ambient `dlc` for this check).

### Phase 3 — Publish platform modules (claim 3) 🟢 install-once

**Do:** (already sketched in tasks — this phase is the packaging gate for that work)

- Publish `dlc-platform` (tag; nested module or moved repo).
- Publish `@devalbo/dlc-web`.
- Template: drop `PlatformPath` / `replace` / `file:`; `dlc new` no longer requires `--platform-path`.
- Install tutorial §6 gap #1 closes.

**Exit:**

- Scaffold builds with `GOPROXY` on and **no** `replace` for `dlc-platform`.
- `npm install` resolves `@devalbo/dlc-web` without `file:`.
- Dogfood: delete the framework checkout; an existing app still `make gen` / `make verify` after pin
  bump instructions.

### Phase 4 — Align Go-module install with the flake (optional DX)

**Do:**

- Introduce `cmd/dlc` (or equivalent) so `go install github.com/devalbo/devalbo-ilc/cmd/dlc@vX` yields
  `dlc`, for contributors who prefer Go over Nix.
- Document both install paths; flake remains what the **scaffold** uses (D1).

**Exit:** `go install …@<tag>` smoke in CI; INSTALL tutorial offers it as an alternative, not the
scaffold default.

### Phase 5 — Nixpkgs / Nixhub (optional polish)

**Do:**

- Open a Nixpkgs PR packaging `dlc` (likely fetching release tarballs or building from tag).
- After merge + index lag, switch the scaffold pin from `github:devalbo/…#dlc` to `dlc@<version>` if
  that improves UX — **or keep the flake forever**; either is fine if D2/D3 hold.

**Exit:** `devbox search dlc` shows the version; scaffold may use either form.

---

## 6. Intermediate UX (after Phase 2, before Phase 3)

Be explicit so tutorials do not lie:

```text
devbox shell          # now provides: go, buf, node, plugins, AND dlc
dlc new myapp … --platform-path /path/to/devalbo-ilc   # still required
cd myapp && devbox shell && make gen
```

The remaining “framework checkout” is **only** for resolving unpublished modules, not for obtaining the
`dlc` binary.

---

## 7. Checks to add (do not invent until the phase)

| Check | Phase | Failure mode it catches |
| --- | --- | --- |
| `verify-dlc-flake.sh` — build + `--help` | 1 | flake bitrot / embed path wrong |
| Scaffold golden includes flake pin line | 2 | pin drift like go-lite `@latest` |
| `verify-scaffold` hides ambient `dlc` | 2 | false green from developer PATH |
| Scaffold `go list -m` has platform, not ilc | already | packaging must not regress D5 |
| No `replace` / no `file:` after publish | 3 | “published” in name only |

---

## 8. Explicit non-goals

- **Not** putting TinyGo inside the `dlc` derivation in Phase 1.
- **Not** making apps `require github.com/devalbo/devalbo-ilc` (D5).
- **Not** waiting on Nixpkgs to improve the scaffold (D1).
- **Not** runtime-`git clone` of templates (still `go:embed`; §16.6).
- **Not** a private Devbox plugin registry — flakes + Nixpkgs are enough.

---

## 9. Sequencing vs other work

```
Phase 0 (this plan)
    → Phase 1 (flake builds dlc)
        → Phase 2 (template + tutorials + golden)     ← first user-visible win
            → Phase 3 (publish dlc-platform + dlc-web) ← true install-once
                → Phase 4 / 5 optional
```

Phase 3 overlaps the existing “publish platform” task; this plan **does not replace** that task — it
makes packaging `dlc` the reason that task unblocks the install story.

Related:

- Install UX today: [`example-tutorials/INSTALL-DLC-STEPS.md`](./example-tutorials/INSTALL-DLC-STEPS.md)
- Platform publish follow-up: [`DEVALBO-DLC-GO-TASKS.md`](./DEVALBO-DLC-GO-TASKS.md) (Platform & tooling)
- Template / embed rules: plan §16.6; `templates/README.md`
- Devbox ↔ Nixpkgs reality: Nixhub indexes Hydra; contribute via Nixpkgs
  ([discussion](https://github.com/jetify-com/devbox/issues/2463))

---

## 10. First concrete PR after acceptance

1. `flake.nix` + CI smoke (`nix build .#dlc` / documented equivalent).
2. No template change yet — keeps Phase 1 reviewable without golden churn.
3. Follow-up PR: template pin + INSTALL rewrite + golden (Phase 2).
