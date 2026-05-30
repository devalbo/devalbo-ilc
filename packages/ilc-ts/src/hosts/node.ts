import * as readline from "node:readline/promises";
import { stdin as input, stdout as output } from "node:process";

import type { ConsoleIo, Environment } from "../generated/types.js";
import { consoleIoFromCallbacks } from "./callbacks.js";

export type NodeEnvironmentOptions = {
  /** When true, `readLine` always returns `null` (e.g. serverless). */
  nonInteractive?: boolean;
};

export function createNodeEnvironment(
  options: NodeEnvironmentOptions = {},
): Environment {
  let consoleIo: ConsoleIo | undefined;
  let rl: readline.Interface | undefined;

  return {
    async consoleIo() {
      if (!consoleIo) {
        consoleIo = consoleIoFromCallbacks({
          onInfo: (message) => {
            output.write(message + "\n");
          },
          onError: (message) => {
            process.stderr.write(message + "\n");
          },
          onReadLine: async () => {
            if (options.nonInteractive) {
              return null;
            }
            if (!rl) {
              rl = readline.createInterface({ input, output });
            }
            const line = await rl.question("");
            return line.length > 0 ? line : null;
          },
        });
      }
      return consoleIo;
    },
  };
}

/** Alias for Lambda-style hosts where stdin is unavailable. */
export function createServerlessEnvironment(): Environment {
  return createNodeEnvironment({ nonInteractive: true });
}
