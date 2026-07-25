# spikes/ — de-risking proofs, kept as permanent regression

Each subdir is a minimal, self-contained proof of one load-bearing assumption. **Kept, not thrown away** —
they become standing regression tests so the foundation stays proven as code changes.

Bootstrap spikes (B1): `component/` (TinyGo→adapter→jco), `proto/` (protobuf-go-lite ↔ es-lite),
`opfs/` (filesystem persistence), `cli/` (kong→TInput→engine, reflection-free), async.

Steps + pass criteria: [test-steps](../docs/DEVALBO-DLC-TEST-STEPS.md) Phase B1.
