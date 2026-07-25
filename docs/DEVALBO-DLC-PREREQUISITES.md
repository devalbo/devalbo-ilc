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

> **Install yourself:** Nix · Devbox · git · a modern browser. (Optional: direnv.)
> **Everything else:** provisioned by `devbox.json` — do **not** install it manually.

This is scoped to the **bootstrap** (CLI + Web tiers). The embedded toolchain (PlatformIO, board SDKs) is
**not** needed yet — see [Later tiers](#later-tiers-not-needed-for-bootstrap).

---

## 1. System prerequisites (install these)

| Tool | Why | Install | Min |
| --- | --- | --- | --- |
| **git** | source control | OS package / Xcode CLT | any recent |
| **Nix** | the package engine under Devbox | [Determinate Systems installer](https://install.determinate.systems/) (`curl --proto '=https' -sSf -L https://install.determinate.systems/nix \| sh -s -- install`) | 2.18+ |
| **Devbox** (Jetify) | pins + provisions the toolchain from `devbox.json` | `curl -fsSL https://get.jetify.com/devbox \| bash` | recent |
| **A modern browser** | the Web tier (OPFS) | Chrome / Edge / Firefox / Safari (current) | OPFS-capable |
| direnv *(optional)* | auto-loads the devbox shell on `cd` | OS package | — |

**Node.js is *not* a required system install** — Devbox provides the Node used for jco/Vite. A system Node
is fine to have but isn't used by the pinned build.

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
| **Node.js** + **jco** (`@bytecodealliance/jco`) | web instantiation + Vite/React | Node 20+ |

Pin every version in `devbox.json` (+ its lockfile). The **versions matter to whoever authors
`devbox.json`**, not to the end user — the user just runs `devbox shell`.

---

## 3. Platform support

- **macOS** (arm64 / x86_64) — fully supported.
- **Linux** (x86_64 / arm64) — fully supported.
- **Windows** — via **WSL2** (Nix + Devbox run in the WSL Linux environment, not native Windows).

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
  nix                ✗  install: https://install.determinate.systems/
  devbox             ✗  install: https://get.jetify.com/devbox
...
→ NOT READY: install the 2 missing system prerequisite(s) above.
```

**Readiness rule of thumb:** if **git + Nix + Devbox** are present, you're ready — `devbox shell` handles
the rest. A modern browser covers the Web tier.

### Assessment output legend
- **All system rows ✓** → ready; enter `devbox shell`.
- **Devbox rows ✗ outside the shell** → *expected*; they appear once you `devbox shell`.
- **Devbox rows ✗ inside the shell** → a `devbox.json` gap — fix the manifest, not your machine.

Automating this is a task: **`dlc doctor`** (and a `make doctor` target) will run the same checks and
report per-tier readiness (see the tasks doc).

---

## 6. First run (once prerequisites are met)

```bash
git clone <repo> && cd devalbo-ilc
devbox shell                 # provisions the pinned toolchain
make doctor                  # (task) confirm readiness
make build-engine            # TinyGo → engine.core.wasm
dlc new myapp                # scaffold a project (terminal)
make dev-web                 # run dlc in the browser (React UI)
```

---

## Later tiers (not needed for bootstrap)

Additional prerequisites appear only when you target these — kept out of the bootstrap path:

- **Desktop (Wails):** a C toolchain + platform webview (WebKit / WebView2).
- **Embedded (ESP32-S3 / RP2350 / RP2040):** **PlatformIO** (owns ESP-IDF / arduino-pico toolchains, WAMR,
  flashing, `pio device monitor`) + a board + USB serial. PlatformIO installs via Devbox's `platformio`
  CLI; the board SDKs are managed by PlatformIO, not Nix (§4.1 of the plan).
- **SQLite tier:** `modernc.org/sqlite` (Go dep, no system install) / `@sqlite.org/sqlite-wasm` (npm).

These carry the heaviest setup, which is exactly why the bootstrap is CLI + Web only.
