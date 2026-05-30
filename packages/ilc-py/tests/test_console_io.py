"""ConsoleIo host tests (stdlib unittest; no external deps).

Includes a regression for the process-host bug where a sync callback returning a
non-None, non-awaitable value (e.g. ``sys.stdout.write`` -> int) was awaited.
Run: ``PYTHONPATH=src python3 -m unittest discover -s tests``
"""

from __future__ import annotations

import unittest

from devalbo_ilc.hello import hello
from devalbo_ilc.hosts.callbacks import ConsoleIoCallbacks, console_io_from_callbacks
from devalbo_ilc.hosts.process import create_process_environment
from devalbo_ilc.hosts.test_host import create_test_environment


class ProcessHostTests(unittest.IsolatedAsyncioTestCase):
    async def test_info_error_do_not_crash(self) -> None:
        # Regression: stdout/stderr .write() returns an int; info/error must not await it.
        env = create_process_environment(non_interactive=True)
        cio = await env.consoleIo()
        await cio.info("info line")
        await cio.error("error line")

    async def test_read_line_eof_when_non_interactive(self) -> None:
        env = create_process_environment(non_interactive=True)
        cio = await env.consoleIo()
        self.assertIsNone(await cio.readLine())


class CallbackConsoleIoTests(unittest.IsolatedAsyncioTestCase):
    async def test_sync_non_awaitable_result_is_not_awaited(self) -> None:
        # Directly the bug class: a callback returning a non-None, non-awaitable value.
        cio = console_io_from_callbacks(
            ConsoleIoCallbacks(
                on_info=lambda _m: 42,
                on_error=lambda _m: 42,
                on_read_line=lambda: None,
            )
        )
        await cio.info("x")
        await cio.error("y")  # must not raise

    async def test_async_callbacks_are_awaited(self) -> None:
        seen: list[str] = []

        async def record(message: str) -> None:
            seen.append(message)

        cio = console_io_from_callbacks(
            ConsoleIoCallbacks(on_info=record, on_error=record, on_read_line=lambda: None)
        )
        await cio.info("a")
        await cio.error("b")
        self.assertEqual(seen, ["a", "b"])


class TestEnvironmentTests(unittest.IsolatedAsyncioTestCase):
    async def test_captures_logs_and_consumes_stdin(self) -> None:
        env = create_test_environment(stdin=["one", "two"])
        cio = await env.consoleIo()
        await cio.info("hi")
        await cio.error("boom")
        self.assertEqual(
            [(e.level, e.message) for e in env.logs],
            [("info", "hi"), ("error", "boom")],
        )
        self.assertEqual(await cio.readLine(), "one")
        self.assertEqual(await cio.readLine(), "two")
        self.assertIsNone(await cio.readLine())

    async def test_hello_handler_against_test_host(self) -> None:
        env = create_test_environment()
        await hello(env)
        self.assertEqual(
            [(e.level, e.message) for e in env.logs],
            [("info", "hello from ILC")],
        )


if __name__ == "__main__":
    unittest.main()
