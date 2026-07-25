# Spike 5 — dual-track async probe (T-B1.5)

`make spike-async` → [`scripts/spike-async.sh`](../../scripts/spike-async.sh).  
**Rule:** no ILC async shims. Tracks stay separate ([`WASI-UPGRADES.md`](../../docs/WASI-UPGRADES.md)).  
**Rich host pin:** Node **≥24** with `--experimental-wasm-jspi` (`nodejs@24` in `devbox.json`).

---

## Fixtures

| Fixture | Track | Guest | Host | Execute |
| --- | --- | --- | --- | --- |
| **F-R-sync** | Rich / CM | TinyGo wasip2 `async-engine` → `engine.component.wasm`; `execute-cli(["wait","50"])` → `host-delay.delay(50)` | `host-delay.js` returns Promise → `"ok:50"` after `setTimeout` | sync `jco transpile` + Node `executeCli(["wait","50"])` *(negative control)* |
| **F-R-jspi** | Rich / CM | same guest wasm | same host | `jco transpile --async-mode jspi --async-imports '…#delay' --async-exports 'execute-cli'` + `node --experimental-wasm-jspi` |
| **F-P** | Portable / WAMR-shaped | TinyGo wasip1 `portable/` → `engine.core.wasm`; `run_wait` → `env.host_delay` | wazero: `host_delay` = `Sleep` then return `ms` | `go run ./cmd/portable-host engine.core.wasm 50` |

---

## Test execution matrix (one row = one assertion)

| ID | Fixture | Assertion | Result (2026-07-25) |
| --- | --- | --- | --- |
| **R1.1** | F-R-sync | Call completes within 5s (no throw / timeout) | 🔴 FAIL — `expected a string, received [object]` *(expected: sync jco can't await Promise)* |
| **R1.2** | F-R-sync | `command-result.success === true` | ⬜ SKIP (blocked by R1.1) |
| **R1.3** | F-R-sync | `output` decodes to `"ok:50"` | ⬜ SKIP (blocked by R1.1) |
| **R1.4** | F-R-sync | Event-loop tick counter &gt; 0 during the call | ⬜ SKIP (blocked by R1.1) |
| **R2.1** | F-R-jspi | Runtime has `WebAssembly.Suspending` or `WebAssembly.promising` | 🟢 PASS (Node ≥24 + `--experimental-wasm-jspi`) |
| **R2.2** | F-R-jspi | Transpiled module exports `executeCli` | 🟢 PASS |
| **R2.3** | F-R-jspi | Call completes within 5s (no throw / timeout) | 🟢 PASS |
| **R2.4** | F-R-jspi | `command-result.success === true` | 🟢 PASS |
| **R2.5** | F-R-jspi | `output` decodes to `"ok:50"` | 🟢 PASS |
| **R2.6** | F-R-jspi | Event-loop tick counter &gt; 0 during the call | 🟢 PASS |
| **P1.1** | F-P | `run_wait` export exists and `Call` returns without error | 🟢 PASS |
| **P1.2** | F-P | Return value `== 50` | 🟢 PASS |
| **P1.3** | F-P | Wall elapsed ≥ 50ms (host actually blocked) | 🟢 PASS |

| Roll-up | Rule | Result |
| --- | --- | --- |
| **RICH** | All **R2.\*** PASS → GREEN (R1 = negative control) | 🟢 GREEN |
| **PORTABLE** | All P1.\* PASS → GREEN | 🟢 GREEN |

**Gotchas (measured):**

- Node **22** has no JSPI surface even with `--experimental-wasm-jspi` → pin **24+** (`nodejs@24`).
- jco needs **both** `--async-imports …#delay` and `--async-exports execute-cli`; import-only yields `trying to suspend without WebAssembly.promising`.
- Sync host control (string return, no Promise) is GREEN under sync jco — the R1 failure is specifically Promise-as-result.
- wasmtime + blocking `host-delay` is GREEN (off-matrix) — CLI/desktop can block; web needs JSPI.
- Browser JSPI not yet in CI (Node is the gate); `wasmtime-go` still lacks Component Model bindings for B2.

---

## Layout

| Path | Fixture |
| --- | --- |
| `main.go`, `host-delay.js`, `harness.mjs` | F-R-sync, F-R-jspi |
| `portable/main.go`, `cmd/portable-host/` | F-P |

```bash
devbox run make spike-async
```
