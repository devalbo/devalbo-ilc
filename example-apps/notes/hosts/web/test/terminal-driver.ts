// Drives the in-page terminal with NO engine, and exposes the request bytes it
// built — which is the point of the exercise.
//
// The terminal is the SECOND consumer of the generated command surface. The
// first is the Go CLI. Parsing happens before the boundary the parity check
// guards, so nothing else in this repo can notice if the two disagree about what
// `create --title "Buy milk"` means. Exporting the encoded bytes lets a test
// compare them against what the Go runner builds for the same line.
import { createFakePort, ok } from "@devalbo/ilc-web/testing";
import { complete, createTerminal } from "@devalbo/ilc-web/terminal";
import type { Terminal } from "@devalbo/ilc-web/terminal";

import {
  CreateRecordResponse,
  ListRecordsResponse,
  DeleteRecordResponse,
} from "@gen/notes/v1/commands.pb";
import { NotesServiceCLI } from "@gen/notes/v1/commands.cli.pb";
import {
  MethodCreateRecord,
  MethodDeleteRecord,
  MethodListRecords,
} from "@gen/notes/v1/commands.registry.pb";

let fake: ReturnType<typeof createFakePort>;
let term: Terminal;

export function setup(): void {
  fake = createFakePort({
    [MethodCreateRecord]: ok(
      CreateRecordResponse.toBinary({
        record: { id: "buy-milk", title: "Buy milk" },
        path: "records/buy-milk.json",
      }),
    ),
    [MethodListRecords]: ok(
      ListRecordsResponse.toBinary({
        records: [{ id: "buy-milk", title: "Buy milk" }],
      }),
    ),
    [MethodDeleteRecord]: ok(DeleteRecordResponse.toBinary({ deleted: true })),
  });

  term = createTerminal({
    port: fake.port,
    commands: NotesServiceCLI,
    // A fixed clock, so the encoded bytes are comparable run to run and tier to
    // tier — the host supplies it because the engine has no clock capability.
    fill: (cmd, values) => {
      if (cmd.method === MethodCreateRecord && !values["created-at"]?.length) {
        values["created-at"] = ["1700000000"];
      }
    },
    render: {
      [MethodCreateRecord]: (bytes) => {
        const r = CreateRecordResponse.fromBinary(bytes);
        return `created ${r.record?.id} -> ${r.path}`;
      },
      [MethodListRecords]: (bytes) => {
        const r = ListRecordsResponse.fromBinary(bytes);
        const recs = r.records ?? [];
        return recs.length === 0
          ? "(no notes)"
          : recs.map((x) => `${x.id}  ${x.title}`).join("\n");
      },
      [MethodDeleteRecord]: (bytes) =>
        DeleteRecordResponse.fromBinary(bytes).deleted ? "deleted" : "no such note",
    },
  });
}

export async function run(line: string): Promise<string> {
  return term.run(line);
}

/** The request bytes of the last command, hex — for the parse-vector diff. */
export function lastRequestHex(): string {
  const call = fake.calls[fake.calls.length - 1];
  if (!call) return "";
  return Array.from(call.request)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export function projection(): string {
  return term.projection();
}

/** Completion candidates for a partial line — Phase 5's payoff, testable. */
export function completions(line: string): { candidates: string[]; prefix: string } {
  return complete(line, NotesServiceCLI);
}
