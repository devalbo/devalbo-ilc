# `dlc`'s command surface

**The canonical list.** It did not exist before 2026-07-29: the commands were split across two dispatch
mechanisms with two separate lists, and the two most-used ones appeared in neither — `dlc gen` and
`dlc build` were a hand-written Go map consulted *before* the CLI ran, so `dlc --help` omitted them entirely.
Someone reading the help would fairly conclude they did not exist.

They are now declared in proto like everything else. **`dlc --help` is the live truth**; this page explains
the shape and lists what is planned.

---

## 1. Three sources, one surface

`dlc`'s CLI concatenates three generated slices (`hosts/native/commands.go`):

| Source | Declared in | Served by | Band |
| --- | --- | --- | --- |
| **the platform's inherited verbs** | `dlc-platform/proto/devalbo/ilc/v1/platform.proto` | the engine | 1–599 |
| **`dlc`'s own verbs** | `proto/devalbo/dlc/v1/commands.proto` | the engine | 9000–9099 |
| **`dlc`'s toolchain verbs** | `proto/devalbo/dlc/v1/toolchain.proto` | **the host** | 9100–9999 |

Every app has the first group. **10000+ belongs to the app and nothing else** — `dlc` moved out of it on
2026-07-29, reversing Decision 29's "claims no privileged block". Collision was never the issue (`dlc` and a
scaffolded app never share a registry); the problem was that `10000` meant "some app's first command, *or*
dlc's `New`", and nothing distinguished them. The block was affordable because 600–9999 was held for
capability verbs and capabilities turned out not to be command-shaped — events is an import, a shared render
is a pulled app command, network is `wasi:http`. Seven framework ids exist in total.

---

## 2. Implemented today

### Inherited by every ILC app

| Command | id | What |
| --- | --- | --- |
| `version` | 1 | the app's version; the string is app-supplied |
| `export-fs` | 100 | bundle a subtree into one portable blob (BFT) |
| `import-fs` | 101 | write a bundle back into the filesystem |
| `reset-fs` | 102 | delete everything under a prefix |

Two more are **dispatchable but not typeable** (`cli_hidden`) — a host sends them, a person never does:

| Command | id | What |
| --- | --- | --- |
| `set-environment` | 2 | the host states what it can do (Decision 32); also triggers capability registration |
| `get-command-surface` | 4 | which ids are registered *right now*, which is not what the schema says |

### `dlc`'s own, served by the engine

| Command | id | What |
| --- | --- | --- |
| `new` | 9000 | scaffold a project |
| `echo` | 9001 | echo through the engine — the smallest end-to-end proof |

Both run in a browser as well as a terminal, which is the test Decision 30 applies: a verb that only touches
app data is an engine handler.

### `dlc`'s own, served by the host (`host_local`)

| Command | id | What |
| --- | --- | --- |
| `gen` | 9100 | regenerate bindings from the schema and `dlc.toml` |
| `build` | 9101 | build a tier (`dlc build web` transpiles the wasm component) |
| `run` | 9102 | launch a tier: exec the CLI, or serve the web tier and open a browser |

These spawn processes, so they cannot run inside wasm and never reach `execute`.

**Their flags are declared in proto too**, so parsing, help text, defaults and
required-ness are generated exactly as for an engine command — there is no
hand-rolled flag loop left in any of them:

```
dlc build [tier] [--out dir] [--web-out dir] [--entry pkg]
dlc run   [tier] [args…] [--no-open]
dlc gen
```

**A boolean is a switch.** `--no-open`, not `--no-open true` — and `--no-open=false` still works for
overriding a default. That took a matching fix in the argv permutation, which had assumed every known flag
consumes the following token: `--no-open web` would otherwise have moved the tier into the flag list and left
no positional behind.

**`run` refuses by default, and that is the design.** `dlc` can exec a binary and
serve a directory; it cannot flash a board or push firmware. A declared tier it
cannot launch gets a message naming itself and a non-zero exit, because appearing
to start something is worse than saying no — the same stance `build` takes. Only
`native` and `web` are launchable today.

`run web` also **refuses stray program arguments** rather than dropping them: the
web tier has nothing to forward them to, and a silently ignored argument is only
noticed when the output is wrong.

---

## 3. Declared and reserved — not built

Held in `toolchain.proto` so the names and numbers cannot be claimed twice. All host-local for the same
reason: they spawn a toolchain or inspect the machine.

| Command | id | Notes |
| --- | --- | --- |
| `verify` | 9103 | the §11 matrix; `--parity` is the Decision 26 native↔wasm check |
| `doctor` | 9104 | the command form of `scripts/preflight.sh`; today `devbox run doctor` |
| `host add` | 9105 | scaffold a tier slot |
| `proto` | 9106 | proto-only codegen, without the rest of `gen` |

One engine-side id is reserved by the platform: **3**, for `SupportedAbis` (Decision 31 — the guest
advertising *its* boundaries, the opposite direction to the manifest).

---

## 4. How `host_local` works

The option is on the rpc:

```proto
rpc Gen(GenRequest) returns (GenResponse) {
  option (devalbo.options.v1.method_id) = 9100;
  option (devalbo.options.v1.host_local) = true;
}
```

What it changes:

- **the name, summary and help come from the schema**, like every other command — which is the whole point,
  since that is what was missing
- **`protoc-gen-dlc-registry` emits no dispatch map** for a service with no engine-served rpc, so an engine
  *cannot* serve one by accident
- **the web surface omits it entirely.** A browser cannot spawn a toolchain, so a host-local verb is left out
  of the generated TypeScript rather than flagged — a command that is listed and cannot run is worse than one
  that is absent, and omitting leaves no runtime check to forget
- **the runner requires a local handler** and refuses to build the surface without one, the same stance it
  takes on a missing renderer: a declared command that silently does nothing is worse than a build error
- **the live-surface check skips it.** A host-local verb is never in the engine's registry, so asking the
  engine about it would mark it permanently `(unavailable on this host)` — which is exactly what happened on
  the first run
- **its flags are declared and parsed like any other command's** — no hand-rolled loop, and booleans are
  switches
- **argv IS reordered** for it, like any other command. It briefly was not — on the grounds that these verbs
  parsed their own arguments — and that exemption outlived its reason: once the flags moved into the schema,
  `dlc build web --entry x` failed with `unexpected argument` until the exemption came out

### Ids here are not wire ids

Nothing sends them anywhere; they key the command surface inside one process. So an implemented host-local id
is **excluded from `method-ids.lock`** — changing one breaks no peer.

**Claiming a reservation is a lock change**, and the generator refuses until it is re-blessed deliberately
(`DLC_ID_LOCK_UPDATE=1 make gen`) — which is how `run` landed: dropping `[reserved].9102` failed the build
until the removal was reviewed.

**Reservations are still locked**, and the asymmetry is deliberate: the lock is also the only record of "this
number is claimed", and a reservation quietly disappearing should show up in review. So the lock answers two
questions — what the wire promises, and what names are taken — and only the first applies to an implemented
host-local verb.

They must still be unique *within a surface*, since all three groups are concatenated into one `[]Command`.
Hence the 9100–9999 sub-block, distinct from `dlc`'s engine-served 9000–9099.

---

## 5. Where the truth lives

| Question | Answer |
| --- | --- |
| what can I run right now? | `dlc --help` |
| what does this command take? | `dlc <command> --help` — flags, positionals and required-ness are generated |
| which ids are permanent? | `proto/method-ids.lock` and `dlc-platform/proto/method-ids.lock` (committed) |
| which ids are registered in a running engine? | `get-command-surface` (id 4) — not the same as the schema |
| what is planned? | §3 above, and Decision 30 in [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) |

---

## 6. Adding a command

1. **Add the rpc** to the right `.proto`, with a permanent `method_id` in the right band — **an app's own
   command goes at 10000+**; only `dlc` uses 9000–9999. Add
   `host_local = true` if it must spawn a toolchain or inspect the machine.
2. **`make gen`.** The id lock will report a new id; that is expected for an addition.
3. **Engine-served:** write the handler and register it. **Host-local:** add the function to the host's
   `Local` map, keyed by the generated `Method…` constant — never by a literal. A local handler receives the
   **encoded request** (decode it with the generated `UnmarshalVT`), not argv: same parsing as an engine
   command, minus the boundary.
4. **Add a renderer** if it is engine-served. Forgetting one is a build error, not silence. A host-local verb
   owes none — it prints its own progress as it drives the toolchain, and has no response to format.
5. **Never write an id in Go or TypeScript.** They are generated. Reserve unused ones in the `.proto` with
   `reserved_method_id`, never in a comment.

Two failure modes worth recognising:

- `command "x" (method N) has no renderer registered` — an engine verb with no printer.
- `command "x" (method N) is host-local but no handler is registered` — a declared local verb with no
  behaviour attached.

Both are deliberate build-time errors. A command that exists in help and does nothing is the outcome they
prevent.
