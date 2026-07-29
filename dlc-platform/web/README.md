# hosts/web/ — browser host (TypeScript glue only)

Runs the same engine under **jco** in the browser. **TypeScript for glue + wiring only — no business logic.**

**Contents:**
- `worker.ts` — the engine worker. Hydrates OPFS into the shim's FileData tree, instantiates the wasip2
  component, exposes `execute(method, request)` over **Comlink**, and flushes back to OPFS after each call.
- `api.ts` — environment-agnostic adapter and the **only** door from the UI to the engine. The UI builds a
  proto request from form state; parsing/menus are host-side (Decision 28).
- `opfs.ts` — the OPFS ↔ FileData bridge (hydrate / flush / list / clear), lifted from Spike 3.
- `events.ts` — the `devalbo:ilc/events` **import**, i.e. a capability this host *provides to the engine*.
  Everything else here calls into the engine; this is the one module it calls back out to.
- `shim/` — a **pinned** patched copy of `preview2-shim`'s browser filesystem. See its README.

**Boot ordering is load-bearing.** Hydrate OPFS *before* importing the transpiled component: the guest
snapshots its preopen descriptor at instantiation, so a later `_setFileData` is invisible to a live engine.
That is why `reboot()` exists instead of a re-hydrate.

**Why a worker:** OPFS sync access handles want one, and it keeps engine calls off the UI thread.

## Events (§6.3)

The engine emits, the host forwards, the UI re-reads. Subscribe through `api.ts`:

```ts
const off = await subscribe((topic, payload) => {
  if (topic === TopicDataChanged) refetch();
});
```

Three rules, each of which fails far from its cause if broken:

1. **`emit` must stay synchronous.** An `async` import returns a Promise, and jco only supports that under
   `--async-imports`, which ILC deliberately does not use (Decision 22, `docs/WASI-UPGRADES.md`). The
   failure surfaces as a jco type error nowhere near the `async` someone added.
2. **`emit` must not throw.** It runs on the engine's stack, mid-command, with the command's filesystem
   write already committed. `events.ts` swallows listener exceptions for exactly this reason.
3. **Never call `execute` from inside the import.** That re-enters the engine while a command is on the
   stack. This host is safe *by construction* — the forwarder posts a message rather than running
   main-thread code — so a subscriber on the main thread may freely call `execute`. A native host has no
   such boundary and must defer explicitly.

**Events are held until the flush completes.** The engine emits mid-command, before the worker has
persisted anything to OPFS. A listener told "data changed" that immediately called `listFiles` would race
that flush and read a half-written tree, so the worker batches a command's events and delivers them after
the write is durable. Spontaneous events (no command in flight) go out immediately.

**One relay, many subscribers.** The worker holds a single listener; `api.ts` keeps the subscriber set and
fans out on the main thread. Handing each subscriber to the worker directly would mean the second one
silently evicted the first.

**The import must be mapped at transpile time.** jco turns a WIT import into a bare specifier — the
component does `import { emit } from 'devalbo:ilc/events'` — which no bundler resolves on its own:

```
jco transpile engine.component.wasm -o out --map 'devalbo:ilc/events=@devalbo/dlc-web/events'
```

`dlc build web` applies this (`hosts/native/build.go`); the platform's own build does it in the `Makefile`.
**Every capability added later needs a line in both.** Forget it and the browser reports only "Failed to
fetch dynamically imported module", naming the component rather than the missing import.

**Flushing** happens after *every* `execute`. Deliberately conservative for the bootstrap — the engine
cannot yet signal that it wrote anything, so flush-always is the only correct option. Revisit when the
Events capability can report a write.

**This directory lives outside the Vite root on purpose** (the host is not the UI). Consequences, both
handled in `frontend/vite.config.ts`: `server.fs.allow` must include the repo, and every **bare** specifier
imported here must be aliased to the frontend's copy — a missing alias fails the build.

The React UI lives in `frontend/` and talks to the engine only through `api.ts`.

Verify: `make verify-web` (headless) or `make verify-web-watch` (headed, so you can watch it).

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.1, §5.2, Decisions 28 + 29.
