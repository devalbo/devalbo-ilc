# hosts/ — per-platform capability providers (the injected Environment)

A host is the entry point that **constructs the `Environment`** and runs the shared engine.

## Two layers, not one (Decision 34)

`engine/` vs `engine/platform/` splits an app's own code from what every app inherits. **The same line runs
through here**, and until it was drawn this directory quietly held both:

| Layer | Portability | What it is |
| --- | --- | --- |
| **host runtime** | inherited — identical for every app | instantiate the engine, wire capabilities, deliver events, turn native input into a request. lives in `dlc-platform/web`, published as `@devalbo/dlc-web`. |
| **tier slot** — `hosts/<tier>/` **in an app** | per app, per tier | that app's presentation and input on that tier |

**The collision is GONE (§16.4, 2026-07-28).** The inherited runtime moved out to the `dlc-platform`
module — Go in `dlc-platform/`, TypeScript in `dlc-platform/web` (`@devalbo/dlc-web`) — so `hosts/` in this
repo now holds only `dlc`'s own slots, exactly like `example-apps/notes/`. `dlc`'s web slot moved from
`frontend/` into `hosts/web/`, the name every app uses.

`native/` still holds dlc's toolchain alongside its slot (`build.go`, `gen.go`, `manifest.go`), but that is
no longer a runtime/slot collision: the runtime half is `dlc-platform/cli`, and an app owning its own
toolchain is ordinary.

**Every tier in `dlc.toml` names its slot, and the slot must exist.** That is the one thing `dlc.toml`
actually gates — `dlc build`/`dlc gen` refuse a manifest whose `root` points nowhere, because a stale
`root` is what a renamed directory looks like from here.

## Rules

- Hosts **provide** capabilities; they carry **no business logic** (that's `engine/`).
- **A tier slot renders; it never decides.** If two hosts each work out whether a board is won, they will
  eventually disagree — on one tier only, with every check green, because parity compares command results,
  the written filesystem, and the event stream, all of which are engine-side. A slot is invisible to it *by
  construction*. The engine decides; a slot may draw what it was told and may not derive it.
- A host must **never call `Execute` from inside an event sink** — the engine is on the stack. The web host
  is safe by construction (Comlink is a message boundary); a native host must defer.
- One host per target; the engine is unchanged across them (the "two bits").
- Bootstrap tiers: `native/` (CLI, engine in-process) and `web/` (jco). Desktop/embedded arrive later.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §6.4, §0.1 and
[HOST-LAYER-PLAN.md](../docs/HOST-LAYER-PLAN.md).
