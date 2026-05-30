from __future__ import annotations

import sys
from dataclasses import dataclass

from devalbo_ilc.generated.types import ConsoleIo, Environment
from devalbo_ilc.hosts.callbacks import ConsoleIoCallbacks, console_io_from_callbacks


@dataclass
class ProcessEnvironment:
    """CPython process host: stdout/stderr and interactive stdin."""

    non_interactive: bool = False
    _console_io: ConsoleIo | None = None

    async def consoleIo(self) -> ConsoleIo:
        if self._console_io is None:
            self._console_io = console_io_from_callbacks(
                ConsoleIoCallbacks(
                    on_info=lambda message: sys.stdout.write(message + "\n"),
                    on_error=lambda message: sys.stderr.write(message + "\n"),
                    on_read_line=self._read_line,
                )
            )
        return self._console_io

    def _read_line(self) -> str | None:
        if self.non_interactive:
            return None
        try:
            line = input()
        except EOFError:
            return None
        return line if line else None


def create_process_environment(*, non_interactive: bool = False) -> Environment:
    return ProcessEnvironment(non_interactive=non_interactive)


def create_serverless_environment() -> Environment:
    return ProcessEnvironment(non_interactive=True)
