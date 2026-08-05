# DEVALBO-DLC — Prerequisites & Getting Started

What a new **`dlc`** user/developer needs installed, and how to **assess** whether a machine is ready.
Companion to [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) (§4 toolchain) and
[`DEVALBO-DLC-GO-TASKS.md`](./DEVALBO-DLC-GO-TASKS.md).

_Created 2026-07-25._

---

## The short story

**Devbox provisions almost everything.** You install a *tiny* set of system tools by hand; then
`devbox shell` pins and provisions the rest (Go, TinyGo, wasm tooling, buf, wasmtime, Node/jco) at exact
versions. So the real prerequisite list is short:

> **Install yourself:** **Devbox** · git · a modern browser — **Devbox auto-installs Nix** on first
> `devbox shell`. (Optional: direnv.)
> **Everything else:** provisioned by `devbox.json` — do **not** install it manually.

This is scoped to the **bootstrap** (CLI + Web tiers). The embedded toolchain (PlatformIO, board SDKs) is
**not** needed yet — see [Later tiers](#later-tiers-not-needed-for-bootstrap).

---

## 1. System prerequisites (install these)

| Tool | Why | Install | Min |
| --- | --- | --- | --- |
| **git** | source control | OS package / Xcode CLT | any recent |
| **Devbox** (Jetify) | pins + provisions the toolchain; **auto-installs Nix** on first run | [installing-devbox](https://www.jetify.com/docs/devbox/installing-devbox) | recent |
| **A modern browser** | the Web tier (OPFS) | Chrome / Edge / Firefox / Safari (current) | OPFS-capable |
| direnv *(optional)* | **convenience only** — auto-activates the env on `cd` instead of `devbox shell` | OS package | — |

**You never install Nix or Node.js by hand.** Devbox installs **Nix** (its underlying engine) on first
`devbox shell`, and provides the **Node** used for jco/Vite. (A system Node is fine to have but isn't used
by the pinned build. If Devbox's Nix auto-install ever fails, the [Determinate
installer](https://install.determinate.systems/) is the fallback.)

**direnv is optional and not needed.** With it, `cd`-ing into the project auto-activates the Devbox
environment; without it, you just run `devbox shell`. Skip it unless you want that shortcut — the preflight
shows it as `○` (optional), not `✗`.

---

## 2. Provisioned by Devbox (do NOT install manually)

`devbox shell` (or `devbox run …`) makes these available at pinned versions:

| Tool | Role | Min / note |
| --- | --- | --- |
| **Go** (standard) | native hosts, wasmtime embedding | 1.22+ |
| **TinyGo** | engine → wasm (`-target=wasip1/2`) | **0.34+** (Component Model / WASI 0.2) |
| **wit-bindgen-go** | WIT → Go capability bindings | recent |
| **wasm-tools** | preview1 → component adapter | recent |
| **buf** + **protoc-gen-go-lite** + **protoc-gen-es-lite** | proto codegen (reflection-free) | recent |
| **wasmtime** | native/CLI runtime (Go embedding) | 25+ |
| **Node.js** + **jco** (`@bytecodealliance/jco`) | web instantiation + Vite/React; Spike 5 JSPI | **Node 24+** (`nodejs@24`; JSPI via `--experimental-wasm-jspi`) |

Pin every version in `devbox.json` (+ its lockfile). The **versions matter to whoever authors
`devbox.json`**, not to the end user — the user just runs `devbox shell`.

---

## 3. Platform support

**This section is about the machine you BUILD on.** Where the apps you build can RUN is a different
question — see the Windows note in [`README.md`](../README.md#windows-apps-ship-there-you-build-them-elsewhere).

- **macOS** (arm64 / x86_64) — fully supported.
- **Linux** (x86_64 / arm64) — fully supported.
- **Windows** — via **WSL2** (Nix + Devbox run in the WSL Linux environment, not native Windows). Native
  Windows is a **deliberate non-goal for the build side**: devbox is nix-based, and a scaffolded project's
  `make gen` assumes a unix shell. The binaries you build here do run on native Windows.

---

## 4. Browser requirements (Web tier)

- **OPFS** (Origin-Private File System) — needed for `dlc` to persist in the browser. Supported in current
  Chrome, Edge, Firefox, Safari.
- **File System Access API** (write to a user-picked *real* folder) — **Chromium only** (Chrome/Edge), and
  it's a **Backlog** feature; OPFS is the bootstrap baseline everywhere.
- Cross-origin isolation (COOP/COEP headers) may be required for OPFS SyncAccessHandle / SharedArrayBuffer
  depending on the jco/sqlite path — the dev server sets these.

---

## 5. Preflight — assess a machine

The check lives at **[`scripts/preflight.sh`](../scripts/preflight.sh)** (pure bash, no toolchain
dependency — it runs *before* Devbox exists). Run it directly or via `make doctor`:

```bash
./scripts/preflight.sh        # or: make doctor
```

It checks the **system** prereqs, then — inside a `devbox shell` — the **provisioned** toolchain, and
exits non-zero if a system prerequisite is missing. Sample output on a fresh machine:

```
System prerequisites (install yourself):
  git                ✓  git version 2.50.1
  devbox             ✗  install: https://www.jetify.com/docs/devbox/installing-devbox

Managed by Devbox (diagnostic — you never install this by hand):
  nix                ✗  Devbox installs this on first 'devbox shell'
...
→ NOT READY: install the 1 missing system prerequisite(s) above.
```

**Readiness rule of thumb:** if **git + Devbox** are present, you're ready — `devbox shell` handles
the rest. A modern browser covers the Web tier.

### Assessment output legend
- **All system rows ✓** → ready; enter `devbox shell`.
- **Devbox rows ✗ outside the shell** → *expected*; they appear once you `devbox shell`.
- **Devbox rows ✗ inside the shell** → a `devbox.json` gap — fix the manifest, not your machine.

**Two layers.** `scripts/preflight.sh` is **Layer 0** — a pure-bash, pre-toolchain gate that runs *before*
a `dlc` binary exists (it answers "can this machine even build `dlc`?"). Once `dlc` exists, **`dlc doctor`**
is **Layer 1** — the command form of this check, run against the `dlc` binary itself, reporting richer
per-tier readiness. `dlc doctor` is a **Phase B2** task (see [tasks](./DEVALBO-DLC-GO-TASKS.md), plan
§16.7); L0 stays as the bootstrap gate that gets you a first `dlc`. Both share a `make doctor` target.

---

## 6. First run (once prerequisites are met)

```bash
git clone <repo> && cd devalbo-ilc
devbox shell                 # provisions the pinned toolchain
make doctor                  # (task) confirm readiness
make build-engine            # TinyGo → engine.core.wasm
dlc new myapp                # scaffold a project (asks which tiers)
dlc new --tiers native --tiers web myapp   # …or say so, which scripts must
make dev-web                 # run dlc in the browser (React UI)
```

---

## Later tiers (not needed for bootstrap)

Additional prerequisites appear only when you target these — kept out of the bootstrap path:

- **Desktop (Wails):** a C toolchain + platform webview (WebKit / WebView2).
- **Embedded (RP2350 / ESP32-P4 / RP2040):** **`rustup`** (the cross targets come from
  `rust-toolchain.toml`; nixpkgs' `rustc` ships only the host's std) + **`picotool`** for flashing and for
  checking the UF2 family and boot block + a board + USB serial. Both install via Devbox's `rustup`
  CLI; the board SDKs are managed by PlatformIO, not Nix (§4.1 of the plan).
- **SQLite tier:** `modernc.org/sqlite` (Go dep, no system install) / `@sqlite.org/sqlite-wasm` (npm).

These carry the heaviest setup, which is exactly why the bootstrap is CLI + Web only.
