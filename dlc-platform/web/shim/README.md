# hosts/web/shim/ — vendored `preview2-shim` filesystem

`filesystem.js` is a patched copy of `@bytecodealliance/preview2-shim@0.19.0`'s **browser** filesystem.

**Why it exists:** the stock browser shim breaks TinyGo writes — `getFlags`/`setSize` are missing,
`openAt` mishandles truncate, and `write` compares a `Number(0)` offset against `0n`. Spike 3 (T-B1.3)
measured this; without the patch, `os.WriteFile` from the engine fails in the browser.

Also exports `_getFileDataTree()` (live object). Stock `_getFileData()` JSON.stringifies
Uint8Arrays as `{0:n,1:n,…}`; flushing a full `dlc new` scaffold through that path timed out
the web suite on CI once the template grew.

**Rule:** this is a *pin*, not a fork to evolve. Re-check it on every `preview2-shim` bump — when upstream
fixes the bigint/flags handling, delete this file and drop the Vite alias in `frontend/vite.config.ts`.

Source of truth for the findings: [`spikes/README.md`](../../../spikes/README.md) (Spike 3).
