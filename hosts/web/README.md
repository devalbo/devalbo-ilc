# hosts/web/ — browser host (TypeScript glue only)

Runs the same engine under **jco** in the browser. **TypeScript for glue + wiring only — no business logic.**

**Contents:**
- `worker.ts` — the engine worker. Hydrates OPFS into the shim's FileData tree, instantiates the wasip2
  component, exposes `execute(method, request)` over **Comlink**, and flushes back to OPFS after each call.
- `api.ts` — environment-agnostic adapter and the **only** door from the UI to the engine. The UI builds a
  proto request from form state; parsing/menus are host-side (Decision 28).
- `opfs.ts` — the OPFS ↔ FileData bridge (hydrate / flush / list / clear), lifted from Spike 3.
- `shim/` — a **pinned** patched copy of `preview2-shim`'s browser filesystem. See its README.

**Boot ordering is load-bearing.** Hydrate OPFS *before* importing the transpiled component: the guest
snapshots its preopen descriptor at instantiation, so a later `_setFileData` is invisible to a live engine.
That is why `reboot()` exists instead of a re-hydrate.

**Why a worker:** OPFS sync access handles want one, and it keeps engine calls off the UI thread.

**Flushing** happens after *every* `execute`. Deliberately conservative for the bootstrap — the engine
cannot yet signal that it wrote anything, so flush-always is the only correct option. Revisit when the
Events capability can report a write.

**This directory lives outside the Vite root on purpose** (the host is not the UI). Consequences, both
handled in `frontend/vite.config.ts`: `server.fs.allow` must include the repo, and every **bare** specifier
imported here must be aliased to the frontend's copy — a missing alias fails the build.

The React UI lives in `frontend/` and talks to the engine only through `api.ts`.

Verify: `make verify-web` (headless) or `make verify-web-watch` (headed, so you can watch it).

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.1, §5.2, Decisions 28 + 29.
