"""Run and release a single Frida app session under an execution observation."""

from __future__ import annotations

import inspect
import logging
from pathlib import Path
from threading import Timer

import frida
import frida_tools

from wa_ios_attestation.binding import NativeBinding, NativeUnavailable

logger = logging.getLogger(__name__)


def invoke(
    device: frida.core.Device,
    pid: int,
    operation: str,
    binding: NativeBinding | None,
    digest: bytes,
    cancellation: frida.Cancellable,
) -> dict[str, object]:
    session = script = None
    stage = "attach"
    try:
        with cancellation:
            session = device.attach(
                pid,
                exceptor="off",
                unwind_broker=False,
                exit_monitor=False,
                thread_suspend_monitor=False,
            )
            bridge = Path(inspect.getfile(frida_tools)).parent / "bridges/objc.js"
            stage = "load"
            script = session.create_script(
                bridge.read_text()
                + "\nglobalThis.ObjC = bridge;\n"
                + Path(__file__).with_name("blocks.js").read_text()
                + "\n"
                + Path(__file__).with_name("agent.js").read_text()
                + "\n"
                + Path(__file__).with_name("recaptcha.js").read_text()
            )
            script.load()
            stage = operation
            if operation == "recaptcha":
                return script.exports_sync.recaptcha(digest.decode("ascii"))
            if binding is None:
                return script.exports_sync.create()
            return getattr(script.exports_sync, operation)(
                binding.key_identifier, binding.bundle_identifier, digest.hex()
            )
    except Exception as error:
        logger.warning("native_call_failed stage=%s error=%s", stage, type(error).__name__)
        raise
    finally:
        failed = False
        for resource, action in ((script, "unload"), (session, "detach")):
            if resource is None:
                continue
            cleanup = frida.Cancellable()
            timer = Timer(3, cleanup.cancel)
            timer.daemon = True
            timer.start()
            try:
                with cleanup:
                    getattr(resource, action)()
            except Exception as error:
                logger.warning(
                    "native_cleanup_failed stage=%s error=%s", action, type(error).__name__
                )
                failed = True
            finally:
                timer.cancel()
        if failed:
            raise NativeUnavailable
