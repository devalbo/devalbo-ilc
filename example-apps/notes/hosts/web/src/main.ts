// notes' web tier slot — the entry point.
//
// Everything this file does is choose an engine and publish a handle. The view
// lives in `view.ts` and takes an `EnginePort` (Decision 34), so the only
// difference between the real app and a slot test is which port gets passed here
// versus there — no build flag, no mock injection, no branch inside the view.
//
// That split is not ceremony. A slot is the one part of an ILC app that parity
// cannot check: it compares command results, the written filesystem, and the
// event stream, all of which are engine-side. So the slot needs its own way to
// be wrong out loud, and it only has one if the engine can be swapped out.
import { enginePort } from "@devalbo/ilc-web/port";

import { mountNotes } from "./view";
import type { NotesView } from "./view";

const view = mountNotes(enginePort);

// `window.app` — drive the app from the dev console.
//
//	await app.create("Buy milk", "two litres")
//	app.projection()
//	await app.remove("buy-milk")
//
// These are the SAME functions the buttons call, not a parallel back door: the
// click handler collects two strings and calls `app.create`. A console API that
// took a different path would be a second front end able to disagree with the
// first, which is the whole failure mode this architecture exists to prevent.
//
// Note what it is NOT: a way to reach the engine. It exposes the slot's own
// operations, so anything you can do here, a user can do by clicking. To drive
// the engine *underneath* the UI — a second writer, which is a different thing —
// go through `@devalbo/ilc-web/api` directly, as `test/driver.ts` does.
declare global {
  interface Window {
    app: NotesView;
  }
}
window.app = view;

view.refresh().catch((e) => {
  const out = document.getElementById("out");
  if (out) out.textContent += `ERROR: ${(e as Error).message}\n`;
});

document.getElementById("out")!.textContent +=
  "ready — the engine boots on the first command. `app` is on window: try app.projection()\n";
