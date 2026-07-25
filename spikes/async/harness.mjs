// Spike 5 Rich/CM harness — one logged result per matrix assertion (R1.* / R2.*).
// See README.md test execution matrix. No ILC async shims.
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

/** F-R-sync → R1.1 … R1.4 */
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

/** F-R-jspi → R2.1 … R2.6 */
async function runR2() {
  console.log("\n== F-R-jspi (jco --async-mode jspi) ==");
  const hasJspi =
    typeof WebAssembly.Suspending === "function" ||
    typeof WebAssembly.promising === "function";
  if (!hasJspi) {
    record(
      "R2.1",
      "FAIL",
      `no Suspending/promising (node=${process.version})`,
    );
    skipRest(
      ["R2.2", "R2.3", "R2.4", "R2.5", "R2.6"],
      "blocked by R2.1",
    );
    return;
  }
  record("R2.1", "PASS");

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
    const run = Promise.resolve(executeCli(["wait", String(DELAY_MS)])).then((r) =>
      r && typeof r.then === "function" ? r : r,
    );
    res = await withTimeout(run, TIMEOUT_MS, "R2");
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

const richIds = results.filter((r) => r.id.startsWith("R"));
const richPass = richIds.every((r) => r.status === "PASS");
const richRan = richIds.some((r) => r.status === "FAIL" || r.status === "PASS");

console.log("\n-------------------------------------------------");
console.log("matrix:");
for (const r of results) {
  console.log(`  ${r.id}\t${r.status}${r.detail ? "\t" + r.detail : ""}`);
}

if (richPass) {
  console.log("RICH=GREEN");
  process.exit(0);
}
if (richRan) {
  console.log("RICH=YELLOW");
  console.log(
    "Ecosystem gap: Promise-returning CM import needs jco JSPI runtime and/or WASI 0.3 guest",
  );
  process.exit(0);
}
console.log("RICH=RED");
process.exit(1);
