import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { ilcVite } from "@devalbo/ilc-web/vite";

import { freshness } from "./vite-plugin-freshness";

const root = dirname(fileURLToPath(import.meta.url));
const repo = dirname(root);

// The ILC web wiring — shim pin, browser conditions, ES workers, single shim
// instance — comes from the host package rather than being restated here. This
// is the same preset a scaffolded app spreads into its own config; dlc consumes
// it exactly the way a generated project does, so a break shows up here first.
const ilc = ilcVite({ root, wasmDir: `${root}/src/wasm`, genDir: `${root}/src/gen` });

export default defineConfig({
  root: ".",
  // freshness() must come first: it fails fast when src/wasm or src/gen is
  // missing or older than its sources, so `npm run dev` cannot silently serve
  // a stale schema.
  plugins: [freshness(repo), react()],
  ...ilc,
  server: { port: 4173, strictPort: true, ...ilc.server },
});
