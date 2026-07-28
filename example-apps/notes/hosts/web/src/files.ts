// notes' OPFS browser route.
//
// Reads the filesystem DIRECTLY, not through the engine — which is the point:
// if the store is only readable via the app, "the files are the truth" (§7.1) is
// a claim about an implementation detail rather than about the store.
//
// It does subscribe to engine events, so a write from the app or the terminal
// shows up here without a manual refresh. That is the same reactivity loop the
// record list uses (§6.3), pointed at a filesystem view.
import { onFlush } from "@devalbo/ilc-web/api";
import { filesStyles, mountFiles } from "@devalbo/ilc-web/files";

document.head.appendChild(document.createElement("style")).textContent =
  filesStyles();

// `onFlush`, not `subscribe`. This page watches the FILESYSTEM and knows nothing
// about what a notes command means — which is the point of it: someone watching
// the store infers what happened, rather than being told by the app. A flush is
// a host fact ("the tree is persisted"); an engine event is an app fact
// ("a record changed"), and only the first is what a watcher is actually
// waiting for.
const files = mountFiles(document.getElementById("files")!, { onFlush });

void files.refresh();

declare global {
  interface Window {
    files: typeof files;
  }
}
window.files = files;
