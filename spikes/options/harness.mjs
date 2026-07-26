// Custom-options spike — TinyGo guest round-trips messages whose .proto carries
// method_id / field options. Options themselves are not read in-engine.
import { executeCli } from "./out/engine.component.js";

function call(args) {
  const r = executeCli(args);
  return {
    ok: r.success === true,
    text: new TextDecoder().decode(Uint8Array.from(r.output ?? [])),
    err: r.error ?? null,
  };
}

let pass = true;
function expectOk(args, want) {
  const r = call(args);
  const good = r.ok && r.text === want;
  console.log(
    `  ${JSON.stringify(args)} -> ${JSON.stringify(r.text)}` +
      (good ? "  OK" : `  FAIL (want ${JSON.stringify(want)}, err=${r.err})`),
  );
  pass = good && pass;
}
function expectErr(args) {
  const r = call(args);
  const good = !r.ok;
  console.log(
    `  ${JSON.stringify(args)} -> success=${r.ok}` +
      (good ? "  OK (error)" : `  FAIL (wanted error, got ${JSON.stringify(r.text)})`),
  );
  pass = good && pass;
}

console.log("== guest (go-lite + TinyGo; options ignored at runtime) ==");
expectOk(["greet", "world"], "hello world");
expectOk(["greet", "x", "2"], "hello x hello x");
expectOk(["add", "2", "3"], "5");
expectOk(["add", "-4", "10"], "6");
expectErr(["bogus"]);
expectErr([]);

if (!pass) {
  console.error("GUEST=RED");
  process.exit(1);
}
console.log("GUEST=GREEN");
