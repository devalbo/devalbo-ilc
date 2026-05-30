import type { Environment } from "./generated/types.js";

export async function hello(env: Environment): Promise<void> {
  const consoleIo = await env.consoleIo();
  await consoleIo.info("hello from ILC");
}
