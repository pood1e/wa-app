import asyncio
import base64
from contextlib import asynccontextmanager

import grpc
import pytest
from byte.v.forge.waapp.v1 import android_attestation_pb2 as pb
from byte.v.forge.waapp.v1 import android_attestation_pb2_grpc as rpc

from wa_android_attestation.app import create_server


class Signer:
    def __init__(self):
        self.keys = {}
        self.calls = []
        self.outcome = {"token": "synthetic-sdk-token"}
        self.started = asyncio.Event()
        self.finish = asyncio.Event()
        self.cleaned = asyncio.Event()
        self.finish.set()

    async def call(self, operation, identifier, payload=b""):
        self.calls.append((operation, identifier, payload))
        if operation == "create":
            self.keys[identifier] = payload
            return {"certificates": [base64.b64encode(b"synthetic-der").decode()] * 2}
        if operation == "release":
            self.keys.pop(identifier, None)
            return {}
        if operation == "integrity":
            assert payload == self.keys[identifier]
            return self.outcome
        self.started.set()
        try:
            await self.finish.wait()
            return {"signature": base64.b64encode(b"synthetic-signature").decode()}
        finally:
            self.cleaned.set()


@asynccontextmanager
async def connected(signer, clock=lambda: 0):
    server, application = create_server(signer, clock)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        async with grpc.aio.insecure_channel(
            f"127.0.0.1:{port}", options=[("grpc.enable_retries", 0)]
        ) as channel:
            yield rpc.AndroidAttestationServiceStub(channel), application
    finally:
        await server.stop(0)
        await application.close()


async def create(client, public_key=b"p" * 32):
    return await client.CreateContext(
        pb.CreateAndroidAttestationContextRequest(registration_public_key=public_key)
    )


def test_contexts_bind_sdk_to_their_public_key_and_release_keys():
    async def run():
        signer = Signer()
        async with connected(signer) as (client, _):
            first, second = await create(client), await create(client, b"q" * 32)
            assert first.context != second.context
            for proof in (first, second):
                result = await client.RequestIntegrity(proof.context)
                assert result.WhichOneof("outcome") == "token"
                assert result.context == proof.context
                message = b"exact-ENC-value"
                signature = await client.Sign(
                    pb.SignAndroidRegistrationRequest(context=proof.context, message=message)
                )
                assert signature.signature == b"synthetic-signature"
                assert signer.calls[-1] == ("sign", proof.context.id, message)
            await client.ReleaseContext(first.context)
            await client.ReleaseContext(first.context)
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.RequestIntegrity(first.context)
            assert error.value.code() == grpc.StatusCode.NOT_FOUND
            assert len(signer.keys) == 1
        assert not signer.keys

    asyncio.run(run())


@pytest.mark.parametrize(
    ("outcome", "expected"),
    [
        ({"errorCode": -2}, "error_code"),
        ({"errorCode": 1004}, "error_code"),
        ({"errorCode": 0}, None),
        ({"errorCode": True}, None),
        ({"token": ""}, None),
        ({"token": "value", "errorCode": -2}, None),
        ({}, None),
    ],
)
def test_integrity_uses_only_an_actual_valid_sdk_outcome(outcome, expected):
    async def run():
        signer = Signer()
        signer.outcome = outcome
        async with connected(signer) as (client, _):
            proof = await create(client)
            if expected:
                result = await client.RequestIntegrity(proof.context)
                assert result.WhichOneof("outcome") == expected
                assert result.error_code == outcome["errorCode"]
            else:
                with pytest.raises(grpc.aio.AioRpcError) as error:
                    await client.RequestIntegrity(proof.context)
                assert error.value.code() == grpc.StatusCode.UNAVAILABLE

    asyncio.run(run())


def test_expired_context_cannot_sign_and_reaping_deletes_its_key():
    async def run():
        now = 0
        signer = Signer()
        async with connected(signer, lambda: now) as (client, application):
            proof = await create(client)
            now = 1800
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.Sign(
                    pb.SignAndroidRegistrationRequest(context=proof.context, message=b"ENC")
                )
            assert error.value.code() == grpc.StatusCode.NOT_FOUND
            await application._reap()
            assert not signer.keys

    asyncio.run(run())


def test_busy_device_rejects_parallel_work_and_cancellation_cleans_up():
    async def run():
        signer = Signer()
        async with connected(signer) as (client, _):
            proof = await create(client)
            signer.finish.clear()
            request = pb.SignAndroidRegistrationRequest(context=proof.context, message=b"ENC")
            pending = client.Sign(request)
            await asyncio.wait_for(signer.started.wait(), 2)
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.RequestIntegrity(proof.context)
            assert error.value.code() == grpc.StatusCode.RESOURCE_EXHAUSTED
            pending.cancel()
            with pytest.raises(asyncio.CancelledError):
                await pending
            await asyncio.wait_for(signer.cleaned.wait(), 2)
            signer.finish.set()
            result = await client.Sign(request)
            assert result.context == proof.context

    asyncio.run(run())
