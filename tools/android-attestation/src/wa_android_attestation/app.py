"""Short-lived Android keys and SDK outcomes behind the canonical Proto API."""

from __future__ import annotations

import asyncio
import base64
import logging
import re
import secrets
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Protocol, TypeVar

import grpc
from byte.v.forge.waapp.v1 import android_attestation_pb2 as pb
from byte.v.forge.waapp.v1 import android_attestation_pb2_grpc as rpc
from google.protobuf.empty_pb2 import Empty

from wa_android_attestation.binding import ContextNotReady, NativeUnavailable

T = TypeVar("T")
logger = logging.getLogger(__name__)


class Signer(Protocol):
    async def call(self, operation: str, identifier: str, payload: bytes = b"") -> dict: ...


@dataclass
class Session:
    public_key: bytes = field(repr=False)
    expires: float
    certificates: list[bytes] = field(default_factory=list, repr=False)


class AndroidAttestationService(rpc.AndroidAttestationServiceServicer):
    def __init__(self, signer: Signer, clock: Callable[[], float] = time.monotonic) -> None:
        self._signer = signer
        self._clock = clock
        self._sessions: dict[str, Session] = {}
        self._busy = asyncio.Lock()

    async def CreateContext(
        self, request: pb.CreateAndroidAttestationContextRequest, context: grpc.aio.ServicerContext
    ) -> pb.AndroidKeyAttestation:
        if len(request.registration_public_key) != 32:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "registration_public_key_invalid")

        async def create() -> pb.AndroidKeyAttestation:
            await self._reap()
            if len(self._sessions) >= 32:
                await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "attestation_capacity")
            identifier = secrets.token_hex(16)
            session = Session(bytes(request.registration_public_key), self._clock() + 1800)
            self._sessions[identifier] = session
            try:
                result = await self._signer.call("create", identifier, session.public_key)
                encoded = result.get("certificates")
                if not isinstance(encoded, list) or not 2 <= len(encoded) <= 8:
                    raise NativeUnavailable
                session.certificates = [_decode(item, 8192) for item in encoded]
                if sum(map(len, session.certificates)) > 32768:
                    raise NativeUnavailable
            except BaseException:
                session.expires = self._clock()
                raise
            return pb.AndroidKeyAttestation(
                context=pb.AndroidAttestationContext(id=identifier),
                certificate_chain=session.certificates,
            )

        return await self._run(context, create)

    async def Sign(
        self, request: pb.SignAndroidRegistrationRequest, context: grpc.aio.ServicerContext
    ) -> pb.AndroidRegistrationSignature:
        await self._validate_reference(context, request.context)
        if not 1 <= len(request.message) <= 65536:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "registration_message_invalid")

        async def sign() -> pb.AndroidRegistrationSignature:
            await self._session(context, request.context)
            result = await self._signer.call("sign", request.context.id, request.message)
            signature = _decode(result.get("signature"), 80)
            if len(signature) < 8:
                raise NativeUnavailable
            return pb.AndroidRegistrationSignature(context=request.context, signature=signature)

        return await self._run(context, sign)

    async def RequestIntegrity(
        self, request: pb.AndroidAttestationContext, context: grpc.aio.ServicerContext
    ) -> pb.AndroidIntegrityResult:
        await self._validate_reference(context, request)

        async def integrity() -> pb.AndroidIntegrityResult:
            session = await self._session(context, request)
            result = await self._signer.call("integrity", request.id, session.public_key)
            if "token" in result and "errorCode" not in result:
                token = result["token"]
                if not isinstance(token, str) or not 1 <= len(token) <= 16384:
                    raise NativeUnavailable
                return pb.AndroidIntegrityResult(context=request, token=token)
            if "token" in result:
                raise NativeUnavailable
            code = result.get("errorCode")
            if type(code) is not int or code == 0 or not -(2**31) <= code < 2**31:
                raise NativeUnavailable
            return pb.AndroidIntegrityResult(context=request, error_code=code)

        return await self._run(context, integrity, timeout=75)

    async def ReleaseContext(
        self, request: pb.AndroidAttestationContext, context: grpc.aio.ServicerContext
    ) -> Empty:
        await self._validate_reference(context, request)

        async def release() -> Empty:
            if request.id in self._sessions:
                await self._signer.call("release", request.id)
                self._sessions.pop(request.id)
            return Empty()

        return await self._run(context, release)

    async def _session(
        self, context: grpc.aio.ServicerContext, reference: pb.AndroidAttestationContext
    ) -> Session:
        session = self._sessions.get(reference.id)
        if session is None or session.expires <= self._clock() or not session.certificates:
            await context.abort(
                grpc.StatusCode.NOT_FOUND, "attestation_context_unavailable"
            )
        return session

    @staticmethod
    async def _validate_reference(
        context: grpc.aio.ServicerContext, reference: pb.AndroidAttestationContext
    ) -> None:
        if re.fullmatch(r"[0-9a-f]{32}", reference.id) is None:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "attestation_context_invalid")

    async def _run(
        self, context: grpc.aio.ServicerContext, operation: Callable[[], Awaitable[T]], timeout=30
    ) -> T:
        if self._busy.locked():
            await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "device_busy")
        async with self._busy:
            await context.send_initial_metadata(())
            remaining = context.time_remaining()
            limit = min(timeout, remaining) if remaining is not None else timeout
            try:
                async with asyncio.timeout(max(0, limit)):
                    return await operation()
            except grpc.aio.AbortError:
                raise
            except ContextNotReady:
                await context.abort(
                    grpc.StatusCode.NOT_FOUND, "attestation_context_missing"
                )
            except (NativeUnavailable, OSError):
                await context.abort(grpc.StatusCode.UNAVAILABLE, "device_sdk_unavailable")
            except TimeoutError:
                await context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "device_sdk_deadline")
            except Exception as error:
                logger.error("android_attestation_internal error_type=%s", type(error).__name__)
                await context.abort(grpc.StatusCode.INTERNAL, "android_attestation_internal")

    async def _reap(self) -> None:
        for identifier, session in list(self._sessions.items()):
            if session.expires > self._clock():
                continue
            try:
                await self._signer.call("release", identifier)
            except (ContextNotReady, NativeUnavailable, OSError, TimeoutError):
                continue
            self._sessions.pop(identifier)

    async def reap(self, stopped: asyncio.Event) -> None:
        while not stopped.is_set():
            try:
                await asyncio.wait_for(stopped.wait(), timeout=30)
            except TimeoutError:
                if not self._busy.locked():
                    async with self._busy:
                        await self._reap()

    async def close(self) -> None:
        async with self._busy:
            for session in self._sessions.values():
                session.expires = self._clock()
            await self._reap()


def _decode(encoded: object, maximum: int) -> bytes:
    if not isinstance(encoded, str) or len(encoded) > 4 * ((maximum + 2) // 3):
        raise NativeUnavailable
    try:
        decoded = base64.b64decode(encoded, validate=True)
    except ValueError:
        raise NativeUnavailable from None
    if not 1 <= len(decoded) <= maximum:
        raise NativeUnavailable
    return decoded


def create_server(
    signer: Signer, clock: Callable[[], float] = time.monotonic
) -> tuple[grpc.aio.Server, AndroidAttestationService]:
    server = grpc.aio.server(
        options=[
            ("grpc.max_receive_message_length", 131072),
            ("grpc.max_send_message_length", 65536),
        ]
    )
    service = AndroidAttestationService(signer, clock)
    rpc.add_AndroidAttestationServiceServicer_to_server(service, server)
    return server, service
