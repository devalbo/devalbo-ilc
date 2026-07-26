// B2 wasm-parity harness (Decision 26). Runs each golden argv vector through the
// jco-transpiled engine component and prints `<success>\t<base64(output)>` per
// line. scripts/verify-parity.sh runs the SAME vectors through the native `dlc`
// and diffs the two streams — identical output proves the one engine.Execute
// behaves the same linked-in-process and compiled to a wasip2 component.
import { readFileSync } from "node:fs";
import { executeCli } from "./out/engine.component.js";

const vectors = JSON.parse(readFileSync(process.argv[2], "utf8"));
for (const args of vectors) {
  const r = executeCli(args);
  const out = Buffer.from(Uint8Array.from(r.output ?? []));
  process.stdout.write(`${r.success === true}\t${out.toString("base64")}\n`);
}
