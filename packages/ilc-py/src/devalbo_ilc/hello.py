from __future__ import annotations

from devalbo_ilc.generated.types import Environment


async def hello(env: Environment) -> None:
    console_io = await env.consoleIo()
    await console_io.info("hello from ILC")
