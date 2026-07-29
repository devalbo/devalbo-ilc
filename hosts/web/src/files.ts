// dlc's OPFS browser route.
//
// Reads the filesystem directly, not through the engine: if the store were only
// legible via the app, §7.1's "the files are the truth" would be a claim about an
// implementation detail rather than about the store.
import { onFlush } from "@devalbo/dlc-web/api";
import { filesStyles, mountFiles } from "@devalbo/dlc-web/files";

document.head.appendChild(document.createElement("style")).textContent = filesStyles();

// `onFlush`, not `subscribe`: this watches the FILESYSTEM and knows nothing about
// what a dlc command means. A flush is a host fact; an event is an app fact.
const files = mountFiles(document.getElementById("files")!, { onFlush });
void files.refresh();

declare global {
  interface Window {
    files: typeof files;
  }
}
window.files = files;
