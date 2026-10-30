"""Device SDK proof generation independent of registration requests."""

import re
from collections.abc import Awaitable, Callable
from typing import Protocol

import grpc
from byte.v.forge.waapp.v1 import recaptcha_pb2 as pb
from byte.v.forge.waapp.v1 import recaptcha_pb2_grpc as rpc

from wa_ios_attestation.binding import NativeUnavailable


class Provider(Protocol):
    async def login_token(self, site_key: str) -> str: ...


class RecaptchaService(rpc.RecaptchaServiceServicer):
    def __init__(self, provider: Provider, run: Callable[..., Awaitable[object]]) -> None:
        self._provider = provider
        self._run = run

    async def GenerateLoginToken(
        self, request: pb.RecaptchaLoginTokenRequest, context: grpc.aio.ServicerContext
    ) -> pb.RecaptchaLoginToken:
        if re.fullmatch(r"[A-Za-z0-9_-]{20,256}", request.site_key) is None:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "recaptcha_site_key_invalid")

        async def generate() -> pb.RecaptchaLoginToken:
            token = await self._provider.login_token(request.site_key)
            if not isinstance(token, str) or not 1 <= len(token) <= 16384 or not token.isascii():
                raise NativeUnavailable
            return pb.RecaptchaLoginToken(token=token)

        return await self._run(context, generate, timeout=75)
