// How dlc prints each response for its TEXT front ends (terminal, inspector).
//
// dlc is an app like any other (AGENTS.md §3), so this is the same file every
// scaffolded app gets — shared between the two routes rather than written twice,
// because two copies are two places for one response to look different.
//
// A slot renders, it never decides (Decision 34): each of these formats what the
// engine returned and works nothing out.
import type { Renderer } from "@devalbo/ilc-web/terminal";

import { NewResponse, EchoResponse } from "@gen/devalbo/dlc/v1/commands.pb";
import { MethodNew, MethodEcho } from "@gen/devalbo/dlc/v1/commands.registry.pb";

import {
  ExportFsResponse,
  ImportFsResponse,
  ResetFsResponse,
  VersionResponse,
} from "@gen/devalbo/ilc/v1/platform.pb";
import {
  MethodExportFs,
  MethodImportFs,
  MethodResetFs,
  MethodVersion,
} from "@gen/devalbo/ilc/v1/platform.registry.pb";

export const dlcRenderers: Record<number, Renderer | undefined> = {
  [MethodNew]: (b) => {
    const r = NewResponse.fromBinary(b);
    const files = r.files ?? [];
    return [
      `scaffold ${r.path}`,
      ...files.map((f) => `  + ${r.path}/${f}`),
      "",
      "next:",
      `  cd ${r.path} && devbox shell`,
      "  make gen && go mod tidy && make verify",
    ].join("\n");
  },
  [MethodEcho]: (b) => EchoResponse.fromBinary(b).text ?? "",

  // The INHERITED verbs. dlc gets these by being an ILC app, exactly as notes
  // and tictactoe do; only their printing is ours.
  [MethodVersion]: (b) => VersionResponse.fromBinary(b).version ?? "",
  [MethodExportFs]: (b) =>
    new TextDecoder().decode(ExportFsResponse.fromBinary(b).bundle ?? new Uint8Array()),
  [MethodImportFs]: (b) => {
    const files = ImportFsResponse.fromBinary(b).files ?? [];
    return files.length === 0 ? "(nothing imported)" : files.map((f) => "  + " + f).join("\n");
  },
  // Top-level ENTRIES removed, not a file count — see the note in the platform's
  // handleResetFs. Naming them beats a number that reads as though something
  // survived a destructive command.
  [MethodResetFs]: (b) => {
    const removed = ResetFsResponse.fromBinary(b).removed ?? [];
    return removed.length === 0 ? "nothing to remove" : removed.map((r) => "  - " + r).join("\n");
  },
};
