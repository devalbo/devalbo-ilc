// hello's web tier slot — the entry point.
//
// Everything this file does is choose an engine and publish a handle. The view
// lives in `view.ts` and takes an `EnginePort` (Decision 34), so the only
// difference between the real app and a slot test is which port gets passed here
// versus there — no build flag, no mock injection, no branch inside the view.
import { enginePort } from "@devalbo/dlc-web/port";

import { mountApp } from "./view";
import type { AppView } from "./view";

const view = mountApp(enginePort);

// `window.app` — drive the app from the dev console:
//
//	await app.greet("world")
//	app.projection()
//
// These are the SAME functions the buttons call, not a parallel back door: the
// click handler collects a string and calls `app.greet`. A console API that took
// a different path would be a second front end able to disagree with the first.
declare global {
  interface Window {
    app: AppView;
  }
}
window.app = view;

document.getElementById("out")!.textContent +=
  "ready — the engine boots on the first command. `app` is on window: try app.greet(\"world\")\n";
