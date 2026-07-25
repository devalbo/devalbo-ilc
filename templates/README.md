# templates/ — what `dlc new` emits (its own concern)

A base skeleton + per-knob fragments that `dlc new` imports (`import-fs`) to scaffold a new app.

**Rules:**
- **Depend-on, never inline.** A template references the `ilc-platform` module as a versioned dependency;
  it must **not** copy framework internals. Editing a template ≠ changing the framework.
- **Hand-authored** here (own PRs, own mental model); drift is caught by CI, not prevented by derivation.
- `go:embed`'d into `dlc` so `dlc new` is self-contained (offline + browser).
- **Validated by** scaffold → build → verify (test-steps Scaffolder row).

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §16.6.
