import { defineConfig } from "vite";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { ilcVite } from "@devalbo/ilc-web/vite";

const root = dirname(fileURLToPath(import.meta.url));

// The ILC web wiring — the preview2-shim pin, browser export conditions,
// ES-format workers, and single-shim-instance aliasing — comes from the host
// package. Every one of those settings fails in a way that does not name its
// cause, so they live in versioned code rather than in this file.
//
// Both directories are inside this root on purpose: jco's loader FETCHES the
// core .wasm at runtime, so it has to be servable, and generated messages must
// resolve @aptre/* from this tree. `make build-web` and `make gen-web` fill them.
const ilc = ilcVite({
  root,
  wasmDir: resolve(root, "src/wasm"),
  genDir: resolve(root, "src/gen"),
});

export default defineConfig({
  root: ".",
  // Spread ilc FIRST, then server — otherwise ilc.server replaces the whole
  // server block and drops strictPort. Without strictPort, a busy 5173 makes
  // Vite silently pick another port while Playwright still waits on 5173.
  ...ilc,
  server: { port: 5173, strictPort: true, ...ilc.server },
});
