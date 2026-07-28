// tictactoe's command inspector route.
import { enginePort } from "@devalbo/ilc-web/port";
import { inspectorStyles, mountInspector } from "@devalbo/ilc-web/inspector";

import { GameServiceCLI } from "@gen/tictactoe/v1/commands.cli.pb";
import { PlatformServiceCLI } from "@gen/devalbo/ilc/v1/platform.cli.pb";

import { gameRenderers } from "./renderers";

document.head.appendChild(document.createElement("style")).textContent =
  inspectorStyles();

const inspector = mountInspector(document.getElementById("commands")!, {
  port: enginePort,
  // This app's commands PLUS the inherited platform verbs — the same two lines
  // hosts/native/main.go writes.
  commands: [...GameServiceCLI, ...PlatformServiceCLI],
  render: gameRenderers,
});

inspector.select(GameServiceCLI[0].name);

declare global {
  interface Window {
    inspector: typeof inspector;
  }
}
window.inspector = inspector;
