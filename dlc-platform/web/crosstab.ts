// Cross-tab staleness for the OPFS-backed store (§6.3 follow-up).
//
// THE PROBLEM THIS SOLVES IS NOT "the other tab misses a notification." It is
// that the other tab is holding a STALE WHOLE-TREE SNAPSHOT and will overwrite
// the store with it.
//
// The web tier hydrates all of OPFS into an in-memory FileData tree at boot and
// mirrors that tree back after every command — and the mirror PRUNES (see
// writeDir in opfs.ts): anything OPFS has that the tree lacks is removed. Two
// tabs therefore hold two snapshots that diverge the moment either one writes,
// and the stale tab's next write does not just fail to show the other's work,
// it deletes it. Measured, not reasoned: with the guard below removed, two tabs
// each creating one note leave ONE note on disk.
//
// The precondition is that the losing tab HYDRATED FIRST — its snapshot has to
// predate the other's write. A tab that boots its engine lazily (dlc does not
// touch the engine until you ask it to) picks up the write for free and loses
// nothing, which is why this hazard could sit here unnoticed: the app that
// exposes it is the one that lists on load, and that is most of them.
//
// WHAT CROSSES IS THE FLUSH, NOT THE EVENT — and that distinction cost a test to
// learn. The first version relayed engine events, which meant `dlc new` wrote 35
// files and broadcast nothing, because its handler emits nothing: events fire
// when a HANDLER chooses, a flush happens after every command that reaches the
// engine. A staleness signal built on events is silent for exactly the writes
// nobody remembered to announce. This is the same argument `onFlush` makes in
// api.ts, one origin wider.
//
// BroadcastChannel is the right primitive because its scope is exactly the scope
// of the problem: same origin, which is also what OPFS is keyed by. Two contexts
// that can see each other here are precisely the two that share a store.
//
// WHAT THIS IS NOT: a sync protocol. Nothing merges, nothing rebases, and a tab
// that has fallen behind cannot catch up in place — the engine captured its
// preopen descriptor at instantiation and cannot be rebound without a reload
// (worker.ts). The honest response to "you are stale" is therefore to reboot,
// and the honest thing to do until then is refuse to write.

/** One channel per origin, which is one channel per OPFS store. */
const CHANNEL = "ilc.store";

/** What crosses: only who wrote. There is nothing useful to say beyond that,
 *  because the receiving engine cannot apply a change even if it knew one. */
interface StoreChanged {
  id: string;
}

/**
 * This context's identity, so a tab can ignore its own broadcast.
 *
 * BroadcastChannel does not deliver to the posting context, so this is belt and
 * braces — but a page that ever runs two workers would otherwise mark itself
 * stale on its own write, which is an unpleasant thing to debug.
 */
export const tabId: string =
  globalThis.crypto?.randomUUID?.() ?? `t${Math.random().toString(36).slice(2)}`;

type Channel = {
  postMessage(message: unknown): void;
  close(): void;
  onmessage: ((event: { data: unknown }) => void) | null;
};

let channel: Channel | null = null;

/**
 * Open the channel, or return null where BroadcastChannel does not exist.
 *
 * Absence is a no-op rather than an error, matching Decision 33: a runtime
 * without the API behaves exactly as this tier did before — one context, no
 * cross-talk. Nothing exposes which case it is in, so no app can branch on it.
 */
function open(): Channel | null {
  if (channel) return channel;
  const Ctor = (globalThis as { BroadcastChannel?: new (name: string) => Channel })
    .BroadcastChannel;
  if (!Ctor) return null;
  channel = new Ctor(CHANNEL);
  return channel;
}

/** Tell every other context on this origin that the store moved under them. */
export function publishStoreChanged(): void {
  open()?.postMessage({ id: tabId } satisfies StoreChanged);
}

/** Hear that another context wrote. Returns an unsubscribe. */
export function receiveStoreChanged(fn: () => void): () => void {
  const ch = open();
  if (!ch) return () => {};
  const prev = ch.onmessage;
  ch.onmessage = (message) => {
    prev?.(message);
    const data = message.data as StoreChanged | null;
    if (!data || typeof data.id !== "string" || data.id === tabId) return;
    fn();
  };
  return () => {
    ch.onmessage = prev;
  };
}
