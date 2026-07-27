Clean tree, all landed. Events (§6.3) — same recommendation, and now it's the only thing standing between notes and a real reactivity story.

The concrete want: notes' browser UI calls refresh() by hand after every command. That's wrong the moment anything else writes — a second tab, the CLI, a future sync. emit-event("data-changed") → host subscription → UI invalidates is the loop §6.3 describes.

Three things it forces, all of which are load-bearing beyond Events:

1. The first custom WIT import. Console and Filesystem are both standard WASI, so the engine has never imported anything a host must provide. Events is the first, and whatever shape it takes is what Display, index, and network inherit.
2. The caps_native.go / caps_wasip2.go seam finally gets built. §5.3 designed it; it's still an unchecked task because nothing needed it. Native satisfies the import in-process, wasm satisfies it through the component boundary — same engine code above the seam.
3. A parity question worth settling early. The check diffs native vs wasm results and written filesystems. If handlers start emitting events, either both tiers emit identically (and we compare them) or events are excluded from the comparison. I'd argue for comparing them — an event is part of what a command does, and silently untested side effects are how the OPFS flush bug survived.

Rough shape: WIT event-host interface → platform.Emit behind the build-tag seam → notes emits data-changed on create/delete → web worker forwards over Comlink → UI drops its manual refresh → browser test asserts the list updates without the UI asking.