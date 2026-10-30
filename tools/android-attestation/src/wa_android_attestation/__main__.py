from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import signal
import stat
from pathlib import Path

from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from wa_android_attestation.app import create_server
from wa_android_attestation.native import DeviceSigner


async def serve(socket: Path, signer: DeviceSigner) -> None:
    socket.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    parent = socket.parent.stat()
    if parent.st_uid != os.getuid() or parent.st_mode & 0o077:
        raise ValueError("socket_directory_not_private")
    if socket.exists() or socket.is_symlink():
        raise ValueError("socket_already_exists")
    os.umask(0o077)
    server, application = create_server(signer)
    status = health.aio.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(status, server)
    await status.set("", health_pb2.HealthCheckResponse.SERVING)
    await status.set(
        "byte.v.forge.waapp.v1.AndroidAttestationService", health_pb2.HealthCheckResponse.SERVING
    )
    if not server.add_insecure_port(f"unix://{socket}"):
        raise RuntimeError("socket_unavailable")
    os.chmod(socket, 0o600)
    inode = socket.stat().st_ino
    stopped = asyncio.Event()
    loop = asyncio.get_running_loop()
    for name in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(name, stopped.set)
    try:
        await server.start()
        reaper = asyncio.create_task(application.reap(stopped))
        print(json.dumps({"event": "ready"}), flush=True)
        await stopped.wait()
    finally:
        stopped.set()
        if "reaper" in locals():
            await reaper
        await status.enter_graceful_shutdown()
        await server.stop(10)
        async with asyncio.timeout(30):
            await application.close()
        if (
            socket.exists()
            and stat.S_ISSOCK(socket.stat().st_mode)
            and socket.stat().st_ino == inode
        ):
            socket.unlink()


def main() -> None:
    parser = argparse.ArgumentParser(description="Private Android KeyStore and Integrity worker")
    parser.add_argument("--socket", required=True, type=Path)
    parser.add_argument("--device", required=True)
    args = parser.parse_args()
    if not args.socket.is_absolute() or re.fullmatch(r"[a-f0-9]{10}", args.device) is None:
        parser.error("absolute socket path and hashed device alias required")
    signer = DeviceSigner(args.device)
    signer.prepare()
    asyncio.run(serve(args.socket, signer))


if __name__ == "__main__":
    main()
