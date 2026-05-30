# devalbo-ilc (Python package name TBD)

Generated `ConsoleIo` and `Environment` protocols (`src/devalbo_ilc/generated/types.py`).

Regenerate:

```bash
cargo run -p ilc-compiler -- compile ../../wit --out ..
```

## Hello `ConsoleIo`

```python
from devalbo_ilc import ConsoleIo, Environment

async def hello(env: Environment) -> None:
    consoleIo = await env.consoleIo()
    await consoleIo.info("hello from ILC")
```

## Hosts

- `create_process_environment()` — stdout / stderr / `input()`
- `create_serverless_environment()` — non-interactive
- `create_test_environment()` — captured logs + queued stdin

## Smoke

```bash
PYTHONPATH=src python3 -m devalbo_ilc.examples.hello --test
PYTHONPATH=src python3 -m devalbo_ilc.examples.hello
```
