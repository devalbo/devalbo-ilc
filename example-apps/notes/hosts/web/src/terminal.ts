// notes' terminal route — the app's command surface, in the browser.
//
// This file is the same shape as `hosts/native/main.go`: choose an engine,
// supply the generated command surface, and say how each response prints. The
// commands, their flags, which are required and the `-h` text all come from
// commands.proto (Decision 29), so this page and the CLI cannot drift apart —
// and the parse vectors in test/terminal.spec.ts assert that in bytes.
import { enginePort } from "@devalbo/ilc-web/port";
import { mountTerminal, terminalStyles } from "@devalbo/ilc-web/terminal-ui";

import { NotesServiceCLI } from "@gen/notes/v1/commands.cli.pb";
import { PlatformServiceCLI } from "@gen/devalbo/ilc/v1/platform.cli.pb";
import { MethodCreateRecord } from "@gen/notes/v1/commands.registry.pb";

import { notesRenderers } from "./renderers";

document.head.appendChild(document.createElement("style")).textContent =
  terminalStyles();


const term = mountTerminal(document.getElementById("terminal")!, {
  port: enginePort,
  // The app's own commands PLUS the inherited platform verbs — the same two
  // lines hosts/native/main.go writes. The platform's TypeScript arrives via
  // `dlc gen`, which is the web tier's version of the Go module dependency.
  commands: [...NotesServiceCLI, ...PlatformServiceCLI],

  // The HOST supplies the clock, exactly as the CLI does with time.Now(): the
  // engine has no clock capability, because a browser tab and an MCU disagree
  // about what one even is.
  fill: (cmd, values) => {
    if (cmd.method === MethodCreateRecord && !values["created-at"]?.length) {
      values["created-at"] = [String(Math.floor(Date.now() / 1000))];
    }
  },

  // The only hand-written part, and it is presentation: a slot renders, it never
  // decides (Decision 34).
  render: notesRenderers,

  banner: [
    "notes — the same engine the CLI links, and the same command surface.",
    "`help` for commands, `<command> -h` for flags.",
    "",
  ],
});

// Same reasoning as `window.app` on the main page: a handle for the dev console,
// going through the SAME path a typed command takes rather than a second one.
declare global {
  interface Window {
    term: typeof term;
  }
}
window.term = term;
