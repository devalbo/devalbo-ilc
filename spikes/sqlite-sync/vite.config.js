import { defineConfig } from "vite";

// `@sqlite.org/sqlite-wasm` must NOT be pre-bundled: its ESM entry locates the
// .wasm relative to its own module URL, and Vite's dep optimizer rewrites that
// URL into .vite/deps/ where the .wasm is not. Excluding it makes Vite serve the
// package from node_modules with its layout intact.
//
// Deliberately NO COOP/COEP headers. The other OPFS VFS ("opfs") needs
// SharedArrayBuffer and therefore cross-origin isolation; the SAH-pool VFS does
// not, and proving that is half of why this spike exists — requiring isolation
// would impose headers on every app that ever hosts an ILC web tier.
export default defineConfig({
  optimizeDeps: { exclude: ["@sqlite.org/sqlite-wasm"] },
  worker: { format: "es" },
  server: { port: 4180, strictPort: true },
});
