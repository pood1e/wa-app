import asyncio
import hashlib
import multiprocessing
import signal
from pathlib import Path
from types import SimpleNamespace

import pytest

from wa_android_attestation import native


def _waiting_child(_pipe, directory, _operation, _identifier, _payload):
    def stop(*_):
        (Path(directory) / "cleaned").touch()
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, stop)
    (Path(directory) / "started").touch()
    signal.pause()


def test_cancellation_joins_native_child(monkeypatch, tmp_path):
    monkeypatch.setattr(native, "_child", _waiting_child)

    async def run():
        call = asyncio.create_task(native.DeviceSigner(str(tmp_path)).call("sign", "context"))
        async with asyncio.timeout(5):
            while not (tmp_path / "started").exists():
                await asyncio.sleep(0.02)
        call.cancel()
        with pytest.raises(asyncio.CancelledError):
            await call
        assert (tmp_path / "cleaned").exists()
        assert not any(
            p.name == "wa-android-attestation-native" for p in multiprocessing.active_children()
        )

    asyncio.run(run())


def test_native_sdk_call_attaches_once_and_detaches(monkeypatch):
    attachments, cleaned, messages, calls = [], [], [], []
    leases = []

    class Script:
        def __init__(self, source):
            self.exports_sync = SimpleNamespace(
                sign=lambda *args: calls.append(args) or {"signature": "synthetic"}
            )

        def load(self):
            pass

        def unload(self):
            cleaned.append("agent")

    class Session:
        def create_script(self, source):
            return Script(source)

        def detach(self):
            cleaned.append("session")

    class Device:
        type = "usb"
        id = "synthetic-device"

        def enumerate_applications(self):
            return [SimpleNamespace(identifier="com.whatsapp", pid=42)]

        def attach(self, pid, **options):
            attachments.append((pid, options))
            return Session()

    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: [Device()])
    monkeypatch.setattr(native, "_set_freeze", lambda *args, sticky: leases.append((*args, sticky)))
    monkeypatch.setattr(native.signal, "signal", lambda *_: None)
    monkeypatch.setattr(native.signal, "alarm", lambda *_: None)
    pipe = SimpleNamespace(send=messages.append, close=lambda: None)
    alias = hashlib.sha256(b"synthetic-device").hexdigest()[:10]
    native._child(pipe, alias, "sign", "synthetic-context", b"exact-ENC")
    assert attachments == [
        (
            42,
            {
                "exceptor": "off",
                "unwind_broker": False,
                "exit_monitor": False,
                "thread_suspend_monitor": False,
            },
        )
    ]
    assert cleaned == ["agent", "session"]
    assert leases == [("synthetic-device", 42, True), ("synthetic-device", 42, False)]
    assert calls == [("synthetic-context", b"exact-ENC".hex())]
    assert messages == [{"signature": "synthetic"}]
