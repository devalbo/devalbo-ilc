# wit/ — the ILC capability world (framework)

`ilc.wit` defines the capabilities the engine imports (console via WASI stdio, `wasi:filesystem`,
`sqlite-host`, `event-host`, `display-host`) and the `execute-cli` export.

**Notes:**
- Framework artifact — **keeps the `ilc` name** (the tool is `dlc`; the framework is ILC). Package
  `devalbo:ilc`.
- Kebab-case source; emitters project camelCase. Bindings are generated into `gen/` by `wit-bindgen-go`.
- Console is **not** a custom interface — it's standard `wasi:cli` stdio.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §6.
