// world root:component/root
export type CommandResult = import('./interfaces/devalbo-ilc-types.js').CommandResult;
export type * as DevalboIlcEvents from './interfaces/devalbo-ilc-events.js'; // import devalbo:ilc/events
export type * as DevalboIlcTypes from './interfaces/devalbo-ilc-types.js'; // import devalbo:ilc/types
export type * as WasiCliEnvironment020 from './interfaces/wasi-cli-environment.js'; // import wasi:cli/environment@0.2.0
export type * as WasiCliStderr020 from './interfaces/wasi-cli-stderr.js'; // import wasi:cli/stderr@0.2.0
export type * as WasiCliStdin020 from './interfaces/wasi-cli-stdin.js'; // import wasi:cli/stdin@0.2.0
export type * as WasiCliStdout020 from './interfaces/wasi-cli-stdout.js'; // import wasi:cli/stdout@0.2.0
export type * as WasiClocksMonotonicClock020 from './interfaces/wasi-clocks-monotonic-clock.js'; // import wasi:clocks/monotonic-clock@0.2.0
export type * as WasiClocksWallClock020 from './interfaces/wasi-clocks-wall-clock.js'; // import wasi:clocks/wall-clock@0.2.0
export type * as WasiFilesystemPreopens020 from './interfaces/wasi-filesystem-preopens.js'; // import wasi:filesystem/preopens@0.2.0
export type * as WasiFilesystemTypes020 from './interfaces/wasi-filesystem-types.js'; // import wasi:filesystem/types@0.2.0
export type * as WasiIoError020 from './interfaces/wasi-io-error.js'; // import wasi:io/error@0.2.0
export type * as WasiIoStreams020 from './interfaces/wasi-io-streams.js'; // import wasi:io/streams@0.2.0
export type * as WasiRandomRandom020 from './interfaces/wasi-random-random.js'; // import wasi:random/random@0.2.0
export function execute(method: number, request: Uint8Array): CommandResult;
