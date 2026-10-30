"""Bound each device SDK call to an isolated Frida subprocess."""

from __future__ import annotations

import asyncio
import hashlib
import inspect
import multiprocessing
import os
import signal
import subprocess
from multiprocessing.connection import Connection
from pathlib import Path
from threading import Timer

import frida
import frida_tools

from wa_android_attestation.binding import ContextNotReady, NativeUnavailable


def _expired(*_: object) -> None:
    raise TimeoutError("native_deadline")


def _warmup() -> None:
    pass


def _set_freeze(device_id: str, pid: int, sticky: bool) -> None:
    command = ["adb", "-s", device_id, "shell", "cmd", "activity", "unfreeze"]
    if sticky:
        command.append("--sticky")
    command.append(str(pid))
    result = subprocess.run(command, capture_output=True, timeout=3, check=False)
    if result.returncode != 0:
        raise NativeUnavailable


def _child(
    pipe: Connection, device_alias: str, operation: str, identifier: str, payload: bytes
) -> None:
    session = script = None
    result = None
    lease = None
    cancellation = frida.Cancellable()
    timeout = 60 if operation == "integrity" else 20
    timer = Timer(timeout, cancellation.cancel)
    timer.daemon = True
    signal.signal(signal.SIGALRM, _expired)
    signal.signal(signal.SIGTERM, _expired)
    signal.alarm(timeout + 2)
    timer.start()
    try:
        with cancellation:
            device = next(
                item
                for item in frida.enumerate_devices()
                if item.type == "usb"
                and hashlib.sha256(item.id.encode()).hexdigest()[:10] == device_alias
            )
            app = next(
                item
                for item in device.enumerate_applications()
                if item.identifier == "com.whatsapp" and item.pid
            )
            lease = (device.id, app.pid)
            _set_freeze(*lease, sticky=True)
            session = device.attach(
                app.pid,
                exceptor="off",
                unwind_broker=False,
                exit_monitor=False,
                thread_suspend_monitor=False,
            )
            bridge = Path(inspect.getfile(frida_tools)).parent / "bridges/java.js"
            source = bridge.read_text() + "\nglobalThis.Java = bridge;\n"
            source += Path(__file__).with_name("agent.js").read_text()
            script = session.create_script(source)
            script.load()
            arguments = [payload.hex()] if operation == "integrity" else [identifier]
            if operation in {"create", "sign"}:
                arguments.append(payload.hex())
            result = getattr(script.exports_sync, operation)(*arguments)
    except Exception:
        result = None
    finally:
        timer.cancel()
        signal.alarm(0)
        for resource, method in ((script, "unload"), (session, "detach")):
            if resource is None:
                continue
            cleanup = frida.Cancellable()
            cleanup_timer = Timer(3, cleanup.cancel)
            cleanup_timer.daemon = True
            cleanup_timer.start()
            signal.alarm(4)
            try:
                with cleanup:
                    getattr(resource, method)()
            except Exception:
                result = None
            finally:
                cleanup_timer.cancel()
                signal.alarm(0)
        if lease is not None:
            try:
                _set_freeze(*lease, sticky=False)
            except (NativeUnavailable, OSError, subprocess.TimeoutExpired):
                result = None
        try:
            pipe.send(result)
        except (BrokenPipeError, OSError):
            pass
        finally:
            pipe.close()


class DeviceSigner:
    def __init__(self, device_alias: str) -> None:
        self.device_alias = device_alias
        self._process_context = multiprocessing.get_context("forkserver")
        self._process_context.set_forkserver_preload([])

    def prepare(self) -> None:
        process = self._process_context.Process(target=_warmup)
        process.start()
        process.join(10)
        try:
            if process.is_alive():
                process.kill()
                process.join(2)
                raise NativeUnavailable
            if process.exitcode != 0:
                raise NativeUnavailable
        finally:
            process.close()

    async def call(self, operation: str, identifier: str, payload: bytes = b"") -> dict:
        timeout = 72 if operation == "integrity" else 30
        reader, writer = multiprocessing.Pipe(duplex=False)
        process = self._process_context.Process(
            name="wa-android-attestation-native",
            target=_child,
            args=(writer, self.device_alias, operation, identifier, payload),
            daemon=True,
        )
        try:
            process.start()
        except BaseException:
            reader.close()
            writer.close()
            process.close()
            raise
        writer.close()
        try:
            async with asyncio.timeout(timeout):
                while not reader.poll():
                    if not process.is_alive():
                        raise NativeUnavailable
                    await asyncio.sleep(0.02)
                value = reader.recv()
            if not isinstance(value, dict):
                raise NativeUnavailable
            if value.get("error") == "context_not_ready":
                raise ContextNotReady
            if value.get("error"):
                raise NativeUnavailable
            return value
        except EOFError:
            raise NativeUnavailable from None
        finally:
            cleanup = asyncio.create_task(self._stop(process))
            try:
                await asyncio.shield(cleanup)
            except asyncio.CancelledError:
                await cleanup
                raise
            finally:
                reader.close()

    @staticmethod
    async def _stop(process: multiprocessing.Process) -> None:
        await asyncio.to_thread(process.join, 0.2)
        if process.is_alive():
            try:
                os.kill(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        await asyncio.to_thread(process.join, 12)
        if process.is_alive():
            process.kill()
            await asyncio.to_thread(process.join, 2)
        process.close()
