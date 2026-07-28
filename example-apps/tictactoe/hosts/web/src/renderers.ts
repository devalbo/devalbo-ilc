// How tic-tac-toe prints each response for the TEXT front ends (terminal and
// inspector) — shared between them, and matching the native slot's board.
//
// A slot renders, it never decides: every one of these formats what the engine
// returned and works nothing out.
import type { Renderer } from "@devalbo/ilc-web/terminal";

import {
  GetStateResponse,
  NewGameResponse,
  PlayResponse,
} from "@gen/tictactoe/v1/commands.pb";
import {
  MethodGetState,
  MethodNewGame,
  MethodPlay,
} from "@gen/tictactoe/v1/commands.registry.pb";

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

import { projectionOf } from "./view";

export const gameRenderers: Record<number, Renderer | undefined> = {
  [MethodGetState]: (b) => projectionOf(GetStateResponse.fromBinary(b).state ?? null),
  [MethodPlay]: (b) => projectionOf(PlayResponse.fromBinary(b).state ?? null),
  [MethodNewGame]: (b) => projectionOf(NewGameResponse.fromBinary(b).state ?? null),

  // Inherited verbs — `dlc gen` supplies these messages, the web tier's version
  // of the Go module dependency.
  [MethodVersion]: (b) => VersionResponse.fromBinary(b).version ?? "",
  [MethodExportFs]: (b) =>
    new TextDecoder().decode(ExportFsResponse.fromBinary(b).bundle ?? new Uint8Array()),
  [MethodImportFs]: (b) => {
    const files = ImportFsResponse.fromBinary(b).files ?? [];
    return files.length === 0 ? "(nothing imported)" : files.map((f) => "  + " + f).join("\n");
  },
  [MethodResetFs]: (b) =>
    `removed ${(ResetFsResponse.fromBinary(b).removed ?? []).length} file(s)`,
};
