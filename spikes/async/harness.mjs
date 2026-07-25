// Spike 5 Rich/CM harness — one logged result per matrix assertion (R1.* / R2.*).
// See README.md test execution matrix. No ILC async shims.
//
// Run under: node --experimental-wasm-jspi harness.mjs  (Node ≥24; see scripts/spike-async.sh)
import { executeCli as executeCliSync } from "./out-sync/engine.component.js";

const DELAY_MS = 50;
const TIMEOUT_MS = 5000;
const want = `ok:${DELAY_MS}`;

const results = []; // { id, status, detail }

function record(id, status, detail = "") {
  results.push({ id, status, detail });
  const mark = status === "PASS" ? "PASS" : status === "SKIP" ? "SKIP" : "FAIL";
  console.log(`  [${mark}] ${id}${detail ? " — " + detail : ""}`);
}

function decode(res) {
  return new TextDecoder().decode(Uint8Array.from(res?.output ?? []));
}

async function withTimeout(p, ms, label) {
  let t;
  const timeout = new Promise((_, rej) => {
    t = setTimeout(() => rej(new Error(`${label}: timed out after ${ms}ms`)), ms);
  });
  try {
    return await Promise.race([p, timeout]);
  } finally {
    clearTimeout(t);
  }
}

function skipRest(ids, because) {
  for (const id of ids) record(id, "SKIP", because);
}

/** F-R-sync → R1.1 … R1.4 — negative control: sync jco cannot lower a Promise import. */
async function runR1() {
  console.log("\n== F-R-sync (jco sync transpile) ==");
  let ticks = 0;
  const iv = setInterval(() => {
    ticks++;
  }, 10);
  let res;
  try {
    res = await withTimeout(
      Promise.resolve().then(() => executeCliSync(["wait", String(DELAY_MS)])),
      TIMEOUT_MS,
      "R1",
    );
    clearInterval(iv);
    record("R1.1", "PASS", "call completed");
  } catch (e) {
    clearInterval(iv);
    record("R1.1", "FAIL", String(e.message || e));
    skipRest(["R1.2", "R1.3", "R1.4"], "blocked by R1.1");
    return;
  }
  if (res.success === true) record("R1.2", "PASS");
  else record("R1.2", "FAIL", `success=${res.success}`);

  const text = decode(res);
  if (text === want) record("R1.3", "PASS");
  else record("R1.3", "FAIL", `got ${JSON.stringify(text)}`);

  if (ticks > 0) record("R1.4", "PASS", `ticks=${ticks}`);
  else record("R1.4", "FAIL", "ticks=0");
}

/** F-R-jspi → R2.1 … R2.6 — stock path: jco JSPI + async import/export. */
async function runR2() {
  console.log("\n== F-R-jspi (jco --async-mode jspi + async-exports) ==");
  const hasJspi =
    typeof WebAssembly.Suspending === "function" ||
    typeof WebAssembly.promising === "function";
  if (!hasJspi) {
    record(
      "R2.1",
      "FAIL",
      `no Suspending/promising (node=${process.version}; need ≥24 + --experimental-wasm-jspi)`,
    );
    skipRest(
      ["R2.2", "R2.3", "R2.4", "R2.5", "R2.6"],
      "blocked by R2.1",
    );
    return;
  }
  record("R2.1", "PASS", `node=${process.version}`);

  let executeCli;
  try {
    const mod = await import("./out-jspi/engine.component.js");
    executeCli = mod.executeCli;
  } catch (e) {
    record("R2.2", "FAIL", String(e.message || e));
    skipRest(["R2.3", "R2.4", "R2.5", "R2.6"], "blocked by R2.2");
    return;
  }
  if (typeof executeCli !== "function") {
    record("R2.2", "FAIL", "executeCli missing");
    skipRest(["R2.3", "R2.4", "R2.5", "R2.6"], "blocked by R2.2");
    return;
  }
  record("R2.2", "PASS");

  let ticks = 0;
  const iv = setInterval(() => {
    ticks++;
  }, 10);
  let res;
  try {
    // With --async-exports execute-cli, jco returns Promise<CommandResult>.
    res = await withTimeout(
      Promise.resolve(executeCli(["wait", String(DELAY_MS)])),
      TIMEOUT_MS,
      "R2",
    );
    clearInterval(iv);
    record("R2.3", "PASS", "call completed");
  } catch (e) {
    clearInterval(iv);
    record("R2.3", "FAIL", String(e.message || e));
    skipRest(["R2.4", "R2.5", "R2.6"], "blocked by R2.3");
    return;
  }

  if (res.success === true) record("R2.4", "PASS");
  else record("R2.4", "FAIL", `success=${res.success}`);

  const text = decode(res);
  if (text === want) record("R2.5", "PASS");
  else record("R2.5", "FAIL", `got ${JSON.stringify(text)}`);

  if (ticks > 0) record("R2.6", "PASS", `ticks=${ticks}`);
  else record("R2.6", "FAIL", "ticks=0");
}

await runR1();
await runR2();

const r2 = results.filter((r) => r.id.startsWith("R2."));
const r2Pass = r2.length > 0 && r2.every((r) => r.status === "PASS");
const richRan = results.some((r) => r.status === "FAIL" || r.status === "PASS");

console.log("\n-------------------------------------------------");
console.log("matrix:");
for (const r of results) {
  console.log(`  ${r.id}\t${r.status}${r.detail ? "\t" + r.detail : ""}`);
}

// RICH greens on the JSPI path (the actual async question). R1 is a negative
// control: sync transpile cannot await a Promise import — expected FAIL.
if (r2Pass) {
  console.log("RICH=GREEN");
  console.log(
    "JSPI path green (R2.*). R1 sync transpile remains a negative control (Promise→type error).",
  );
  process.exit(0);
}
if (richRan) {
  console.log("RICH=YELLOW");
  console.log(
    "Ecosystem gap: need Node ≥24 + --experimental-wasm-jspi + jco --async-mode jspi with async-imports and async-exports",
  );
  process.exit(0);
}
console.log("RICH=RED");
process.exit(1);
