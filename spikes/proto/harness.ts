// Spike 2 — protobuf cross-decode harness (T-B1.2).
// Calls the wasip2 engine for binary + JSON encodings, checks goldens, then
// decodes both with protobuf-es-lite and asserts field equality.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { executeCli } from "./out/engine.component.js";
// Copied next to this harness by `make spike-proto` so @aptre/* resolves from
// this package's node_modules (Node walks from the importing file, not cwd).
import { SpikeMessage } from "./spike.pb.ts";

const here = dirname(fileURLToPath(import.meta.url));
const wantHex = readFileSync(join(here, "golden.hex"), "utf8").trim();
const wantJSON = readFileSync(join(here, "golden.json"), "utf8").trim();

function call(mode: string): Uint8Array {
  const res = executeCli([mode]);
  if (!res.success) {
    throw new Error(`executeCli([${mode}]) failed: ${res.error}`);
  }
  return Uint8Array.from(res.output ?? []);
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

const bin = call("binary");
const jsonBytes = call("json");
const gotHex = toHex(bin);
const gotJSON = new TextDecoder().decode(jsonBytes).trim();

console.log(`binary hex: ${gotHex}`);
console.log(`json:       ${gotJSON}`);

let fail = 0;
if (gotHex !== wantHex) {
  console.error(`FAIL binary golden\n got: ${gotHex}\nwant: ${wantHex}`);
  fail++;
}
if (gotJSON !== wantJSON) {
  console.error(`FAIL json golden\n got: ${gotJSON}\nwant: ${wantJSON}`);
  fail++;
}

const fromBin = SpikeMessage.fromBinary(bin);
const fromJSON = SpikeMessage.fromJsonString(wantJSON);

const expect = { name: "spike", count: 42, ok: true };
for (const [label, msg] of [
  ["fromBinary", fromBin],
  ["fromJsonString", fromJSON],
] as const) {
  if (msg.name !== expect.name || msg.count !== expect.count || msg.ok !== expect.ok) {
    console.error(`FAIL ${label}: got`, msg, "want", expect);
    fail++;
  } else {
    console.log(`OK ${label}: name=${msg.name} count=${msg.count} ok=${msg.ok}`);
  }
}

if (!SpikeMessage.equals(fromBin, fromJSON)) {
  console.error("FAIL fromBinary !== fromJsonString");
  fail++;
}

if (fail) {
  console.error(`FAIL: ${fail} assertion(s)`);
  process.exit(1);
}
console.log("PASS: binary+json goldens and es-lite cross-decode");
