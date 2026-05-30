import type { ConsoleIo } from "../generated/types.js";

export type ConsoleIoCallbacks = {
  onInfo: (message: string) => void | Promise<void>;
  onError: (message: string) => void | Promise<void>;
  onReadLine: () => string | null | Promise<string | null>;
};

/** Build a `ConsoleIo` from host-provided sinks (SDK internal; hosts use factory functions). */
export function consoleIoFromCallbacks(callbacks: ConsoleIoCallbacks): ConsoleIo {
  return {
    async info(message: string) {
      await callbacks.onInfo(message);
    },
    async error(message: string) {
      await callbacks.onError(message);
    },
    async readLine() {
      return await callbacks.onReadLine();
    },
  };
}
