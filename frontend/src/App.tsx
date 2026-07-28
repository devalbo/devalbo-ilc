// The web tier's UI. It builds a typed request from form state and hands it to
// the engine through the adapter — that is the whole Decision 28 story on this
// tier: parsing is host-side, and the "parser" here is a form.
//
// No business logic lives here. What `new` *means* is in engine/, shared with
// the CLI; this only collects fields and renders what came back.
import { DataChangedEventTopic } from "@gen/devalbo/ilc/v1/platform.events.pb";
import { useCallback, useEffect, useState } from "react";
import {
  execute,
  listFiles,
  reset,
  subscribe,
} from "@devalbo/ilc-web/api";
// Ids are generated, never typed by hand — see the note in the host's api.ts.
import { MethodNew } from "@gen/devalbo/dlc/v1/commands.registry.pb";
import {
  MethodExportFs,
  MethodImportFs,
} from "@gen/devalbo/ilc/v1/platform.registry.pb";
import { NewRequest, NewResponse } from "@gen/devalbo/dlc/v1/commands.pb";
// The filesystem/bundle verbs are the PLATFORM's, not dlc's — every app gets
// this same download/import UI for free.
import {
  DataChangedEvent,
  ExportFsRequest,
  ExportFsResponse,
  ImportFsRequest,
  ImportFsResponse,
  ImportMode,
} from "@gen/devalbo/ilc/v1/platform.pb";

export function App() {
  const [name, setName] = useState("myapp");
  const [module, setModule] = useState("");
  // TIER SELECTION, this tier's way of asking. The engine has no default — an
  // empty list is refused — because a tier is a directory of host code plus a
  // `dlc.toml` entry that is checked to exist, and choosing on a caller's behalf
  // scaffolds a layout nobody picked. The CLI marks `--tiers` required and
  // prompts when interactive; a browser asks with checkboxes. Same question,
  // two idioms, one refusal underneath (Decision 28).
  const [tiers, setTiers] = useState<string[]>(["native", "web"]);

  const toggleTier = (t: string) =>
    setTiers((cur) => (cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t]));
  const [files, setFiles] = useState<string[]>([]);
  const [log, setLog] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const say = useCallback((line: string) => {
    setLog((l) => [...l, `${new Date().toISOString().slice(11, 19)}  ${line}`]);
  }, []);

  const refresh = useCallback(async () => {
    setFiles(await listFiles());
  }, []);

  // On load, show whatever survived the last session — this is the OPFS
  // persistence claim, visible rather than asserted.
  useEffect(() => {
    refresh().catch((e) => say(`ERROR listing OPFS: ${e.message}`));
  }, [refresh, say]);

  // The reactivity loop (§6.3): the engine announces that the filesystem
  // changed, and the view re-reads. This is not a nicety over calling refresh()
  // after our own commands — it is the only thing that works when the writer is
  // NOT this component: another tab on the same OPFS origin, a future sync, or
  // an `execute` from anywhere else in the app.
  //
  // The callback runs on the main thread (the worker reaches it by message), so
  // calling back into the engine from here would be legal. Inside the worker it
  // would be re-entrancy — see hosts/web/README.md.
  useEffect(() => {
    let unsubscribe: (() => void) | null = null;
    let dropped = false;
    subscribe((topic, payload) => {
      if (topic !== DataChangedEventTopic) return;
      const { prefix, method } = DataChangedEvent.fromBinary(payload);
      say(`event ${topic} — prefix "${prefix ?? ""}", method ${method ?? 0}`);
      refresh().catch((e) => say(`ERROR listing OPFS: ${e.message}`));
    })
      .then((off) => {
        // StrictMode double-mounts in dev: if the cleanup already ran, drop the
        // subscription the moment it resolves rather than leaking it.
        if (dropped) off();
        else unsubscribe = off;
      })
      .catch((e) => say(`ERROR subscribing: ${e.message}`));
    return () => {
      dropped = true;
      unsubscribe?.();
    };
  }, [refresh, say]);

  async function runNew() {
    setBusy(true);
    try {
      // Only what the template can actually honor. The engine now REJECTS
      // unsupported caps/tiers/ui/storage rather than silently emitting
      // something else, so sending aspirational values here would just fail —
      // correctly. They come back as the template grows.
      const request = NewRequest.toBinary({ name, module, tiers });
      const r = await execute(MethodNew, request);
      if (!r.success) {
        say(`new failed: ${r.error ?? "(no message)"}`);
        return;
      }
      const resp = NewResponse.fromBinary(r.output);
      say(`scaffolded ${resp.path} — ${resp.files?.length ?? 0} files`);
      await refresh();
    } catch (e) {
      say(`ERROR: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  // Download = export-fs. Per §7.3 these are the SAME operation, not two
  // features: the engine bundles the tree, the host just hands the blob to the
  // browser. A bundle downloaded here imports byte-identically in the terminal.
  //
  // Exports the WHOLE root, not the named app, so that download and import are
  // exact inverses. `export-fs --prefix app` bundles the *contents* of app/,
  // which would reimport as loose files at the root and quietly lose the
  // directory — correct for the CLI (where you choose the destination prefix),
  // surprising for a one-click download. The engine still does subtrees; this
  // is the host picking the sane default for this tier.
  async function runExport() {
    setBusy(true);
    try {
      const request = ExportFsRequest.toBinary({ prefix: "" });
      const r = await execute(MethodExportFs, request);
      if (!r.success) {
        say(`export-fs failed: ${r.error ?? "(no message)"}`);
        return;
      }
      const { bundle } = ExportFsResponse.fromBinary(r.output);
      const blob = new Blob([bundle ?? new Uint8Array()], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "dlc-workspace.bft.json";
      a.click();
      URL.revokeObjectURL(url);
      say(`exported workspace — ${bundle?.length ?? 0} bytes of BFT`);
    } catch (e) {
      say(`ERROR: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  // …and the inverse: a bundle from anywhere (this browser, a colleague's
  // terminal, a git diff someone hand-edited) unpacks into OPFS.
  async function runImport(file: File) {
    setBusy(true);
    try {
      const bundle = new Uint8Array(await file.arrayBuffer());
      // REPLACE, not merge: importing a bundle should give you exactly what the
      // bundle says. A merge cannot express a deletion, so re-importing an
      // edited bundle would silently keep files you removed.
      const request = ImportFsRequest.toBinary({
        bundle,
        prefix: "",
        mode: ImportMode.REPLACE,
      });
      const r = await execute(MethodImportFs, request);
      if (!r.success) {
        say(`import-fs failed: ${r.error ?? "(no message)"}`);
        return;
      }
      const resp = ImportFsResponse.fromBinary(r.output);
      say(`imported ${resp.files?.length ?? 0} files from ${file.name}`);
      // NO refresh() here, deliberately. `import-fs` emits `ilc.data-changed`,
      // and the subscription above re-lists. If the tree stops updating after an
      // import, the event path is broken — which is the point: a manual refresh
      // here would keep the UI correct while the capability rotted unnoticed.
      //
      // `new` still refreshes by hand: it does not emit yet (the platform verbs
      // that touch the whole store do). It will lose that call when it does.
    } catch (e) {
      say(`ERROR: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  async function runReset() {
    setBusy(true);
    try {
      await reset();
      say("OPFS cleared");
      await refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <main style={styles.main}>
      <h1 style={styles.h1}>dlc — web tier</h1>
      <p style={styles.sub}>
        The same engine the CLI links natively, running here as a wasip2
        component with its filesystem root bound to OPFS.
      </p>

      <section style={styles.card}>
        <label style={styles.label}>
          app name
          <input
            style={styles.input}
            value={name}
            onChange={(e) => setName(e.target.value)}
            data-testid="name"
          />
        </label>
        <label style={styles.label}>
          module <span style={styles.hint}>(optional)</span>
          <input
            style={styles.input}
            value={module}
            placeholder="github.com/you/<app>"
            onChange={(e) => setModule(e.target.value)}
            data-testid="module"
          />
        </label>
        <fieldset style={styles.row}>
          <legend>tiers</legend>
          {["native", "web"].map((t) => (
            <label key={t} style={{ marginRight: "1rem" }}>
              <input
                type="checkbox"
                checked={tiers.includes(t)}
                onChange={() => toggleTier(t)}
                data-testid={`tier-${t}`}
              />{" "}
              {t}
            </label>
          ))}
        </fieldset>
        <div style={styles.row}>
          <button
            style={styles.button}
            onClick={runNew}
            // Disabled with no tier selected: the engine would refuse, and a
            // form that lets you submit a request it knows will fail is just a
            // slower error message.
            disabled={busy || !name || tiers.length === 0}
            data-testid="new"
          >
            dlc new
          </button>
          <button
            style={styles.secondary}
            onClick={runExport}
            disabled={busy}
            data-testid="export"
          >
            download .bft.json
          </button>
          <label style={styles.fileButton}>
            import bundle
            <input
              type="file"
              accept=".json,application/json"
              style={{ display: "none" }}
              data-testid="import"
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = ""; // re-selecting the same file must re-fire
                if (file) runImport(file);
              }}
            />
          </label>
          <button style={styles.secondary} onClick={runReset} disabled={busy}>
            clear OPFS
          </button>
          <button style={styles.secondary} onClick={() => location.reload()}>
            reload page
          </button>
        </div>
      </section>

      <section style={styles.card}>
        <h2 style={styles.h2}>OPFS ({files.length} files)</h2>
        <ul style={styles.tree} data-testid="files">
          {files.length === 0 && <li style={styles.empty}>empty</li>}
          {files.map((f) => (
            <li key={f}>{f}</li>
          ))}
        </ul>
      </section>

      <section style={styles.card}>
        <h2 style={styles.h2}>log</h2>
        <pre style={styles.log} data-testid="log">
          {log.join("\n")}
        </pre>
      </section>
    </main>
  );
}

const styles: Record<string, React.CSSProperties> = {
  main: {
    fontFamily: "ui-sans-serif, system-ui, sans-serif",
    maxWidth: "42rem",
    margin: "0 auto",
    padding: "2rem 1rem",
    lineHeight: 1.5,
  },
  h1: { fontSize: "1.5rem", margin: "0 0 .25rem" },
  h2: { fontSize: ".95rem", margin: "0 0 .5rem", fontWeight: 600 },
  sub: { margin: "0 0 1.5rem", color: "#666", fontSize: ".9rem" },
  card: {
    border: "1px solid #ddd",
    borderRadius: 8,
    padding: "1rem",
    marginBottom: "1rem",
  },
  label: { display: "block", marginBottom: ".75rem", fontSize: ".85rem" },
  hint: { color: "#999", fontWeight: 400 },
  input: {
    display: "block",
    width: "100%",
    marginTop: ".25rem",
    padding: ".4rem .5rem",
    font: "inherit",
    border: "1px solid #ccc",
    borderRadius: 4,
  },
  row: { display: "flex", gap: ".5rem", flexWrap: "wrap" },
  button: {
    padding: ".45rem .9rem",
    font: "inherit",
    cursor: "pointer",
    border: "1px solid #333",
    borderRadius: 4,
    background: "#333",
    color: "#fff",
  },
  secondary: {
    padding: ".45rem .9rem",
    font: "inherit",
    cursor: "pointer",
    border: "1px solid #ccc",
    borderRadius: 4,
    background: "#fff",
  },
  // A <label> wrapping a hidden <input type=file>: the only way to style a file
  // picker, and it keeps the row visually consistent with the buttons.
  fileButton: {
    padding: ".45rem .9rem",
    font: "inherit",
    cursor: "pointer",
    border: "1px solid #ccc",
    borderRadius: 4,
    background: "#fff",
    display: "inline-block",
  },
  tree: {
    margin: 0,
    paddingLeft: "1.1rem",
    fontFamily: "ui-monospace, monospace",
    fontSize: ".8rem",
  },
  empty: { color: "#999", listStyle: "none", marginLeft: "-1.1rem" },
  log: {
    margin: 0,
    fontFamily: "ui-monospace, monospace",
    fontSize: ".75rem",
    whiteSpace: "pre-wrap",
    color: "#444",
    minHeight: "1rem",
  },
};
