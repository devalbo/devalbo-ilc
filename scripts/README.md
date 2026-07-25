# scripts/ — repo helper scripts

Small, dependency-free helpers. **Pure bash, no toolchain dependency** — they must run *before* Devbox
provisions anything.

- `preflight.sh` — assess prerequisites (system + provisioned toolchain). Run via `./scripts/preflight.sh`
  or `make doctor`. Superseded later by `dlc doctor`.

See [prerequisites](../docs/DEVALBO-DLC-PREREQUISITES.md).
