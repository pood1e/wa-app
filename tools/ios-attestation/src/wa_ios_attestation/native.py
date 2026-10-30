"""Isolate each Frida call so cancellation also bounds synchronous native RPC."""

from __future__ import annotations

import asyncio
import base64
import hashlib
import logging
import multiprocessing
import os
import signal
from multiprocessing.connection import Connection
from pathlib import Path
from threading import Timer

import cbor2
import frida
from filelock import FileLock, Timeout

from wa_ios_attestation.attestation import decode_cbor, key_attestation
from wa_ios_attestation.binding import (
    ContextNotReady,
    DeviceBusy,
    NativeBinding,
    NativeUnavailable,
)
from wa_ios_attestation.execution import run_observed
from wa_ios_attestation.session import invoke

logger = logging.getLogger(__name__)


def _expired(*_: object) -> None:
    raise TimeoutError("native_deadline")


def _warmup() -> None:
    pass


def _child(
    pipe: Connection,
    device_alias: str,
    operation: str,
    binding: NativeBinding | None,
    digest: bytes,
    timeout: int,
) -> None:
    directory = Path.home() / ".cache" / "ios-device-locks"
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    device_lock = FileLock(directory / f"{device_alias}.lock", timeout=0)
    try:
        device_lock.acquire()
    except Timeout:
        try:
            pipe.send(("ok", {"error": "device_busy"}))
        finally:
            pipe.close()
        return
    result = ("unavailable", None)
    cancellation = frida.Cancellable()
    active: asyncio.Task | None = None
    timer = Timer(timeout, cancellation.cancel)
    timer.daemon = True

    def expired(*args):
        cancellation.cancel()
        if active is None:
            _expired(*args)
        elif not active.cancelling():
            active.cancel()

    signal.signal(signal.SIGALRM, expired)
    signal.signal(signal.SIGTERM, expired)
    signal.alarm(timeout + 2)
    timer.start()
    stage = "discover"
    try:
        with cancellation:
            device = next(
                d
                for d in frida.enumerate_devices()
                if d.type == "usb"
                and hashlib.sha256(d.id.encode()).hexdigest()[:10] == device_alias
            )
            stage = "discover_application"
            application = next(
                (
                    app
                    for app in device.enumerate_applications()
                    if app.identifier == "net.whatsapp.WhatsApp" and app.pid
                ),
                None,
            )
            if application is None:
                raise NativeUnavailable
            pid = application.pid
        stage = "observe_process"

        async def execute():
            nonlocal active
            active = asyncio.current_task()
            try:
                return await run_observed(
                    device.id,
                    pid,
                    lambda: invoke(device, pid, operation, binding, digest, cancellation),
                    cancellation.cancel,
                )
            except asyncio.CancelledError:
                raise NativeUnavailable from None
            finally:
                active = None

        value = asyncio.run(execute())
        result = ("ok", value)
    except Exception as error:
        logger.warning("native_call_failed stage=%s error=%s", stage, type(error).__name__)
        result = ("unavailable", None)
    finally:
        timer.cancel()
        signal.alarm(0)
        device_lock.release()
        try:
            pipe.send(result)
        except (BrokenPipeError, OSError):
            pass
        finally:
            pipe.close()


def _assertion(encoded: str) -> bytes:
    if len(encoded) > 8192:
        raise NativeUnavailable
    try:
        raw = base64.b64decode(encoded, validate=True)
        value = decode_cbor(raw)
    except (ValueError, cbor2.CBORDecodeError):
        raise NativeUnavailable from None
    if len(raw) > 4096:
        raise NativeUnavailable
    if (
        set(value) != {"signature", "authenticatorData"}
        or not isinstance(value["signature"], bytes)
        or not 8 <= len(value["signature"]) <= 80
        or not isinstance(value["authenticatorData"], bytes)
        or len(value["authenticatorData"]) != 37
    ):
        raise NativeUnavailable
    return raw


class DeviceSigner:
    def __init__(self, device_alias: str) -> None:
        self.device_alias = device_alias
        self._process_context = multiprocessing.get_context("forkserver")
        self._process_context.set_forkserver_preload([])

    def prepare(self) -> None:
        # Start the clean fork server before gRPC creates native threads.
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

    async def create(self) -> NativeBinding:
        value = await self._call("create", None, b"")
        fields = ("keyIdentifier", "bundleIdentifier")
        if not all(
            isinstance(value.get(key), str) and 1 <= len(value[key]) <= 256 for key in fields
        ):
            raise NativeUnavailable
        try:
            key = base64.b64decode(value["keyIdentifier"], validate=True)
        except ValueError:
            raise NativeUnavailable from None
        if len(key) != 32:
            raise NativeUnavailable
        return NativeBinding(*(value[key] for key in fields))

    async def attest(self, binding: NativeBinding, digest: bytes) -> bytes:
        value = await self._call("attest", binding, digest)
        encoded = value.get("proof")
        if not isinstance(encoded, str):
            raise NativeUnavailable
        return key_attestation(binding.key_identifier, encoded, digest)

    async def sign(self, binding: NativeBinding, digest: bytes) -> bytes:
        value = await self._call("sign", binding, digest)
        encoded = value.get("proof")
        if not isinstance(encoded, str):
            raise NativeUnavailable
        return _assertion(encoded)

    async def login_token(self, site_key: str) -> str:
        value = await self._call("recaptcha", None, site_key.encode("ascii"))
        token = value.get("token")
        if not isinstance(token, str) or not 1 <= len(token) <= 16384 or not token.isascii():
            raise NativeUnavailable
        return token

    async def _call(
        self, operation: str, binding: NativeBinding | None, digest: bytes
    ) -> dict[str, object]:
        timeout = 60 if operation in {"attest", "recaptcha"} else 20
        reader, writer = multiprocessing.Pipe(duplex=False)
        process = self._process_context.Process(
            name="wa-app-attest-native",
            target=_child,
            args=(writer, self.device_alias, operation, binding, digest, timeout),
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
            async with asyncio.timeout(timeout + 4):
                while not reader.poll():
                    if not process.is_alive():
                        raise NativeUnavailable
                    await asyncio.sleep(0.02)
                status, value = reader.recv()
            if status != "ok" or not isinstance(value, dict):
                raise NativeUnavailable
            if value.get("error") == "context_not_ready":
                raise ContextNotReady
            if value.get("error") == "device_busy":
                raise DeviceBusy
            if value.get("error") == "server_unavailable":
                raise NativeUnavailable
            return value
        except (EOFError, TimeoutError):
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
        await asyncio.to_thread(process.join, 9)
        if process.is_alive():
            process.kill()
            await asyncio.to_thread(process.join, 2)
        process.close()
