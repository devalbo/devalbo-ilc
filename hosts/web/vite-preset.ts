// The Vite wiring an ILC web app needs, as a function instead of a paragraph in
// a README.
//
// Every one of these settings is load-bearing and non-obvious, and getting one
// wrong fails in a way that does not name the cause:
//
//   - the shim PIN: stock preview2-shim's browser filesystem breaks TinyGo writes
//     (bigint offsets, missing flags) and no-ops `unlinkFileAt`, so a delete
//     silently succeeds and changes nothing (Spike 3 + the reset-fs work)
//   - browser export conditions: the package advertises a "node" condition that
//     Vite would otherwise prefer, yielding a host that cannot see OPFS
//   - `worker.format: "es"`: the engine worker dynamic-imports the component
//     (it must, to instantiate AFTER hydrating OPFS), which is code-splitting,
//     which IIFE workers cannot do
//   - a single shim instance: the shim holds module-level state (`_setFileData`
//     sets the root preopen), so if the worker and the component resolve
//     different copies, one hydrates a tree the other never reads
//
// A scaffolded app spreads this into its own config, so a fix here arrives on a
// version bump rather than needing an edit in every generated project.
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

export interface IlcViteOptions {
  /** The app's Vite root (where node_modules lives). */
  root: string;
  /** Where `dlc build web` put the transpiled component. Default: build/wasm. */
  wasmDir?: string;
  /** Where generated es-lite messages were copied. Default: src/gen. */
  genDir?: string;
}

export function ilcVite(opts: IlcViteOptions) {
  const { root } = opts;
  const wasmDir = opts.wasmDir ?? resolve(root, "build/wasm");
  const genDir = opts.genDir ?? resolve(root, "src/gen");
  const shimBrowser = resolve(
    root,
    "node_modules/@bytecodealliance/preview2-shim/dist/browser",
  );

  return {
    resolve: {
      conditions: ["browser", "import", "module", "default"],
      alias: {
        "@wasm": wasmDir,
        "@gen": genDir,
        // PIN, not a fork — see the note above.
        "@bytecodealliance/preview2-shim/filesystem": resolve(
          here,
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
        // This package lives outside the app's root, so every BARE specifier it
        // imports must resolve to the app's copy — otherwise the build fails
        // with "failed to resolve import", or worse, loads a second shim.
        comlink: resolve(root, "node_modules/comlink"),
      },
    },
    optimizeDeps: { exclude: ["@bytecodealliance/preview2-shim"] },
    worker: { format: "es" as const },
    // Allow the app root, this package, and the package's parent (the platform
    // checkout when consumed via file:). Vite resolves realpaths of symlinked
    // file: deps; omitting the parent fails on some CI layouts with
    // "outside of Vite serving allow list".
    server: { fs: { allow: [root, here, dirname(here)] } },
  };
}
