from __future__ import annotations

from dataclasses import dataclass, field

from devalbo_ilc.generated.types import ConsoleIo, Environment
from devalbo_ilc.hosts.callbacks import ConsoleIoCallbacks, console_io_from_callbacks


@dataclass
class LogEntry:
    level: str
    message: str


@dataclass
class TestEnvironment:
    stdin: list[str] = field(default_factory=list)
    logs: list[LogEntry] = field(default_factory=list)
    _stdin_index: int = 0
    _console_io: ConsoleIo | None = None

    async def consoleIo(self) -> ConsoleIo:
        if self._console_io is None:
            self._console_io = console_io_from_callbacks(
                ConsoleIoCallbacks(
                    on_info=lambda message: self._log("info", message),
                    on_error=lambda message: self._log("error", message),
                    on_read_line=self._read_line,
                )
            )
        return self._console_io

    def _log(self, level: str, message: str) -> None:
        self.logs.append(LogEntry(level=level, message=message))

    def _read_line(self) -> str | None:
        if self._stdin_index >= len(self.stdin):
            return None
        line = self.stdin[self._stdin_index]
        self._stdin_index += 1
        return line if line else None


def create_test_environment(*, stdin: list[str] | None = None) -> TestEnvironment:
    return TestEnvironment(stdin=list(stdin or []))
