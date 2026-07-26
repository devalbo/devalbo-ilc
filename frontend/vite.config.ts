import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { freshness } from "./vite-plugin-freshness";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const root = dirname(fileURLToPath(import.meta.url));
const repo = resolve(root, "..");
// preview2-shim ≥0.19 ships browser builds under dist/browser (was lib/browser).
const shimBrowser = resolve(
  root,
  "node_modules/@bytecodealliance/preview2-shim/dist/browser",
);

export default defineConfig({
  root: ".",
  // freshness() must come first: it fails fast when src/wasm or src/gen is
  // missing or older than its sources, so `npm run dev` cannot silently serve
  // a stale schema the way it could before.
  plugins: [freshness(repo), react()],
  server: {
    port: 4173,
    strictPort: true,
    // hosts/web/ and gen/ live outside the Vite root on purpose — the host is
    // not part of the UI, and that boundary should stay visible in the tree.
    fs: { allow: [repo] },
  },
  resolve: {
    // package.json lists a "node" export condition; force browser builds.
    conditions: ["browser", "import", "module", "default"],
    alias: {
      // The transpiled component; `make build-wasm` puts it here (TinyGo → jco).
      // Aliased so hosts/web/ need not know the path.
      "@wasm": resolve(root, "src/wasm"),
      // Generated es-lite messages — one schema, shared with the Go engine.
      // Copied into the Vite root by `make gen-web` (see the Makefile note).
      "@gen": resolve(root, "src/gen"),
      // hosts/web/ lives outside the Vite root and has no node_modules of its
      // own, so every BARE specifier it imports must be aliased to this app's
      // copy. Keep this list in sync with hosts/web/'s imports — a missing
      // entry fails the build with "failed to resolve import", not at runtime.
      comlink: resolve(root, "node_modules/comlink"),
      // PIN, not a fork: the stock browser shim breaks TinyGo writes on bigint
      // offsets / missing flags (Spike 3). See hosts/web/shim/README.md.
      "@bytecodealliance/preview2-shim/filesystem": resolve(
        repo,
        "hosts/web/shim/filesystem.js",
      ),
      "@bytecodealliance/preview2-shim/cli": resolve(shimBrowser, "cli.js"),
      "@bytecodealliance/preview2-shim/clocks": resolve(shimBrowser, "clocks.js"),
      "@bytecodealliance/preview2-shim/io": resolve(shimBrowser, "io.js"),
      "@bytecodealliance/preview2-shim/random": resolve(shimBrowser, "random.js"),
      "@bytecodealliance/preview2-shim/environment": resolve(
        shimBrowser,
        "environment.js",
      ),
      "@bytecodealliance/preview2-shim/config": resolve(shimBrowser, "config.js"),
      "@bytecodealliance/preview2-shim": resolve(shimBrowser, "index.js"),
    },
  },
  // The engine worker uses dynamic import (it instantiates the component only
  // after OPFS is hydrated), which is code-splitting — so it must be an ES
  // module worker, not Vite's default IIFE.
  worker: { format: "es" },
  optimizeDeps: { exclude: ["@bytecodealliance/preview2-shim"] },
});
