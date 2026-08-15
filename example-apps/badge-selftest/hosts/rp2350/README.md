# The badge slot — deliberately empty

**This app adds nothing here, and that is the design working.**

A tier's `root` holds the host code that turns native input into a proto request
and a response into something a person can see. On the badge, all of that is
**inherited** from `dlc-platform/embedded`: the loader that finds payloads, the
ST7789 console that renders text, the five buttons, the status colour. See
`EMBEDDED-PLAN.md` D3 — a Pulley host inlined into each app would be the mistake
§16.6 exists to prevent, because code copied into a scaffold is frozen there.

A self-test has no badge-specific presentation to add: it returns a string, and
every tier already knows how to show a string.

The directory exists because `dlc.toml` requires every tier to name a root. That
is arguably a tooling wrinkle rather than a fact about this app — an inherited
slot has no directory of its own — and it is recorded here rather than worked
around silently.
