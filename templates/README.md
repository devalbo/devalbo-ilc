# templates/ — what `dlc new` emits (its own concern)

Layout (§16.6, Decision 25) — directory names = **substrate**; track prose = Rich/CM vs Portable/WAMR:

| Path | Kind | Role |
| --- | --- | --- |
| `templates/component-model/` | **in-tree first** (submodule later) | Rich/CM — **full `dlc`-shaped** terminal + browser skeleton. **Bootstrap first.** |
| `templates/wamr/` | **in-tree first** (submodule later) | Portable/WAMR — after embedded verify exists. |
| `templates/fragments/` | **in-tree** overlay packs | `--caps` / `--tiers` / `--ui` / `--storage` — ABI mode picks the skeleton, not a fragment |

**Bootstrap sequencing (locked):**
1. Author skeletons **in-tree**; lift to per-skeleton git submodules later.
2. **Defer** versioned `ilc-platform` `go.mod` depends until that submodule graduation.
3. `component-model/` is a **full `dlc`**, not a thin hello-world.
4. Engine wasm is **`go:embed`’d in the native host** (lift-ready package); template trees are
   **`go:embed`’d into the engine** for offline + browser `dlc new`. Never runtime-`git clone`.

**Rules:**
- **Depend-on, never inline** is the *destination* after `ilc-platform` extract + submodule lift.
- **Validated by** scaffold → build → verify (test-steps Scaffolder row).

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §16.6 and [`WASI-UPGRADES.md`](../docs/WASI-UPGRADES.md).
