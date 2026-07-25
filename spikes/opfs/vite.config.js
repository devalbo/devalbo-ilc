import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const root = dirname(fileURLToPath(import.meta.url));
// preview2-shim ≥0.19 ships browser builds under dist/browser (was lib/browser).
const shimBrowser = resolve(
  root,
  "node_modules/@bytecodealliance/preview2-shim/dist/browser",
);

export default defineConfig({
  root: ".",
  server: {
    port: 4173,
    strictPort: true,
  },
  // package.json lists a "node" export condition; force browser builds for Chromium.
  resolve: {
    conditions: ["browser", "import", "module", "default"],
    alias: {
      // Patched getFlags/setSize/truncate — see shim/filesystem.js (based on 0.19.0)
      "@bytecodealliance/preview2-shim/filesystem": resolve(
        root,
        "shim/filesystem.js",
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
  optimizeDeps: {
    exclude: ["@bytecodealliance/preview2-shim"],
  },
});
