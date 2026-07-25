# hosts/web/ — browser host (TypeScript glue only)

Runs the same engine under **jco** in the browser. **TypeScript for glue + wiring only — no business logic.**

**Contents:**
- `worker.ts` — boots the jco module + `preview2-shim`; `setPreopens({'/': opfsRoot})` (OPFS); injects
  capabilities (WASI stdio → `console.*`, filesystem → OPFS).
- `api.ts` — environment-agnostic adapter exposing `executeCli(args)` to the UI (via Comlink).

The React UI lives in `frontend/` and talks to the engine only through this adapter.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.1, §5.2.
