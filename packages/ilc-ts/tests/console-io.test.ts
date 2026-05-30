import test from "node:test";
import assert from "node:assert/strict";

import { hello } from "../src/hello.js";
import { createTestEnvironment } from "../src/hosts/test.js";

test("test host captures info and error logs", async () => {
  const env = createTestEnvironment();
  const cio = await env.consoleIo();
  await cio.info("hi");
  await cio.error("boom");
  assert.deepEqual(env.logs, [
    { level: "info", message: "hi" },
    { level: "error", message: "boom" },
  ]);
});

test("readLine drains queued stdin then yields null at EOF", async () => {
  const env = createTestEnvironment({ stdin: ["one", "two"] });
  const cio = await env.consoleIo();
  assert.equal(await cio.readLine(), "one");
  assert.equal(await cio.readLine(), "two");
  assert.equal(await cio.readLine(), null);
});

test("hello handler runs against the test host", async () => {
  const env = createTestEnvironment();
  await hello(env);
  assert.deepEqual(env.logs, [{ level: "info", message: "hello from ILC" }]);
});
