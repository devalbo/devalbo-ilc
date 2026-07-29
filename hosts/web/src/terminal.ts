// dlc's terminal route.
//
// Plain TypeScript, not React, even though index.html is a React app — the three
// text routes are separate Vite entries, so they cost the React bundle nothing
// and read identically to the ones a scaffolded app gets. dlc consuming the
// runtime the same way a generated project does is the point (AGENTS.md §3).
import { enginePort } from "@devalbo/dlc-web/port";
import { mountTerminal, terminalStyles } from "@devalbo/dlc-web/terminal-ui";

import { DlcServiceCLI } from "@gen/devalbo/dlc/v1/commands.cli.pb";
import { PlatformServiceCLI } from "@gen/devalbo/ilc/v1/platform.cli.pb";

import { dlcRenderers } from "./renderers";

document.head.appendChild(document.createElement("style")).textContent = terminalStyles();

const term = mountTerminal(document.getElementById("terminal")!, {
  // dlc's own commands PLUS the platform verbs it inherits — the same two lines
  // hosts/native/commands.go writes.
  commands: [...DlcServiceCLI, ...PlatformServiceCLI],
  port: enginePort,
  render: dlcRenderers,
  banner: [
    "dlc — the same engine the CLI links, and the same command surface.",
    "`help` for commands, `<command> -h` for flags, Tab to complete.",
    "",
  ],
});

declare global {
  interface Window {
    term: typeof term;
  }
}
window.term = term;
