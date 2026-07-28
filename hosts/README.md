# hosts/ — per-platform capability providers (the injected Environment)

A host is the entry point that **constructs the `Environment`** and runs the shared engine.

## Two layers, not one (Decision 34)

`engine/` vs `engine/platform/` splits an app's own code from what every app inherits. **The same line runs
through here**, and until it was drawn this directory quietly held both:

| Layer | Portability | What it is |
| --- | --- | --- |
| **host runtime** | inherited — identical for every app | instantiate the engine, wire capabilities, deliver events, turn native input into a request. `web/` is exactly this, published as `@devalbo/ilc-web`. |
| **tier slot** — `hosts/<tier>/` **in an app** | per app, per tier | that app's presentation and input on that tier |

**In this repo the two collide, deliberately and temporarily.** `web/` is pure runtime, `native/` still
mixes runtime (the in-process engine binding) with `dlc`'s own commands (`commands.go`, `build.go`,
`gen.go`), and `dlc`'s web slot is `frontend/` at the repo root rather than `hosts/web/` — because that name
is taken by the runtime until it is extracted (§16.4). An app has no such collision: `example-apps/notes/`
carries `hosts/native/` and `hosts/web/` and nothing else.

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
