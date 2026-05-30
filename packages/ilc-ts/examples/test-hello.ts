import { createTestEnvironment } from "../src/hosts/test.js";
import { hello } from "../src/hello.js";

const env = createTestEnvironment();
await hello(env);

const info = env.logs.find((e) => e.level === "info");
if (!info || info.message !== "hello from ILC") {
  console.error("unexpected logs:", env.logs);
  process.exit(1);
}
console.error("ok: test host captured hello");
