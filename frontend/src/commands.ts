// dlc's command inspector route.
import { enginePort } from "@devalbo/ilc-web/port";
import { inspectorStyles, mountInspector } from "@devalbo/ilc-web/inspector";

import { DlcServiceCLI } from "@gen/devalbo/dlc/v1/commands.cli.pb";
import { PlatformServiceCLI } from "@gen/devalbo/ilc/v1/platform.cli.pb";

import { dlcRenderers } from "./renderers";

document.head.appendChild(document.createElement("style")).textContent = inspectorStyles();

const inspector = mountInspector(document.getElementById("commands")!, {
  port: enginePort,
  commands: [...DlcServiceCLI, ...PlatformServiceCLI],
  render: dlcRenderers,
});

inspector.select("new");

declare global {
  interface Window {
    inspector: typeof inspector;
  }
}
window.inspector = inspector;
