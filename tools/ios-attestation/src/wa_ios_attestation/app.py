"""Private gRPC boundary for device-backed, short-lived App Attest keys."""

from __future__ import annotations

import asyncio
import logging
import secrets
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Protocol, TypeVar

import grpc
from byte.v.forge.waapp.v1 import app_attest_pb2 as pb
from byte.v.forge.waapp.v1 import app_attest_pb2_grpc as rpc
from byte.v.forge.waapp.v1 import recaptcha_pb2_grpc as recaptcha_rpc
from google.protobuf.empty_pb2 import Empty

from wa_ios_attestation.attestation import verify_assertion
from wa_ios_attestation.binding import (
    ContextNotReady,
    DeviceBusy,
    NativeBinding,
    NativeUnavailable,
)
from wa_ios_attestation.recaptcha import RecaptchaService

T = TypeVar("T")
logger = logging.getLogger(__name__)


class Signer(Protocol):
    async def create(self) -> NativeBinding: ...

    async def attest(self, binding: NativeBinding, digest: bytes) -> bytes: ...

    async def sign(self, binding: NativeBinding, digest: bytes) -> bytes: ...

    async def login_token(self, site_key: str) -> str: ...


@dataclass
class Session:
    binding: NativeBinding = field(repr=False)
    expires: float
    digest: bytes = field(default=b"", repr=False)
    key_attestation: bytes = field(default=b"", repr=False)
    counter: int = 0


class AppAttestService(rpc.AppAttestServiceServicer):
    def __init__(self, signer: Signer, clock: Callable[[], float] = time.monotonic) -> None:
        self._signer = signer
        self._busy = asyncio.Lock()
        self._sessions: dict[str, Session] = {}
        self._clock = clock

    async def CreateContext(
        self, _request: Empty, context: grpc.aio.ServicerContext
    ) -> pb.AppAttestContext:
        async def create() -> pb.AppAttestContext:
            if len(self._sessions) >= 32:
                await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "attest_context_capacity")
            binding = await self._signer.create()
            identifier = secrets.token_urlsafe(32)
            self._sessions[identifier] = Session(binding, self._clock() + 30 * 60)
            return pb.AppAttestContext(id=identifier)

        return await self._run(context, create)

    async def AttestKey(
        self, request: pb.AttestAppAttestKeyRequest, context: grpc.aio.ServicerContext
    ) -> pb.AttestAppAttestKeyResponse:
        await self._validate_input(context, request.context, request.client_data_hash)

        async def attest() -> pb.AttestAppAttestKeyResponse:
            session = await self._session(request.context, context)
            if session.digest and session.digest != request.client_data_hash:
                await context.abort(grpc.StatusCode.FAILED_PRECONDITION, "attest_key_already_bound")
            if not session.key_attestation:
                session.digest = request.client_data_hash
                proof = await self._signer.attest(session.binding, request.client_data_hash)
                if not 1 <= len(proof) <= 16 * 1024:
                    raise NativeUnavailable
                session.key_attestation = proof
            return pb.AttestAppAttestKeyResponse(
                context=request.context,
                key_identifier=session.binding.key_identifier,
                key_attestation=session.key_attestation,
            )

        return await self._run(context, attest, request.context.id, timeout=75)

    async def GenerateAssertion(
        self, request: pb.GenerateAppAttestAssertionRequest, context: grpc.aio.ServicerContext
    ) -> pb.GenerateAppAttestAssertionResponse:
        await self._validate_input(context, request.context, request.client_data_hash)

        async def sign() -> pb.GenerateAppAttestAssertionResponse:
            session = await self._session(request.context, context)
            if not session.key_attestation:
                await context.abort(grpc.StatusCode.FAILED_PRECONDITION, "attest_key_not_attested")
            proof = await self._signer.sign(session.binding, request.client_data_hash)
            if not 1 <= len(proof) <= 4096:
                raise NativeUnavailable
            session.counter = verify_assertion(
                proof, session.key_attestation, request.client_data_hash, session.counter
            )
            return pb.GenerateAppAttestAssertionResponse(context=request.context, assertion=proof)

        return await self._run(context, sign, request.context.id)

    async def ReleaseContext(
        self, request: pb.AppAttestContext, context: grpc.aio.ServicerContext
    ) -> Empty:
        if not request.id:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "attest_context_required")

        async def release() -> Empty:
            self._sessions.pop(request.id, None)
            return Empty()

        return await self._run(context, release)

    async def _session(
        self, reference: pb.AppAttestContext, context: grpc.aio.ServicerContext
    ) -> Session:
        session = self._sessions.get(reference.id)
        if session is None:
            await context.abort(grpc.StatusCode.NOT_FOUND, "attest_context_unavailable")
        return session

    @staticmethod
    async def _validate_input(
        context: grpc.aio.ServicerContext, reference: pb.AppAttestContext, digest: bytes
    ) -> None:
        if len(digest) != 32 or not reference.id or len(reference.id) > 128:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "attestation_input_invalid")

    async def _run(
        self,
        context: grpc.aio.ServicerContext,
        operation: Callable[[], Awaitable[T]],
        context_id: str = "",
        timeout: int = 30,
    ) -> T:
        if self._busy.locked():
            await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "signer_busy")
        async with self._busy:
            now = self._clock()
            self._sessions = {
                key: value for key, value in self._sessions.items() if value.expires > now
            }
            # Commit response headers before native work to prevent transparent replay.
            await context.send_initial_metadata(())
            try:
                remaining = context.time_remaining()
                if remaining is not None and remaining <= 0:
                    await context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "native_deadline")
                async with asyncio.timeout(
                    min(timeout, remaining if remaining is not None else timeout)
                ):
                    return await operation()
            except grpc.aio.AbortError:
                raise
            except DeviceBusy:
                await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "device_busy")
            except ContextNotReady:
                self._sessions.pop(context_id, None)
                await context.abort(grpc.StatusCode.NOT_FOUND, "attest_context_not_ready")
            except (NativeUnavailable, OSError):
                await context.abort(grpc.StatusCode.UNAVAILABLE, "device_signer_unavailable")
            except TimeoutError:
                await context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "native_deadline")
            except Exception as error:
                logger.error("signer_internal error_type=%s", type(error).__name__)
                await context.abort(grpc.StatusCode.INTERNAL, "signer_internal")


def create_server(signer: Signer, clock: Callable[[], float] = time.monotonic) -> grpc.aio.Server:
    server = grpc.aio.server(
        options=[("grpc.max_receive_message_length", 4096), ("grpc.max_send_message_length", 32768)]
    )
    service = AppAttestService(signer, clock)
    rpc.add_AppAttestServiceServicer_to_server(service, server)
    recaptcha_rpc.add_RecaptchaServiceServicer_to_server(
        RecaptchaService(signer, service._run), server
    )
    return server
