// dlc's command inspector route.
import { enginePort } from "@devalbo/ilc-web/port";
import { setEnvironment, type CommandResult } from "@devalbo/ilc-web/api";
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

// `window.host` — the HOST's own handle, next to window.app's engine one.
//
// The distinction is the point (AGENTS.md §3): window.app runs commands, which
// is the app's domain; this states what the host can do, which is the host's.
// Exposed for the same reason window.app is — a capability you can only reach
// through the UI is one nobody exercises — and it is how the volatile half of
// the manifest is driven at all today, since the browser gives no event for a
// filesystem appearing or disappearing.
declare global {
  interface Window {
    inspector: typeof inspector;
    host: { setEnvironment(hasFilesystem: boolean): Promise<CommandResult> };
  }
}
window.inspector = inspector;
window.host = { setEnvironment };
