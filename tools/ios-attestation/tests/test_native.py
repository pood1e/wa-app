import asyncio
import base64
import hashlib
import multiprocessing
import signal
from pathlib import Path
from types import SimpleNamespace

import cbor2
import pytest
from filelock import FileLock

from wa_ios_attestation import native
from wa_ios_attestation.binding import NativeBinding, NativeUnavailable
from wa_ios_attestation.native import DeviceSigner, _assertion


def test_assertion_preserves_native_bytes_and_rejects_trailing_data():
    raw = cbor2.dumps({"signature": b"x" * 70, "authenticatorData": b"y" * 37})
    assert _assertion(base64.b64encode(raw).decode()) == raw
    with pytest.raises(NativeUnavailable):
        _assertion(base64.b64encode(raw + b"extra").decode())


def test_binding_repr_excludes_key_identifier():
    binding = NativeBinding("PRIVATE_KEY_SENTINEL", "synthetic.app")
    assert "PRIVATE" not in repr(binding)


def test_create_requires_only_key_and_app_identity(monkeypatch):
    identifier = base64.b64encode(bytes(32)).decode()

    async def response(*_):
        return {"keyIdentifier": identifier, "bundleIdentifier": "synthetic.app"}

    signer = DeviceSigner("synthetic-device")
    monkeypatch.setattr(signer, "_call", response)
    assert asyncio.run(signer.create()) == NativeBinding(identifier, "synthetic.app")


def _waiting_child(_pipe, directory, _operation, _binding, _digest, _timeout):
    def stop(*_):
        (Path(directory) / "cleaned").touch()
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, stop)
    (Path(directory) / "started").touch()
    signal.pause()


def test_cancellation_joins_native_child(monkeypatch, tmp_path):
    monkeypatch.setattr(native, "_child", _waiting_child)

    async def run():
        call = asyncio.create_task(DeviceSigner(str(tmp_path)).create())
        async with asyncio.timeout(5):
            while not (tmp_path / "started").exists():
                await asyncio.sleep(0.02)
        call.cancel()
        with pytest.raises(asyncio.CancelledError):
            await call
        assert (tmp_path / "cleaned").exists()
        assert not any(p.name == "wa-app-attest-native" for p in multiprocessing.active_children())

    asyncio.run(run())


def test_native_call_observes_only_the_existing_app_until_attachment_cleanup(monkeypatch, tmp_path):
    attachments = []
    cleaned = []
    messages = []
    calls = []

    class Script:
        def __init__(self, source):
            assert "SBGetScreenLockStatus" not in source
            assert "SpringBoardServices" not in source
            self.exports_sync = SimpleNamespace(
                create=lambda: calls.append("create") or {"synthetic": True},
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
            return [SimpleNamespace(identifier="net.whatsapp.WhatsApp", pid=42)]

        def attach(self, pid, **options):
            assert calls == ["observe"]
            attachments.append((pid, options))
            return Session()

    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: [Device()])
    monkeypatch.setattr(native.Path, "home", lambda: tmp_path)
    monkeypatch.setattr(native.signal, "signal", lambda *_: None)
    monkeypatch.setattr(native.signal, "alarm", lambda *_: None)

    async def observed(serial, pid, operation, _cancel):
        assert serial == "synthetic-device" and pid == 42
        calls.append("observe")
        try:
            return operation()
        finally:
            assert cleaned == ["agent", "session"]
            calls.append("stop_observing")

    monkeypatch.setattr(native, "run_observed", observed)
    pipe = SimpleNamespace(send=messages.append, close=lambda: None)
    alias = hashlib.sha256(b"synthetic-device").hexdigest()[:10]
    native._child(pipe, alias, "create", None, b"", 20)

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
    assert calls == ["observe", "create", "stop_observing"]
    assert messages == [("ok", {"synthetic": True})]


def test_observation_failure_skips_attachment_and_releases_device_lock(monkeypatch, tmp_path):
    calls, messages = [], []

    class Device:
        type = "usb"
        id = "synthetic-device"

        def enumerate_applications(self):
            return [SimpleNamespace(identifier="net.whatsapp.WhatsApp", pid=42)]

        def attach(self, *_args, **_options):
            calls.append("attach")

    monkeypatch.setattr(native.Path, "home", lambda: tmp_path)
    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: [Device()])
    monkeypatch.setattr(native.signal, "signal", lambda *_: None)
    monkeypatch.setattr(native.signal, "alarm", lambda *_: None)

    async def observed(_serial, pid, _operation, _cancel):
        assert pid == 42
        raise OSError("synthetic developer connection failure")

    monkeypatch.setattr(native, "run_observed", observed)
    pipe = SimpleNamespace(send=messages.append, close=lambda: None)
    alias = hashlib.sha256(b"synthetic-device").hexdigest()[:10]
    native._child(pipe, alias, "create", None, b"", 20)
    assert calls == []
    assert messages == [("unavailable", None)]
    with FileLock(tmp_path / ".cache" / "ios-device-locks" / f"{alias}.lock", timeout=0):
        pass


def test_device_lock_rejects_another_worker_before_device_access(monkeypatch, tmp_path):
    calls, messages = [], []
    monkeypatch.setattr(native.Path, "home", lambda: tmp_path)
    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: calls.append("discover") or [])
    directory = tmp_path / ".cache" / "ios-device-locks"
    directory.mkdir(parents=True)
    pipe = SimpleNamespace(send=messages.append, close=lambda: None)
    with FileLock(directory / "synthetic-device.lock", timeout=0):
        native._child(pipe, "synthetic-device", "create", None, b"", 20)
    assert calls == []
    assert messages == [("ok", {"error": "device_busy"})]


def test_stopped_app_stays_stopped_and_releases_device_lock(monkeypatch, tmp_path):
    calls, messages = [], []

    class Device:
        type = "usb"
        id = "synthetic-device"

        def enumerate_applications(self):
            return [SimpleNamespace(identifier="net.whatsapp.WhatsApp", pid=0)]

        def attach(self, *_args, **_options):
            calls.append("attach")

        def spawn(self, *_args, **_options):
            calls.append("spawn")

        def resume(self, *_args, **_options):
            calls.append("resume")

    monkeypatch.setattr(native.Path, "home", lambda: tmp_path)
    monkeypatch.setattr(native.frida, "enumerate_devices", lambda: [Device()])
    monkeypatch.setattr(native.signal, "signal", lambda *_: None)
    monkeypatch.setattr(native.signal, "alarm", lambda *_: None)
    pipe = SimpleNamespace(send=messages.append, close=lambda: None)
    alias = hashlib.sha256(b"synthetic-device").hexdigest()[:10]
    native._child(pipe, alias, "create", None, b"", 20)
    assert calls == []
    assert messages == [("unavailable", None)]
    with FileLock(tmp_path / ".cache" / "ios-device-locks" / f"{alias}.lock", timeout=0):
        pass
