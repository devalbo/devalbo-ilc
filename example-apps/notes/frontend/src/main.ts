// notes' web UI.
//
// The form IS this tier's command parser (Decision 28): it collects fields,
// encodes a typed request, and hands the bytes to the engine. What create/list/
// delete MEAN lives in engine/ — shared with the CLI — which is why a note
// written here reads identically from `notes list` in a terminal.
//
// No business logic here. Note in particular that the UI does not know records
// are JSON files, or where they live: that is the engine's split-storage
// decision, and this tier only sees commands.
import { execute } from "@devalbo/ilc-web/api";

import {
  CreateRecordRequest,
  CreateRecordResponse,
  DeleteRecordRequest,
  ListRecordsRequest,
  ListRecordsResponse,
} from "@gen/notes/v1/commands.pb";
import {
  MethodCreateRecord,
  MethodDeleteRecord,
  MethodListRecords,
} from "@gen/notes/v1/commands.registry.pb";

const out = document.getElementById("out") as HTMLPreElement;
const list = document.getElementById("list") as HTMLUListElement;
const count = document.querySelector('[data-testid="count"]') as HTMLElement;
const titleInput = document.getElementById("title") as HTMLInputElement;
const bodyInput = document.getElementById("body") as HTMLInputElement;

function say(line: string) {
  out.textContent += line + "\n";
}

async function refresh() {
  const r = await execute(MethodListRecords, ListRecordsRequest.toBinary({}));
  if (!r.success) {
    say(`list failed: ${r.error ?? "(no message)"}`);
    return;
  }
  const { records = [] } = ListRecordsResponse.fromBinary(r.output);
  count.textContent = String(records.length);
  list.replaceChildren(
    ...records.map((rec) => {
      const li = document.createElement("li");
      li.dataset.id = rec.id ?? "";
      li.textContent = `${rec.id} — ${rec.title}`;
      const del = document.createElement("button");
      del.textContent = "delete";
      del.dataset.testid = `delete-${rec.id}`;
      del.addEventListener("click", () => remove(rec.id ?? ""));
      li.append(" ", del);
      return li;
    }),
  );
}

async function create() {
  // The HOST supplies the clock, exactly as the CLI does with time.Now(): the
  // engine has no clock capability, because a browser tab and an MCU disagree
  // about what one even is.
  const request = CreateRecordRequest.toBinary({
    title: titleInput.value,
    body: bodyInput.value,
    createdAt: BigInt(Math.floor(Date.now() / 1000)),
  });
  const r = await execute(MethodCreateRecord, request);
  if (!r.success) {
    say(`create failed: ${r.error ?? "(no message)"}`);
    return;
  }
  const resp = CreateRecordResponse.fromBinary(r.output);
  say(`created ${resp.record?.id} -> ${resp.path}`);
  titleInput.value = "";
  bodyInput.value = "";
  await refresh();
}

async function remove(id: string) {
  const r = await execute(MethodDeleteRecord, DeleteRecordRequest.toBinary({ id }));
  if (!r.success) {
    say(`delete failed: ${r.error ?? "(no message)"}`);
    return;
  }
  say(`deleted ${id}`);
  await refresh();
}

document.getElementById("create")!.addEventListener("click", () => {
  create().catch((e) => say(`ERROR: ${(e as Error).message}`));
});

refresh().catch((e) => say(`ERROR: ${(e as Error).message}`));
say("ready — the engine boots on the first command");
