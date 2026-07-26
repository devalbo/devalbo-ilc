// go-lite oneof spike harness. Feeds argv, asserts the dispatched result. Each
// call exercises: oneof construct → MarshalVT/UnmarshalVT + MarshalJSON/UnmarshalJSON
// round-trips → tag→handler map dispatch, all inside the TinyGo wasip2 engine.
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
  console.log(`  ${JSON.stringify(args)} -> ${JSON.stringify(r.text)}` + (good ? "  OK" : `  FAIL (want ${JSON.stringify(want)}, err=${r.err})`));
  pass = good && pass;
}
function expectErr(args) {
  const r = call(args);
  const good = !r.ok;
  console.log(`  ${JSON.stringify(args)} -> success=${r.ok}` + (good ? "  OK (error)" : `  FAIL (wanted error, got ${JSON.stringify(r.text)})`));
  pass = good && pass;
}

expectOk(["greet", "world"], "hello world"); // Command_Greet arm
expectOk(["greet"], "hello ");               // empty oneof field
expectOk(["add", "2", "3"], "5");            // Command_Add arm (int32 fields)
expectOk(["add", "-4", "10"], "6");          // signed round-trip
expectErr(["bogus"]);                         // unknown verb
expectErr([]);                                // no command

if (!pass) {
  console.error("FAIL");
  process.exit(1);
}
console.log("PASS");
