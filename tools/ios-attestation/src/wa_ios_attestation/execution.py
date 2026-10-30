"""Keep an existing app executable while one native SDK call owns the device."""

from __future__ import annotations

import asyncio
from collections.abc import Callable
from typing import TypeVar

from pymobiledevice3.remote.remote_service_discovery import RemoteServiceDiscoveryService
from pymobiledevice3.services.dvt.instruments.dvt_provider import DvtProvider
from pymobiledevice3.services.dvt.instruments.process_control import ProcessControl

T = TypeVar("T")


async def _provider(serial: str) -> RemoteServiceDiscoveryService:
    from pymobiledevice3.remote.native_tunnel import establish_native_rsd

    return await establish_native_rsd(serial=serial)


async def run_observed(
    serial: str, pid: int, operation: Callable[[], T], cancel: Callable[[], None]
) -> T:
    provider = await _provider(serial)
    try:
        async with DvtProvider(provider) as dvt, ProcessControl(dvt) as control:
            await control.service.startObservingPid_(pid)
            try:
                work = asyncio.create_task(asyncio.to_thread(operation))
                try:
                    return await asyncio.shield(work)
                except asyncio.CancelledError:
                    cancel()
                    try:
                        await work
                    except Exception:
                        pass
                    raise
            finally:
                async with asyncio.timeout(3):
                    await control.service.stopObservingPid_(pid)
    finally:
        async with asyncio.timeout(3):
            await provider.close()
