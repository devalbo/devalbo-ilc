// Stock jco host binding for devalbo:ilc/host-delay (mapped via --map).
// Genuinely async: resolves after setTimeout — not queueMicrotask-only.

export function delay(ms) {
  const n = Number(ms);
  return new Promise((resolve) => {
    setTimeout(() => resolve(`ok:${n}`), n);
  });
}
