import type { ConsoleIo, Environment } from "../generated/types.js";
import { consoleIoFromCallbacks } from "./callbacks.js";

export type LogEntry = {
  level: "info" | "error";
  message: string;
};

export type TestEnvironment = Environment & {
  readonly logs: LogEntry[];
  /** Lines consumed by `readLine` (in order). */
  readonly stdinLines: string[];
};

export type TestEnvironmentOptions = {
  /** Queued stdin lines; exhausted queue yields `null` (EOF). */
  stdin?: string[];
};

export function createTestEnvironment(
  options: TestEnvironmentOptions = {},
): TestEnvironment {
  const logs: LogEntry[] = [];
  const stdinLines = [...(options.stdin ?? [])];
  let index = 0;

  const consoleIo: ConsoleIo = consoleIoFromCallbacks({
    onInfo: (message) => {
      logs.push({ level: "info", message });
    },
    onError: (message) => {
      logs.push({ level: "error", message });
    },
    onReadLine: async () => {
      if (index >= stdinLines.length) {
        return null;
      }
      return stdinLines[index++] ?? null;
    },
  });

  return {
    logs,
    get stdinLines() {
      return [...stdinLines];
    },
    async consoleIo() {
      return consoleIo;
    },
  };
}
