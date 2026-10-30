import asyncio

import grpc
import pytest
from byte.v.forge.waapp.v1 import recaptcha_pb2 as pb
from byte.v.forge.waapp.v1 import recaptcha_pb2_grpc as rpc

from wa_ios_attestation.app import create_server
from wa_ios_attestation.binding import NativeUnavailable


class Provider:
    def __init__(self, token="synthetic-token", error=None):
        self.token = token
        self.error = error
        self.calls = []

    async def login_token(self, site_key):
        self.calls.append(site_key)
        if self.error:
            raise self.error
        return self.token


@pytest.mark.parametrize(
    ("key", "token", "error", "code"),
    [
        ("synthetic-key-1234567890", "synthetic-token", None, None),
        ("", "synthetic-token", None, grpc.StatusCode.INVALID_ARGUMENT),
        ("synthetic-key-1234567890", "", None, grpc.StatusCode.UNAVAILABLE),
        ("synthetic-key-1234567890", "x" * 16385, None, grpc.StatusCode.UNAVAILABLE),
        ("synthetic-key-1234567890", "synthetic-token", NativeUnavailable(), grpc.StatusCode.UNAVAILABLE),
    ],
)
def test_recaptcha_rpc_validates_proof_and_preserves_host_configuration(key, token, error, code):
    async def run():
        provider = Provider(token, error)
        server = create_server(provider)
        port = server.add_insecure_port("127.0.0.1:0")
        await server.start()
        try:
            async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
                client = rpc.RecaptchaServiceStub(channel)
                request = pb.RecaptchaLoginTokenRequest(site_key=key)
                if code:
                    with pytest.raises(grpc.aio.AioRpcError) as rejected:
                        await client.GenerateLoginToken(request, timeout=5)
                    assert rejected.value.code() == code
                else:
                    assert (await client.GenerateLoginToken(request, timeout=5)).token == token
            assert provider.calls == ([key] if key else [])
        finally:
            await server.stop(0)

    asyncio.run(run())
