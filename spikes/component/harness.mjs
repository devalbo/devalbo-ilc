// Spike 1 — component round-trip harness (T-B1.1).
// Imports the jco-transpiled component and calls the engine's execute-cli export,
// asserting it returns "ok:hi". Run from this directory (npm deps resolve locally).
import { executeCli } from "./out/engine.component.js";

const res = executeCli(["hi"]);
const text = new TextDecoder().decode(Uint8Array.from(res.output ?? []));
const ok = res.success === true && text === "ok:hi";

console.log(
  `executeCli(["hi"]) -> success=${res.success} output=${JSON.stringify(text)} error=${res.error}`,
);

if (!ok) {
  console.error('FAIL: expected success=true, output="ok:hi"');
  process.exit(1);
}
console.log("PASS: ok:hi");
