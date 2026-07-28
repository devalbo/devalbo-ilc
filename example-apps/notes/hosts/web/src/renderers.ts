// How notes prints each response — shared by every front end on this tier.
//
// A SLOT RENDERS; IT NEVER DECIDES (Decision 34). Nothing here works out what a
// command means; it formats what the engine returned.
//
// Shared between the terminal and the inspector on purpose. Two copies would be
// two places for the same response to be formatted differently depending on
// which page you happened to be looking at — and the one nobody looks at is the
// one that drifts.
import type { Renderer } from "@devalbo/ilc-web/terminal";

import {
  CreateRecordResponse,
  DeleteRecordResponse,
  ListRecordsResponse,
  OpenRecordResponse,
} from "@gen/notes/v1/commands.pb";
import {
  MethodCreateRecord,
  MethodDeleteRecord,
  MethodListRecords,
  MethodOpenRecord,
} from "@gen/notes/v1/commands.registry.pb";

// The INHERITED platform messages. `dlc gen` copies these in from the platform
// checkout — the Go host imports the equivalent straight from the platform
// module, and this is the web tier's version of that dependency.
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


/** First line of a body, shortened — a list is an index, not the content. */
function excerpt(body: string, max = 40): string {
  const line = body.split("\n")[0] ?? "";
  return line.length > max ? line.slice(0, max - 1) + "…" : line;
}

export const notesRenderers: Record<number, Renderer | undefined> = {
  [MethodCreateRecord]: (bytes) => {
    const r = CreateRecordResponse.fromBinary(bytes);
    return `created ${r.record?.id} -> ${r.path}`;
  },

  [MethodListRecords]: (bytes) => {
    const recs = ListRecordsResponse.fromBinary(bytes).records ?? [];
    if (recs.length === 0) return "(no notes)";
    // A HEADER, and the body excerpt. Without them this printed two columns of
    // id and title — usually identical, because the id is slugged from the
    // title — so it read as the title being duplicated into the body. The data
    // was always right; the rendering was not.
    const rows = [
      { id: "ID", title: "TITLE", body: "BODY" },
      ...recs.map((r) => ({
        id: r.id ?? "",
        title: r.title ?? "",
        body: excerpt(r.body ?? ""),
      })),
    ];
    const w = (k: "id" | "title") => Math.max(...rows.map((r) => r[k].length));
    const [wid, wtitle] = [w("id"), w("title")];
    return rows
      .map((r) => `${r.id.padEnd(wid)}  ${r.title.padEnd(wtitle)}  ${r.body}`.trimEnd())
      .join("\n");
  },

  [MethodOpenRecord]: (bytes) => {
    const r = OpenRecordResponse.fromBinary(bytes).record;
    return `# ${r?.title}\n\n${r?.body}`;
  },

  [MethodDeleteRecord]: (bytes) =>
    DeleteRecordResponse.fromBinary(bytes).deleted ? "deleted" : "no such note",

  // The INHERITED verbs. An app gets these by being an ILC app; only their
  // printing is ours. No button in the UI exposes them, which is much of why a
  // terminal and an inspector are worth having.
  //
  // `version` is the platform's, and it reports what `dlc gen` put in
  // dlcconfig from dlc.toml — so every app answers `version` without writing a
  // handler, and the answer is the manifest's, not a string someone typed.
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
};
