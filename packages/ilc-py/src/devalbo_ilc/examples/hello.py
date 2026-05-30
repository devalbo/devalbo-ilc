"""Run: python -m devalbo_ilc.examples.hello"""

from __future__ import annotations

import asyncio
import sys

from devalbo_ilc.hello import hello
from devalbo_ilc.hosts import create_process_environment, create_test_environment


async def run() -> None:
    if "--test" in sys.argv:
        env = create_test_environment()
        await hello(env)
        info = next((e for e in env.logs if e.level == "info"), None)
        if info is None or info.message != "hello from ILC":
            print("unexpected logs:", env.logs, file=sys.stderr)
            raise SystemExit(1)
        print("ok: test host captured hello", file=sys.stderr)
        return

    await hello(create_process_environment())


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
