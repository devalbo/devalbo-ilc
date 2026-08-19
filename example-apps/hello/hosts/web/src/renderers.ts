// How hello prints each response — shared by every front end here.
//
// A SLOT RENDERS; IT NEVER DECIDES (Decision 34). Nothing here works out what a
// command means; it formats what the engine returned.
//
// Shared between the terminal and the inspector on purpose: two copies would be
// two places for the same response to look different depending on which page you
// were on, and the one nobody looks at is the one that drifts.
import type { Renderer } from "@devalbo/dlc-web/terminal";

import {
  CountResponse,
  GreetResponse,
  LightResponse,
  MathResponse,
  Problem,
} from "@gen/hello/v1/commands.pb";
import {
  MethodCount,
  MethodGreet,
  MethodLight,
  MethodMath,
} from "@gen/hello/v1/commands.registry.pb";

// The INHERITED platform messages. `dlc gen` copies these in from the platform
// checkout — the Go host imports the equivalent straight from the platform
// module, and this is the web tier's version of that dependency.
import {
  ExportFsResponse,
  ImportFsResponse,
  RebuildIndexResponse,
  ResetFsResponse,
  VersionResponse,
} from "@gen/devalbo/ilc/v1/platform.pb";
import {
  MethodExportFs,
  MethodImportFs,
  MethodRebuildIndex,
  MethodResetFs,
  MethodVersion,
} from "@gen/devalbo/ilc/v1/platform.registry.pb";

// problemText spells a Problem for a person. The ENUM crosses the wire and a
// test asserts on it; this is only how it reads.
function problemText(problem: Problem): string {
  switch (problem) {
    case Problem.DIVIDE_BY_ZERO:
      return "cannot divide by zero";
    case Problem.OVERFLOW:
      return "the numbers are too big";
    default:
      return String(problem);
  }
}

export const appRenderers: Record<number, Renderer | undefined> = {
  [MethodGreet]: (bytes) => GreetResponse.fromBinary(bytes).text ?? "",

  // count already streamed every tick to the terminal, so this prints only what
  // a reader could NOT already see — the tally the app kept.
  [MethodCount]: (bytes) => {
    const r = CountResponse.fromBinary(bytes);
    return `(${r.counted ?? 0} ticks)`;
  },

  // THE STRUCTURED ONE. Same facts as the engine printed, as fields — which is
  // what a slot that can decode the schema is for.
  [MethodMath]: (bytes) => {
    const r = MathResponse.fromBinary(bytes);
    // TRUTHY IS ENOUGH: es-lite types an optional enum field without its zero
    // value, so comparing against UNSPECIFIED is a comparison the compiler can
    // prove never matters.
    if (r.problem) {
      // A PROBLEM IS NOT AN ERROR. The command ran and is reporting what it
      // found, so this renders as a result rather than throwing.
      return `${r.expression ?? ""}: ${problemText(r.problem)}`;
    }
    return String(r.result ?? 0);
  },

  [MethodLight]: (bytes) =>
    LightResponse.fromBinary(bytes).shown ? "set" : "this world has no light to set",

  // The inherited verbs. You get these by being an ILC app; only their printing
  // is yours. `version` answers with what `dlc gen` put in dlcconfig from
  // dlc.toml — so the app reports the manifest's version without a handler.
  [MethodVersion]: (bytes) => VersionResponse.fromBinary(bytes).version ?? "",
  [MethodExportFs]: (bytes) =>
    new TextDecoder().decode(ExportFsResponse.fromBinary(bytes).bundle ?? new Uint8Array()),
  [MethodImportFs]: (bytes) => {
    const files = ImportFsResponse.fromBinary(bytes).files ?? [];
    return files.length === 0 ? "(nothing imported)" : files.map((f) => "  + " + f).join("\n");
  },
  // The engine returns the TOP-LEVEL entries removed, not a file count —
  // `records/` is one entry holding many notes. Naming them beats a number that
  // reads as though something survived.
  [MethodResetFs]: (bytes) => {
    const removed = ResetFsResponse.fromBinary(bytes).removed ?? [];
    return removed.length === 0
      ? "nothing to remove"
      : removed.map((r) => "  - " + r).join("\n");
  },
  // The derived index. This app maintains none today, so the verb is marked
  // unavailable — but a renderer is owed by every command in the inherited
  // surface, and the two tiers must print the same thing (host parity).
  [MethodRebuildIndex]: (bytes) =>
    `indexed ${RebuildIndexResponse.fromBinary(bytes).entries ?? 0} entries`,
};
