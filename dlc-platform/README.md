# engine/platform/ — the ILC runtime apps INHERIT

What every app gets rather than writes (§16.3): command dispatch, the filesystem
root seam, path containment, BFT bundles, the startup sequence (`Boot`), and the
verbs `version` / `set-environment` / `get-command-surface` / `export-fs` /
`import-fs` / `reset-fs`.

**This becomes the `dlc-platform` module** (§16.4). Templates **depend on it, never
inline it** (§16.6) — and that rule is load-bearing, not tidiness: code copied into a
scaffold is frozen in that app forever, so a path-containment fix inlined into a
template could never reach an app that had already been generated.

## Method id ranges (permanent)

Per-capability blocks, mirroring §6, so a new capability gets its own block instead of
the next free number — and an id's magnitude tells you what it is in a wire trace.

| Range | Owner |
| --- | --- |
| 1 – 99 | core lifecycle (`version`, `set-environment`, `get-command-surface`; later `supported-abis`) |
| 100 – 199 | filesystem (`export-fs`, `import-fs`, `reset-fs`; later read/write/delete/list) |
| 200 – 299 | index (SQLite) |
| 300 – 399 | events |
| 400 – 499 | display |
| 500 – 599 | network |
| 600 – 9999 | reserved |
| **10000 +** | **the app** (`platform.AppMethodBase`) |

Dispatch is one flat `map[uint32]Handler` across all of it, so the ranges are what stop
an app colliding with a platform verb added later. The platform range is far larger than
it needs to be on purpose: ids are `u32`, so reserving 9999 costs nothing, while widening
the range once apps exist would break every app that had claimed an id inside it.

## What an app writes

```go
func init() {
    platform.RegisterAll()              // inherit version/export-fs/import-fs/reset-fs
    platform.SetVersion("myapp 1.0.0")  // the platform owns the command, you own the string
    platform.Register(MethodFoo, platform.TypedHandler(handleFoo))
}
```

To change a platform verb's semantics, `Unregister` it first — a plain re-`Register`
panics, so an override is never accidental.

### Registration policy: eager or discovered

`RegisterAll` is right for an app whose hosts always have a filesystem — today, every
native app. Use `RegisterDiscovered` when one might not (a browser whose OPFS is
denied) and you would rather offer a smaller command surface than verbs that cannot
work: it registers the core-lifecycle block now and each capability's verbs when the
environment manifest says that capability is there (§6.4a).

The choice is engine-side but the reason is host-side, and the engine is ONE artifact
across every tier — so an app cannot be eager natively and discovering in a browser. It
picks one for all tiers. `dlc` discovers; `notes` and `tictactoe` are eager.

## What a HOST writes

```go
platform.Boot(platform.BootOptions{
    Root:           platform.AppRoot(dlcconfig.Name),  // or "." if the data is the user's project
    FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR,
})
```

`Boot` owns the startup sequence — grant the root, install the event sink, send the
manifest — because the order is load-bearing and a template that copied it would freeze
today's order into every app scaffolded today (`docs/ENVIRONMENT-PLAN.md` §2.5).

**Every in-process caller is a host.** A test, a golden-tree generator and the parity
runner each learned this by granting a root without sending a manifest and getting
`unknown method_id 100`.

**Rules:** TinyGo-safe and reflection-free (it compiles into every tier, wasm included).
Use `SafeJoin` / `WriteTree` for anything touching the filesystem — apps get containment
by using the platform rather than by remembering to check.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.2, §7.3, §16.3, §16.4, §16.6.
