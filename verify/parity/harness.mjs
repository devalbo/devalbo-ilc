// B2 wasm-parity harness (Decision 26). Runs golden vectors through the
// jco-transpiled engine component and prints one result line each;
// scripts/verify-parity.sh runs the SAME vectors through the native engine and
// diffs the two streams — identical output proves the one engine behaves the
// same linked in-process and compiled to a wasip2 component.
//
// Two boundaries, two vector files, two line formats:
//
//   argv    (the bootstrap execute-cli shim)      `<success>\t<base64(output)>`
//   method  (execute(method, request) — the real  `<success>\t<base64(output)>\t<error>`
//            boundary, Decision 28/31)
//
// The method stream carries the error string too: on that boundary the failures
// (envelope errors, unregistered ids, undecodable requests) are as much of the
// contract as the successes, and TinyGo must word them exactly as native Go does.
import { readFileSync } from "node:fs";
import { execute, executeCli } from "./out/engine.component.js";

const [, , mode, vectorFile] = process.argv;
const vectors = JSON.parse(readFileSync(vectorFile, "utf8"));
const b64 = (out) => Buffer.from(Uint8Array.from(out ?? [])).toString("base64");

if (mode === "argv") {
  for (const args of vectors) {
    const r = executeCli(args);
    process.stdout.write(`${r.success === true}\t${b64(r.output)}\n`);
  }
} else if (mode === "method") {
  for (const { method, request } of vectors) {
    const r = execute(method, Uint8Array.from(Buffer.from(request, "hex")));
    process.stdout.write(`${r.success === true}\t${b64(r.output)}\t${r.error ?? ""}\n`);
  }
} else {
  console.error("usage: harness.mjs <argv|method> <vectors.json>");
  process.exit(2);
}
