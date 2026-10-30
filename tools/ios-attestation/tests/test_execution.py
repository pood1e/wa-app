import asyncio
import hashlib
import signal
from contextlib import asynccontextmanager
from threading import Event
from types import SimpleNamespace

import pytest

from wa_ios_attestation import execution, native


def observation(monkeypatch, events, *, fail_start=False, fail_stop=False):
    async def close():
        events.append("provider_closed")

    async def provider(serial):
        assert serial == "synthetic-device"
        return SimpleNamespace(close=close)

    @asynccontextmanager
    async def dvt(_provider):
        yield "developer-channel"

    async def start(pid):
        assert pid == 42
        if fail_start:
            raise OSError("synthetic observation failure")
        events.append("observed")

    async def stop(pid):
        assert pid == 42
        events.append("released")
        if fail_stop:
            raise OSError("synthetic release failure")

    @asynccontextmanager
    async def control(channel):
        assert channel == "developer-channel"
        yield SimpleNamespace(
            service=SimpleNamespace(startObservingPid_=start, stopObservingPid_=stop)
        )

    monkeypatch.setattr(execution, "_provider", provider)
    monkeypatch.setattr(execution, "DvtProvider", dvt)
    monkeypatch.setattr(execution, "ProcessControl", control)


@pytest.mark.parametrize("fails", [False, True])
def test_observation_covers_native_cleanup_before_release(monkeypatch, fails):
    events = []
    observation(monkeypatch, events)

    def operation():
        assert events == ["observed"]
        try:
            if fails:
                raise ValueError("synthetic SDK failure")
            return "proof"
        finally:
            events.append("native_cleaned")

    call = execution.run_observed("synthetic-device", 42, operation, lambda: None)
    if fails:
        with pytest.raises(ValueError):
            asyncio.run(call)
    else:
        assert asyncio.run(call) == "proof"
    assert events == ["observed", "native_cleaned", "released", "provider_closed"]


@pytest.mark.parametrize("fails_at_start", [False, True])
def test_observation_failure_closes_provider_and_rejects_a_successful_result(
    monkeypatch, fails_at_start
):
    events = []
    observation(monkeypatch, events, fail_start=fails_at_start, fail_stop=not fails_at_start)

    def operation():
        events.append("native_cleaned")
        return "proof"

    with pytest.raises(OSError):
        asyncio.run(execution.run_observed("synthetic-device", 42, operation, lambda: None))
    if fails_at_start:
        assert events == ["provider_closed"]
    else:
        assert events == ["observed", "native_cleaned", "released", "provider_closed"]


def test_cancellation_waits_for_native_cleanup_before_releasing_observation(monkeypatch):
    events = []
    observation(monkeypatch, events)
    running, cancelled = Event(), Event()

    def operation():
        running.set()
        assert cancelled.wait(2)
        events.append("native_cleaned")

    async def run():
        call = asyncio.create_task(
            execution.run_observed("synthetic-device", 42, operation, cancelled.set)
        )
        async with asyncio.timeout(3):
            while not running.is_set():
                await asyncio.sleep(0.01)
        call.cancel()
        with pytest.raises(asyncio.CancelledError):
            await call

    asyncio.run(run())
    assert events == ["observed", "native_cleaned", "released", "provider_closed"]


def test_native_stop_signals_keep_observation_until_thread_cleanup(monkeypatch, tmp_path):
    events, messages, handlers = [], [], {}
    observation(monkeypatch, events)
    cancelled = Event()

    class Cancellation:
        def __enter__(self):
            return self

        def __exit__(self, *_):
            pass

        def cancel(self):
            cancelled.set()

    device = SimpleNamespace(
        id="synthetic-device",
        type="usb",
        enumerate_applications=lambda: [
            SimpleNamespace(identifier="net.whatsapp.WhatsApp", pid=42)
        ],
    )

    def invoke(*_):
        assert cancelled.wait(2)
        events.append("native_cleaned")

    def stop():
        handlers[signal.SIGTERM]()
        handlers[signal.SIGTERM]()

    original = native.run_observed

    async def observed(*args):
        asyncio.get_running_loop().call_later(0.02, stop)
        return await original(*args)

    monkeypatch.setattr(native.Path, "home", lambda: tmp_path)
    monkeypatch.setattr(native.frida, "Cancellable", Cancellation)
    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: [device])
    monkeypatch.setattr(native.signal, "signal", lambda sig, fn: handlers.__setitem__(sig, fn))
    monkeypatch.setattr(native.signal, "alarm", lambda *_: None)
    monkeypatch.setattr(native, "run_observed", observed)
    monkeypatch.setattr(native, "invoke", invoke)
    alias = hashlib.sha256(device.id.encode()).hexdigest()[:10]
    native._child(
        SimpleNamespace(send=messages.append, close=lambda: None), alias, "create", None, b"", 20
    )
    assert events == ["observed", "native_cleaned", "released", "provider_closed"]
    assert messages == [("unavailable", None)]
