// B2 wasm-parity harness (Decision 26). Runs golden vectors through the
// jco-transpiled engine component and prints one result line each;
// scripts/verify-parity.sh runs the SAME vectors through the native engine and
// diffs the two streams — identical output proves the one engine behaves the
// same linked in-process and compiled to a wasip2 component.
//
// ONE boundary now — `execute(method, request)` (Decisions 28/31). The argv
// stream retired with the execute-cli shim: hosts parse and build requests, so
// there is no second thing to compare.
//
// Each line is `<success>\t<base64(output)>\t<error>`. The error string is part
// of the diff on purpose: the failure modes (envelope errors, unregistered ids,
// undecodable requests) are as much of the contract as the successes, and TinyGo
// must word them exactly as native Go does.
//
// FILESYSTEM ROOT: commands like `new` write real files, so the guest is given a
// preopen rooted at $PARITY_ROOT — the wasm-side equivalent of the native run's
// cwd (§5.2: the host binds the root, the engine just uses `os`). Preopens must
// be set BEFORE the component is instantiated, hence the dynamic import below.
import { readFileSync } from "node:fs";
import { _setPreopens } from "@bytecodealliance/preview2-shim/filesystem";

const [, , mode, vectorFile] = process.argv;
const root = process.env.PARITY_ROOT;
if (!root) {
  console.error("harness.mjs: PARITY_ROOT must point at an isolated directory");
  process.exit(2);
}
// Replaces the shim's default "/" -> "/" mapping, so the guest cannot reach
// outside the sandbox the runner gave it.
_setPreopens({ "/": root });

const { execute } = await import("./out/engine.component.js");

const vectors = JSON.parse(readFileSync(vectorFile, "utf8"));
const b64 = (out) => Buffer.from(Uint8Array.from(out ?? [])).toString("base64");

if (mode === "method") {
  for (const { method, request } of vectors) {
    const r = execute(method, Uint8Array.from(Buffer.from(request, "hex")));
    process.stdout.write(`${r.success === true}\t${b64(r.output)}\t${r.error ?? ""}\n`);
  }
} else {
  console.error("usage: harness.mjs method <vectors.json>");
  process.exit(2);
}
