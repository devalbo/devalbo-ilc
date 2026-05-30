from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass

from devalbo_ilc.generated.types import ConsoleIo


@dataclass(frozen=True)
class ConsoleIoCallbacks:
    on_info: Callable[[str], Awaitable[None] | None]
    on_error: Callable[[str], Awaitable[None] | None]
    on_read_line: Callable[[], Awaitable[str | None] | str | None]


class CallbackConsoleIo:
    def __init__(self, callbacks: ConsoleIoCallbacks) -> None:
        self._callbacks = callbacks

    async def info(self, message: str) -> None:
        result = self._callbacks.on_info(message)
        if result is not None:
            await result

    async def error(self, message: str) -> None:
        result = self._callbacks.on_error(message)
        if result is not None:
            await result

    async def readLine(self) -> str | None:
        result = self._callbacks.on_read_line()
        if result is not None and hasattr(result, "__await__"):
            return await result
        return result


def console_io_from_callbacks(callbacks: ConsoleIoCallbacks) -> ConsoleIo:
    return CallbackConsoleIo(callbacks)
