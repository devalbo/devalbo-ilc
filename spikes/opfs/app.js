// Spike 3 browser host — boot the wasip2 engine with a WASI root backed by
// OPFS (via the preview2-shim in-memory tree + opfs-bridge hydrate/flush).
//
// Important: hydrate _setFileData *before* importing the transpiled component —
// guests typically snapshot preopens at instantiation.
import {
  _setFileData,
  _getFileData,
} from "@bytecodealliance/preview2-shim/filesystem";
import {
  flushTreeToOPFS,
  loadTreeFromOPFS,
  opfsRoot,
  readOPFSText,
  reviveFileDataJSON,
} from "./opfs-bridge.js";

const PATH = "/hello.txt";
const CONTENT = "persist-me";

const logEl = document.getElementById("log");
const statusEl = document.getElementById("status");

/** @type {null | ((args: string[]) => { success: boolean, output?: number[], error?: string })} */
let executeCli = null;

function log(msg) {
  const line = `[${new Date().toISOString().slice(11, 19)}] ${msg}`;
  console.log(line);
  if (logEl) {
    logEl.textContent += line + "\n";
  }
}

function setStatus(s) {
  if (statusEl) statusEl.textContent = s;
  document.body.dataset.status = s;
}

function call(args) {
  if (!executeCli) throw new Error("engine not booted");
  const res = executeCli(args);
  const text = new TextDecoder().decode(Uint8Array.from(res.output ?? []));
  if (!res.success) {
    throw new Error(`${args[0]} failed: ${res.error ?? text}`);
  }
  return text;
}

async function hydrateFromOPFS() {
  const tree = await loadTreeFromOPFS();
  _setFileData(tree);
  log(
    `hydrated WASI root from OPFS (${Object.keys(tree.dir ?? {}).length} top-level entries)`,
  );
}

async function flushToOPFS() {
  const tree = reviveFileDataJSON(_getFileData());
  await flushTreeToOPFS(tree);
  log("flushed in-memory WASI FS → OPFS");
}

async function bootEngine() {
  await hydrateFromOPFS();
  const mod = await import("./out/engine.component.js");
  executeCli = mod.executeCli;
  log("engine instantiated (after OPFS hydrate)");
}

/**
 * Write via the engine, flush to OPFS. Used before reload.
 */
async function spikeWrite() {
  setStatus("writing");
  // Hydrate exactly once before instantiation — the guest caches the preopen
  // Descriptor; later _setFileData swaps would be invisible to a live engine.
  if (!executeCli) await bootEngine();
  const wrote = call(["write", PATH, CONTENT]);
  log(`engine write → ${wrote}`);
  await flushToOPFS();
  const direct = await readOPFSText(PATH);
  log(`OPFS direct read → ${JSON.stringify(direct)}`);
  if (direct !== CONTENT) {
    throw new Error(`OPFS mismatch after flush: ${JSON.stringify(direct)}`);
  }
  setStatus("wrote");
  return { wrote, path: PATH, content: CONTENT, opfsDirect: direct };
}

/**
 * After reload: hydrate from OPFS, (re)boot engine, read via engine.
 */
async function spikeRead() {
  setStatus("reading");
  // Always boot fresh after reload so preopens see hydrated OPFS.
  executeCli = null;
  await bootEngine();
  const text = call(["read", PATH]);
  log(`engine read → ${JSON.stringify(text)}`);
  const direct = await readOPFSText(PATH);
  log(`OPFS direct read → ${JSON.stringify(direct)}`);
  if (text !== CONTENT) {
    throw new Error(`engine read mismatch: ${JSON.stringify(text)}`);
  }
  if (direct !== CONTENT) {
    throw new Error(`OPFS direct mismatch: ${JSON.stringify(direct)}`);
  }
  setStatus("pass");
  return { text, opfsDirect: direct, path: PATH, content: CONTENT };
}

window.__spike = {
  PATH,
  CONTENT,
  boot: bootEngine,
  write: spikeWrite,
  read: spikeRead,
  hydrateFromOPFS,
  flushToOPFS,
  opfsRoot,
  readOPFSText,
  call,
};

document.getElementById("btn-write")?.addEventListener("click", () => {
  spikeWrite().catch((e) => {
    log("ERROR: " + e.message);
    setStatus("fail");
  });
});
document.getElementById("btn-read")?.addEventListener("click", () => {
  spikeRead().catch((e) => {
    log("ERROR: " + e.message);
    setStatus("fail");
  });
});
document.getElementById("btn-reload")?.addEventListener("click", () => {
  location.reload();
});

setStatus("ready");
log("ready — click Write, or let Playwright drive the flow");
