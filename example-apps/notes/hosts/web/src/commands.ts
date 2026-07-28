// notes' command inspector route.
//
// The renderers are the same ones the terminal uses — deliberately shared rather
// than re-listed, because a second copy would be a second place for a response
// to be formatted differently depending on which page you were looking at.
import { enginePort } from "@devalbo/ilc-web/port";
import { inspectorStyles, mountInspector } from "@devalbo/ilc-web/inspector";

import { NotesServiceCLI } from "@gen/notes/v1/commands.cli.pb";
import { PlatformServiceCLI } from "@gen/devalbo/ilc/v1/platform.cli.pb";
import { MethodCreateRecord } from "@gen/notes/v1/commands.registry.pb";

import { notesRenderers } from "./renderers";

document.head.appendChild(document.createElement("style")).textContent =
  inspectorStyles();

const inspector = mountInspector(document.getElementById("commands")!, {
  port: enginePort,
  // The app's own commands PLUS the inherited platform verbs — the same two
  // lines hosts/native/main.go writes. The platform's TypeScript arrives via
  // `dlc gen`, which is the web tier's version of the Go module dependency.
  commands: [...NotesServiceCLI, ...PlatformServiceCLI],
  render: notesRenderers,
  // The HOST supplies the clock, exactly as the CLI and the terminal do.
  fill: (cmd, values) => {
    if (cmd.method === MethodCreateRecord && !values["created-at"]?.length) {
      values["created-at"] = [String(Math.floor(Date.now() / 1000))];
    }
  },
});

inspector.select("create");

declare global {
  interface Window {
    inspector: typeof inspector;
  }
}
window.inspector = inspector;
