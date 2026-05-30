import type { Environment } from "../generated/types.js";
import { consoleIoFromCallbacks } from "./callbacks.js";

/** Browser or DevTools: `console.log` / `console.error`, `readLine` via `prompt()`. */
export function createBrowserEnvironment(): Environment {
  const consoleIo = consoleIoFromCallbacks({
    onInfo: (message) => {
      globalThis.console.log(message);
    },
    onError: (message) => {
      globalThis.console.error(message);
    },
    onReadLine: async () => {
      const line = globalThis.prompt("");
      if (line === null) {
        return null;
      }
      return line.length > 0 ? line : null;
    },
  });

  return {
    async consoleIo() {
      return consoleIo;
    },
  };
}

/** Paste-friendly alias for DevTools REPL usage. */
export const createDevToolsEnvironment = createBrowserEnvironment;
