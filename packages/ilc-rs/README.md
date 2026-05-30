# devalbo-ilc (Rust crate name TBD)

Generated `ConsoleIo` and `Environment` traits (`src/generated/types.rs`).

Regenerate:

```bash
cargo run -p ilc-compiler -- compile ../../wit --out ..
```

## Hello `ConsoleIo`

```rust
use devalbo_ilc::{ConsoleIo, Environment};

pub async fn hello(env: &impl Environment) {
    let consoleIo = env.consoleIo().await;
    consoleIo.info("hello from ILC").await;
}
```

## Hosts

- `create_process_environment()` — stdout / stderr / stdin
- `create_serverless_environment()` — non-interactive
- `create_test_environment()` — captured logs + queued stdin

## Smoke

```bash
cargo run --example hello -p devalbo-ilc -- --test
cargo run --example hello -p devalbo-ilc
```
