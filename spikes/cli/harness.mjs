// Spike 4 harness — full parsing matrix (identical expects for every variant).
import { executeCli } from "./out/engine.component.js";

function call(args) {
  const r = executeCli(args);
  const text = new TextDecoder().decode(Uint8Array.from(r.output ?? []));
  return { success: r.success === true, text, error: r.error ?? null };
}

function expectOk(args, want) {
  const r = call(args);
  const ok = r.success && r.text === want;
  console.log(
    `  ${JSON.stringify(args)} -> ${JSON.stringify(r.text)}` +
      (ok ? "  OK" : `  FAIL (want ${JSON.stringify(want)}, err=${r.error})`),
  );
  return ok;
}

function expectErr(args) {
  const r = call(args);
  const ok = !r.success;
  console.log(
    `  ${JSON.stringify(args)} -> success=${r.success}` +
      (ok ? "  OK (error)" : `  FAIL (wanted error, got ${JSON.stringify(r.text)})`),
  );
  return ok;
}

let ok = true;
ok = expectOk(["greet", "--name", "world"], "hello world") && ok;
ok = expectOk(["greet", "--name=world"], "hello world") && ok;
ok = expectOk(["greet", "-name", "world"], "hello world") && ok;
ok = expectOk(["greet", "world"], "hello world") && ok;
ok = expectOk(["greet", "--name", "x", "--shout"], "HELLO X") && ok;
ok = expectOk(["greet", "--name", "x", "--times", "2"], "hello x hello x") && ok;
ok = expectOk(["greet", "--times", "2", "--name", "x"], "hello x hello x") && ok;
ok = expectOk(["count", "-n", "3"], "count=3 step=1") && ok;
ok = expectOk(["count", "-n", "3", "--step", "2"], "count=3 step=2") && ok;
ok = expectErr(["count", "-n", "notanint"]) && ok;
ok = expectErr(["count"]) && ok;
ok = expectErr(["greet", "--nope", "x"]) && ok;
ok = expectErr(["bogus"]) && ok;
ok = expectErr([]) && ok;
ok = expectOk(["host", "add", "web"], "host+web") && ok;
ok = expectErr(["host", "add"]) && ok;
ok = expectErr(["host", "bogus"]) && ok;

if (!ok) {
  console.error("FAIL");
  process.exit(1);
}
console.log("PASS");
