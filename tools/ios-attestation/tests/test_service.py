import asyncio
from contextlib import asynccontextmanager

import grpc
import pytest
from byte.v.forge.waapp.v1 import app_attest_pb2 as pb
from byte.v.forge.waapp.v1 import app_attest_pb2_grpc as rpc
from google.protobuf.empty_pb2 import Empty
from proofs import SyntheticKey

from wa_ios_attestation.app import create_server
from wa_ios_attestation.binding import ContextNotReady, DeviceBusy, NativeUnavailable


class Signer:
    def __init__(self):
        self.keys = {}
        self.attest_calls = 0
        self.started = asyncio.Event()
        self.released = asyncio.Event()
        self.cleaned = asyncio.Event()
        self.released.set()
        self.error = self.attest_error = self.digest = None

    async def create(self):
        key = SyntheticKey()
        self.keys[key.identifier] = key
        return key.binding()

    async def attest(self, binding, digest):
        self.attest_calls += 1
        if self.attest_error:
            raise self.attest_error
        return self.keys[binding.key_identifier].attest(digest)

    async def sign(self, binding, digest):
        self.digest = digest
        self.started.set()
        try:
            await self.released.wait()
            if self.error:
                raise self.error
            return self.keys[binding.key_identifier].sign(digest)
        finally:
            self.cleaned.set()


@asynccontextmanager
async def connected(signer, **options):
    server = create_server(signer, **options)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        async with grpc.aio.insecure_channel(
            f"127.0.0.1:{port}", options=[("grpc.enable_retries", 0)]
        ) as channel:
            yield rpc.AppAttestServiceStub(channel)
    finally:
        await server.stop(0)


def request(context, digest=b"x" * 32):
    return pb.GenerateAppAttestAssertionRequest(context=context, client_data_hash=digest)


async def ready(client):
    context = await client.CreateContext(Empty())
    proof = await client.AttestKey(
        pb.AttestAppAttestKeyRequest(context=context, client_data_hash=b"p" * 32)
    )
    assert proof.context == context and proof.key_attestation and proof.key_identifier
    return context


def test_contexts_keep_separate_keys_and_release_is_idempotent():
    async def run():
        signer = Signer()
        async with connected(signer) as client:
            first, second = await ready(client), await ready(client)
            assert first != second and len(signer.keys) == 2
            for context in (first, second):
                response = await client.GenerateAssertion(request(context))
                assert response.context == context and response.assertion
            await client.ReleaseContext(first)
            await client.ReleaseContext(first)
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.GenerateAssertion(request(first))
            assert error.value.code() == grpc.StatusCode.NOT_FOUND
            assert (await client.GenerateAssertion(request(second))).context == second
        async with connected(signer) as restarted:
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await restarted.GenerateAssertion(request(second))
            assert error.value.code() == grpc.StatusCode.NOT_FOUND

    asyncio.run(run())


def test_attestation_retry_reuses_key_and_success_is_cached():
    async def run():
        signer = Signer()
        async with connected(signer) as client:
            context = await client.CreateContext(Empty())
            signer.attest_error = NativeUnavailable()
            attest = pb.AttestAppAttestKeyRequest(context=context, client_data_hash=b"p" * 32)
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.AttestKey(attest)
            assert error.value.code() == grpc.StatusCode.UNAVAILABLE
            with pytest.raises(grpc.aio.AioRpcError) as changed:
                await client.AttestKey(
                    pb.AttestAppAttestKeyRequest(context=context, client_data_hash=b"q" * 32)
                )
            assert changed.value.code() == grpc.StatusCode.FAILED_PRECONDITION
            assert signer.attest_calls == 1
            signer.attest_error = None
            proof = await client.AttestKey(attest)
            assert await client.AttestKey(attest) == proof
            assert signer.attest_calls == 2 and len(signer.keys) == 1
            attest.client_data_hash = b"q" * 32
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.AttestKey(attest)
            assert error.value.code() == grpc.StatusCode.FAILED_PRECONDITION
            assert (await client.GenerateAssertion(request(context))).context == context

    asyncio.run(run())


@pytest.mark.parametrize(
    ("context", "digest", "code"),
    [
        (pb.AppAttestContext(), b"x" * 32, grpc.StatusCode.INVALID_ARGUMENT),
        (pb.AppAttestContext(id="opaque"), b"x", grpc.StatusCode.INVALID_ARGUMENT),
        (pb.AppAttestContext(id="其他上下文"), b"x" * 32, grpc.StatusCode.NOT_FOUND),
    ],
)
def test_invalid_requests_do_not_reach_device(context, digest, code):
    async def run():
        signer = Signer()
        async with connected(signer) as client:
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.GenerateAssertion(request(context, digest))
            assert error.value.code() == code and not signer.started.is_set()
            unprepared = await client.CreateContext(Empty())
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.GenerateAssertion(request(unprepared))
            assert error.value.code() == grpc.StatusCode.FAILED_PRECONDITION

    asyncio.run(run())


def test_device_busy_and_cancellation_release_ownership():
    async def run():
        signer = Signer()
        signer.released.clear()
        async with connected(signer) as client:
            context = await ready(client)
            first = client.GenerateAssertion(request(context))
            await asyncio.wait_for(signer.started.wait(), 5)
            with pytest.raises(grpc.aio.AioRpcError) as busy:
                await client.CreateContext(Empty())
            assert busy.value.code() == grpc.StatusCode.RESOURCE_EXHAUSTED
            first.cancel()
            with pytest.raises(asyncio.CancelledError):
                await first
            await asyncio.wait_for(signer.cleaned.wait(), 5)
            signer.released.set()
            assert (await client.GenerateAssertion(request(context))).context == context

    asyncio.run(run())


@pytest.mark.parametrize(
    ("exception", "code"),
    [
        (DeviceBusy("PRIVATE_FAILURE"), grpc.StatusCode.RESOURCE_EXHAUSTED),
        (ContextNotReady("PRIVATE_FAILURE"), grpc.StatusCode.NOT_FOUND),
        (NativeUnavailable("PRIVATE_FAILURE"), grpc.StatusCode.UNAVAILABLE),
        (RuntimeError("PRIVATE_FAILURE"), grpc.StatusCode.INTERNAL),
    ],
)
def test_errors_exclude_native_material(exception, code, caplog):
    async def run():
        signer = Signer()
        async with connected(signer) as client:
            first, second = await ready(client), await ready(client)
            signer.error = exception
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.GenerateAssertion(request(first))
            assert error.value.code() == code and "PRIVATE" not in error.value.details()
            signer.error = None
            assert (await client.GenerateAssertion(request(second))).context == second
            if isinstance(exception, ContextNotReady):
                with pytest.raises(grpc.aio.AioRpcError):
                    await client.GenerateAssertion(request(first))
            else:
                assert (await client.GenerateAssertion(request(first))).context == first

    asyncio.run(run())
    assert "PRIVATE" not in caplog.text


def test_context_capacity_and_expiration_are_bounded():
    async def run():
        now = [0.0]
        signer = Signer()
        async with connected(signer, clock=lambda: now[0]) as client:
            contexts = [await client.CreateContext(Empty()) for _ in range(32)]
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.CreateContext(Empty())
            assert error.value.code() == grpc.StatusCode.RESOURCE_EXHAUSTED
            assert len(signer.keys) == 32
            now[0] = 1801
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await client.GenerateAssertion(request(contexts[0]))
            assert error.value.code() == grpc.StatusCode.NOT_FOUND
            await ready(client)
            assert len(signer.keys) == 33

    asyncio.run(run())
