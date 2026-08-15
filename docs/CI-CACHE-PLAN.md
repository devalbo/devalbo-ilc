# CI cache plan

A hand-off spec for making CI cheap **without making it weaker**. Two independent pieces of work:

1. **Toolchain caches** — stop re-downloading and re-compiling dependencies. Mechanical, low risk, biggest win.
2. **Versioned build artifacts** — content-address the things one tier builds for another, so an unmodified
   lower tier is *restored* instead of rebuilt.

Everything below is measured on this repo (2026-08-15) rather than estimated. `scripts/ci.sh` and
`scripts/test-b2.sh` now print per-step durations and a sorted summary, so the numbers can be re-measured on
the runner rather than trusted from here.

---

## 0. Rationale: why `full` runs what it runs

**Read this before optimising anything.** The tiers are not a priority ordering, and the cheap ones are not a
sufficient substitute for the expensive ones.

**The tier boundary is TOOLCHAIN AVAILABILITY, not importance.** `fast` is what runs without TinyGo, a
browser, or Rust. `full` adds the tiers that need them. `all` adds what needs the *network* (a scaffold
resolving its own `@latest` devbox packages, the pinned platform ref) plus the slow B1 spikes. A check is in
`full` because it needs a real toolchain, never because it is more important than one in `fast`.

**Why `full` must stay a per-push gate.** Every expensive check in it catches a class the cheap steps
provably cannot see:

| check | what only it can see |
| --- | --- |
| native↔wasm parity | the two tiers disagreeing — same engine, different answers or different filesystem |
| parity self-test | **the parity check having stopped working**; a check nobody has seen fail is indistinguishable from a broken one |
| scaffold builds and runs | `dlc new` emitting a tree that compiles and answers, with `GOPROXY=off` so a hidden fetch cannot rescue it |
| web tier (B3) | the browser tier, OPFS, and cross-tab clobbering — none of which exist natively |
| embedded firmware builds | an API change that keeps the *library* green and breaks its *callers* |

The governing rule is AGENTS.md §5: **a test CI does not run is a comment.** This repo has been bitten four
times by checks that existed and never executed, most recently two firmware modes that were skipped on every
machine and every nightly for as long as they had existed.

**So the one hard constraint on this whole plan: caching may make work cheaper, and may never make coverage
smaller.** Concretely — allowed: restore a `.cwasm` instead of recompiling it. **Not allowed:** skip the
firmware build because "nothing under `dlc-platform/embedded/` changed". Path-filtered test skipping is
specifically wrong here because the interesting failures are *cross-tier*: a change in `engine/` breaks the
badge, a change in the platform breaks the scaffold, a change in `templates/` breaks a build nothing else
compiles. "Unmodified" is exactly the claim this repo cannot cheaply prove, which is why the artifact hashes
in Part 3 must include every input, and why a hash miss must fall back to *building*, never to *skipping*.

---

## 1. What the measurements say

Measured locally, warm caches unless noted. Use the **ratios**, not the absolutes: CI is a cold Ubuntu runner
and will be slower.

| step | warm | notes |
| --- | --- | --- |
| `ci.sh full` (total) | **583s** | 728s in a run where two build artifacts had been deleted |
| engine boundary (B2) | **410s** in that run, **89s** standalone | same checks, 4.6× apart — cache warmth is the single biggest factor in this repo |
| ├ parity self-test | 39s | rebuilds with an injected probe |
| ├ native↔wasm parity | 32s | TinyGo wasip2 + Pulley |
| ├ scaffold builds and runs | 8s | |
| ├ example apps build and pass | 4s | |
| └ everything else in B2 | ≤3s each | units, golden, platform-gen, npm package |
| web tier (B3) | 58s | with `node_modules` and Chromium already present |
| embedded firmware builds | 35s | 5 flash-time modes, wasmtime warm |
| badge payload artifact | 7s | AOT only |
| hello component | 6s | TinyGo warm |
| bare-metal link | 4–25s | |
| codegen (`make gen`) | 17s | |
| `ci.sh fast` (total) | 14s | |

**The conclusion that should drive the work:** B2 at 410s cold versus 89s warm is a bigger prize than any
parallelism, and today the workflow caches **only devbox** (`enable-cache: true`). Nothing caches Go, TinyGo,
cargo, npm, or Playwright, so every push pays full cold compilation for all of them.

---

## 2. Toolchain caches (do this first)

### 2.1 The two cache classes — do not mix them up

| class | examples | fuzzy `restore-keys`? | why |
| --- | --- | --- | --- |
| **Compiler / dependency caches** | `go-build`, `tinygo`, cargo registry + target, `~/.npm`, Playwright browsers | **Yes, use them** | A partial hit is a warm start. The compiler revalidates inputs itself, so a stale entry costs time, never correctness. |
| **Content-addressed artifacts** | `engine.component.wasm`, `hello.pulley32.cwasm`, the `dlc` binary | **NO — exact key only** | A fuzzy hit yields *bytes built from different sources*, and every downstream check would validate the wrong artifact and pass. |

Getting this backwards is the one way this plan can produce a green CI that proves nothing.

### 2.2 Cache entries

Sizes are local measurements; CI will differ. All paths are for `ubuntu-latest`.

**IMPLEMENTED 2026-08-15**, with two deviations from the original table, both recorded below rather than
silently applied.

| # | what | paths | key | restore-keys |
| --- | --- | --- | --- | --- |
| C1 | Go build + module cache | resolved: `go env GOCACHE`, `GOMODCACHE` | `go-${{ runner.os }}-${{ hashFiles('**/go.sum') }}` | `go-${{ runner.os }}-` |
| C2 | TinyGo build cache | resolved: **`tinygo env GOCACHE`** | `tinygo-${{ runner.os }}-${{ hashFiles('devbox.lock', '**/go.sum') }}` | `tinygo-${{ runner.os }}-` |
| C3 | Cargo **registry + git only** | resolved: `${CARGO_HOME:-~/.cargo}` | `cargo-${{ runner.os }}-${{ hashFiles('dlc-platform/embedded/**/Cargo.lock', 'rust-toolchain.toml') }}` | `cargo-${{ runner.os }}-` |
| C4 | npm packages | resolved: `npm config get cache` | `npm-${{ runner.os }}-${{ hashFiles('**/package-lock.json') }}` | `npm-${{ runner.os }}-` |
| C5 | Playwright browsers | **declared**: `PLAYWRIGHT_BROWSERS_PATH` | `pw-${{ runner.os }}-${{ hashFiles('**/package-lock.json') }}` | `pw-${{ runner.os }}-` |
| C6 | devbox `init_hook` generator installs | `.devbox/gobin`, `.devbox/npm-global` | `devboxgen-${{ runner.os }}-${{ hashFiles('devbox.lock') }}` | `devboxgen-${{ runner.os }}-` |

### Deviation 1 — no `Swatinem/rust-cache`, and therefore no cached target dirs

`rust-cache` is a JS action: it cannot see devbox's environment, so making it work needed a shim directory of
symlinks, a job-level `RUSTUP_HOME`, and PATH surgery — three pieces of machinery whose only purpose was to
work around the toolchain manager this repo is built on. Measured target dirs are **11G / 3.5G / 1.5G / 1.0G /
911M**; the registry is **414 MB**. So C3 takes the registry, which is pure download cost and needs no
machinery, and leaves compiled artifacts alone.

**What this gives up is real**: wasmtime recompiles. Whether that dominates is a question for the per-step
summary on the runner, not for a guess here — and the honest next move if it does is `sccache` (§6), not
re-adding an action that has to be tricked into finding cargo.

### Deviation 2 — `.devbox/virtenv/rustup` (2.1 GB) is not cached

A fifth of the repository budget in one entry, for a *download* rather than a build. Re-fetching is likely
cheaper than moving 2.1 GB in and out every run — likely, not proven, so it is written down as a claim to
check against §5.6 rather than a decision to forget.

### Deviation 3 — devbox.json DECLARES the cache paths (the biggest simplification)

`devbox.json` has a first-class **`env`** field, and this repo was already using `init_hook` to point `GOBIN`
and `NPM_CONFIG_PREFIX` into `.devbox/`. Doing the same for the caches makes **devbox the single source of
truth for where they live**:

```json
"env": {
  "GOCACHE":                  "$PWD/.devbox/cache/go-build",
  "GOMODCACHE":               "$PWD/.devbox/cache/go-mod",
  "CARGO_HOME":               "$PWD/.devbox/cache/cargo",
  "npm_config_cache":         "$PWD/.devbox/cache/npm",
  "PLAYWRIGHT_BROWSERS_PATH": "$PWD/.devbox/cache/ms-playwright"
}
```

So the workflow **names** these paths instead of querying five tools, a laptop and a runner have the same
layout, and the paths cannot drift from what the build obeys. `$PWD` interpolation works; verified.

**TinyGo is the exception and cannot be declared.** It takes its cache from Go's `os.UserCacheDir()` and
ignores both `GOCACHE` and `XDG_CACHE_HOME` — checked, not assumed (`GOCACHE=/tmp/x tinygo env GOCACHE` still
answers `~/Library/Caches/tinygo`). It stays global, and `tinygo env GOCACHE` is the one query the workflow
still makes.

**Still SEPARATE cache entries with per-tool keys**, despite now being one directory tree. Go's cache should
turn over when `go.sum` moves and npm's when `package-lock.json` does; a single entry for `.devbox/cache/`
would evict everything whenever any one lockfile changed.

**Two costs, both real.** Every checkout gets its own copy rather than sharing `~/.cargo` and `~/go/pkg/mod`
across projects, so a second clone re-downloads — including cargo's 414 MB registry once. And `CARGO_HOME`
also decides where `cargo install` puts binaries: tools already in `~/.cargo/bin` keep working and stay on
PATH, but new installs made inside this project land in `.devbox/cache/cargo/bin` and will not be.

**A bug this caught.** `npx playwright install --with-deps` runs OUTSIDE devbox (it needs root, so it is the
adapter's job), which means it would have downloaded browsers to Playwright's own default while the cache and
the suite both used devbox's path. Nothing would have failed — the cache would simply never hit and the suite
would re-download inside the timed run. The workflow now reads the declared value from devbox and publishes it
to `$GITHUB_ENV`, so every later step agrees without the path being written twice.

### The devbox division of labour

Asked, because it was not obvious: **`devbox cache` is nix-store only** — `devbox cache upload [--to URI]`
pushes *nix packages* to a binary cache (Jetify's, or any URI), and `devbox cache info` currently reports "No
cache configured". It has no notion of a go-build or cargo cache. What it caches is what
`devbox-install-action`'s `enable-cache: "true"` already covers for free.

| what | cached by |
| --- | --- |
| go, tinygo, rustup, node, qemu, picotool binaries | **devbox**, via `enable-cache: "true"` — already on |
| build outputs (go-build, tinygo, cargo, npm, browsers) | `actions/cache`; outside devbox's remit |
| **where those live** | **devbox** — ask the tool, never hard-code |
| **cache keys** | **devbox** — `devbox.lock` is the real pin, `@latest` in devbox.json is not a version |

**Every path is resolved by asking the tool inside devbox**, and `tinygo env GOCACHE` is why: TinyGo keeps
its own cache at a *different* path from `go env GOCACHE`, so the plausible guess caches the wrong directory
— and `actions/cache` archives an empty directory, reports success, and leaves every run slow with nothing
to notice. `PLAYWRIGHT_BROWSERS_PATH` has no query, so it is *declared* in the job env instead and the
install step and the cache step agree by construction.

**Notes the agent needs:**

- **C6 needs no path resolution**, unlike the rest: `devbox.json`'s `init_hook` sets
  `GOBIN="$PWD/.devbox/gobin"` and `NPM_CONFIG_PREFIX="$PWD/.devbox/npm-global"`, so both are repo-relative
  by devbox's own config. It installs four generators there (`wit-bindgen-go`, `protoc-gen-go-lite`, `jco`,
  `protoc-gen-es-lite`) on every activation.
- **Scope the `Cargo.lock` glob to `dlc-platform/embedded/**`.** A bare `**/Cargo.lock` also matches the
  rustup toolchain sources under `.devbox/`, which would let installed content feed a cache key.
- **Do NOT cache `gen/`.** It is gitignored, regenerated in 17s, and is a *correctness surface* — a stale
  binding would mask exactly the codegen break `verify-platform-gen.sh` exists to catch. Regenerate every run.

### 2.3 Expected outcome

If the ratios hold, `full` should land nearer its warm 583s than its cold time, with B2 falling from ~410s
toward ~90s. Re-measure with the built-in summary before claiming a number.

---

## 3. Versioned build artifacts

The part that makes "an unmodified lower tier does not rebuild" true. Four artifacts are built by one tier and
consumed by another, are small, and are expensive to produce:

| artifact | size | built by | consumed by |
| --- | --- | --- | --- |
| `dlc` binary | — | `go build ./hosts/native` | every app's `make gen` / `dlc build web` |
| `example-apps/hello/build/engine.component.wasm` | ~1.5 MB | hello's `make build-web` (TinyGo + jco) | B3, and `make badge-cwasm` |
| `build/hello.pulley32.cwasm` | **890 KB** | `make badge-cwasm` (wasmtime AOT) | badge firmware's built-in payload modes, `make qemu` |
| badge firmware ELF/UF2 | ~1 MB | `cargo build --release` in `rp2350` | flashing only — not consumed by another check |

### 3.1 The hash graph

Each artifact's cache key is a hash of **every input that can change its bytes**, including its upstream
artifact's hash. Compute with `hashFiles(...)`, and remember `hashFiles` takes globs, so list source trees
explicitly rather than hashing the whole repo (which would defeat the point by changing on every commit).

```
H_toolchain = hashFiles('devbox.lock', 'rust-toolchain.toml')

H_dlc       = hashFiles('go.sum', 'engine/**', 'hosts/native/**', 'cmd/**',
                        'templates/**', 'proto/**', 'dlc-platform/**.go',
                        'dlc-platform/gen/**') + H_toolchain

H_component = hashFiles('example-apps/hello/engine/**',
                        'example-apps/hello/proto/**',
                        'example-apps/hello/cmd/**',
                        'example-apps/hello/dlc.toml',
                        'example-apps/hello/go.sum') + H_dlc

H_cwasm     = H_component
            + hashFiles('dlc-platform/embedded/precompile/**',
                        'dlc-platform/embedded/Cargo.lock')
            + H_toolchain      # wasmtime is pinned at 46.0.1 in devbox.json

H_firmware  = H_cwasm
            + hashFiles('dlc-platform/embedded/rp2350/**',
                        'dlc-platform/embedded/src/**',
                        'dlc-platform/embedded/Cargo.lock')
```

`templates/**` is in `H_dlc` deliberately: templates are embedded in the binary, so editing one changes what
`dlc new` emits with no other file changing.

### 3.2 Rules

1. **Exact key, no `restore-keys`.** A near-miss is the wrong bytes. On a miss, build.
2. **A miss falls back to building, never to skipping.** See Part 0.
3. **Verify after restore, cheaply — and know what the verification does not cover.** The artifact must exist
   and be non-empty. Downstream there is a real integrity check: the payload image records an FNV-1a checksum
   of the payload and `catalog.rs` verifies it before running, so a truncated or corrupted artifact is caught.
   **It cannot catch a well-formed artifact built from the wrong sources**, because the checksum is computed
   over whatever bytes the image builder was handed. The exact-match key in rule 1 is the *only* thing
   guaranteeing the bytes match the commit, which is why rule 1 is not negotiable.
4. **Save only from a clean tree.** `verify-parity-selftest.sh` deliberately injects `engine/zz_*.go`; `ci.sh`
   refuses to start if one survives. Never save an artifact cache from a job that ran the self-test without
   confirming the tree is clean, or a probe-built artifact could be published under a legitimate key.
5. **Keep `REQUIRE_BADGE_PAYLOAD=1` set in CI.** With artifacts now arriving from a cache, this is what turns
   "the artifact did not appear" into a red step instead of a silent skip of two firmware modes.

---

## 4. Job graph

`scripts/ci.sh` already exposes three slices that together cover exactly `full`:

```
./scripts/ci.sh native     # fast + engine boundary (Go, TinyGo; no browser)
./scripts/ci.sh web        # the browser tier (npm, Chromium)
./scripts/ci.sh embedded   # bare metal + Rust runtime + badge firmware
```

Run them as three parallel jobs, each with its own checkout — which is what makes them safe, because the
working tree is shared mutable state by design (`gen/` is regenerated, `build/` is one directory, `dlc` is one
binary at the root, and the self-test mutates `engine/`).

**Ordering:** the only cross-slice dependency is that `make badge-cwasm` needs hello's component. `full` gets
it free from the web tier; the `embedded` slice builds it itself via `make hello-component`. With Part 3 in
place, both restore it from cache when its inputs are unchanged.

**Keep the `windows` job as it is** — plain Go, no devbox, and it covers the filesystem behaviour a
cross-build cannot.

**Nightly (`schedule`) keeps running `all`** in a single job: it adds the network-bound checks, where wall
time matters least.

**Expect parallelism to be worth less than caching**, and possibly negative on a cold cache: three jobs each
pay their own toolchain warm-up, so total CPU rises while wall time falls. Land Part 2 first, re-measure, then
decide whether the split still pays.

---

## 5. Acceptance criteria

Falsify each one; do not infer it.

1. **A second identical run restores rather than rebuilds.** Push twice with no source change. Every cache
   reports a hit, and the per-step summary shows B2 near its warm number.
2. **A source change invalidates exactly one level.** Touch `example-apps/hello/engine/`: the component and
   the `.cwasm` rebuild, and the Go/TinyGo/cargo caches still hit. Touch nothing but
   `dlc-platform/embedded/rp2350/src/`: the firmware rebuilds and the `.cwasm` is restored.
3. **A corrupt restored artifact is caught, not used.** Truncate a cached `.cwasm` and confirm the run goes
   red — the catalog's FNV-1a check should fire. Record the limit honestly while you are there: a *complete*
   artifact from the wrong sources is indistinguishable to every downstream check, so this criterion tests the
   corruption path only, and rule 1 of §3.2 covers the rest.
4. **The required-payload gate still fires.** Remove the artifact and confirm `embedded firmware builds` goes
   red with `REQUIRE_BADGE_PAYLOAD=1` set — proving the cache cannot silently reduce coverage to four modes.
5. **Coverage is unchanged.** The union of steps across the three slice jobs equals the step list of
   `ci.sh full`. `scripts/ci.sh` composes tiers in exactly one `case` block; read it and compare.
6. **The cache budget holds.** Total cache size stays under the repository limit with room for LRU churn, and
   no single entry takes longer to upload than it saves.

---

## 6. Out of scope

- **Path-filtered test skipping** (`paths:` / `dorny/paths-filter` gating whole jobs). Rejected in Part 0.
- **Caching `gen/`.** Regenerate it; see §2.2.
- **Running the three example apps' browser suites in parallel.** hello, notes and tictactoe all bind
  **port 5273** with `--strictPort`, which was a deliberate fix after tests silently ran against another app's
  dev server. Per-app ports would have to land first. (`dlc`'s own web suite uses 4173 and is not affected.)
- **Raising Playwright's `workers: 1`.** Actual test execution is a handful of seconds per suite; the cost is
  installs and builds, which is what Part 2 addresses. `fullyParallel: false` is also load-bearing for notes'
  cross-tab tests, which coordinate two pages against one OPFS origin.
- **A remote build cache (sccache, Bazel, Nix store push).** Defensible later; out of scope until Part 2's
  numbers are known.
