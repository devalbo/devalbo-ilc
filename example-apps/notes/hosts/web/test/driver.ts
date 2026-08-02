// A second writer, for the browser test — NOT part of the app.
//
// Nothing in the app imports this: `index.html` loads only `src/main.ts`, so a
// production `vite build` never sees it. The Playwright test pulls it in at run
// time with `import("/test/driver.ts")`, which the dev server transforms like any
// other module — which is how a bare specifier resolves here at all.
//
// Why it exists: the point of events is that the UI repaints for writes IT DID
// NOT MAKE. A test that clicks "create" cannot show that, however the handler is
// written — it only shows that some path from the click leads to a list. Driving
// the engine from outside the UI removes that ambiguity: no handler runs, no code
// in `main.ts` is on the stack, and the list still has to update.
//
// It shares the app's engine rather than starting a second one: both modules
// import the same `@devalbo/dlc-web/api` URL, and ES modules are singletons per
// graph, so this goes through the same worker and the same subscription the page
// already installed.
import { execute } from "@devalbo/dlc-web/api";
import { CreateRecordRequest } from "@gen/notes/v1/commands.pb";
import { MethodCreateRecord } from "@gen/notes/v1/commands.registry.pb";
import { ExportFsRequest, ExportFsResponse } from "@gen/devalbo/ilc/v1/platform.pb";
import { MethodExportFs } from "@gen/devalbo/ilc/v1/platform.registry.pb";

/** Create a note without touching the DOM. Throws on refusal — the test wants a
 *  failure here to look like a failure, not like a missing event. */
export async function createDirect(title: string): Promise<void> {
  const r = await execute(
    MethodCreateRecord,
    CreateRecordRequest.toBinary({
      title,
      body: "",
      // Host-supplied clock, as everywhere else — the engine has none.
      createdAt: BigInt(Math.floor(Date.now() / 1000)),
    }),
  );
  if (!r.success) throw new Error(r.error ?? "create failed");
}

/** The BFT bundle as text, so a test can assert what is and is not in it. */
export async function exportBundle(): Promise<string> {
  const r = await execute(MethodExportFs, ExportFsRequest.toBinary({}));
  if (!r.success) throw new Error(r.error ?? "export failed");
  return new TextDecoder().decode(
    ExportFsResponse.fromBinary(r.output).bundle ?? new Uint8Array(),
  );
}
