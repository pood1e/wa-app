import base64

import cbor2
import pytest
from proofs import SyntheticKey

from wa_ios_attestation.attestation import key_attestation, verify_assertion
from wa_ios_attestation.binding import ContextNotReady, NativeUnavailable


def test_key_attestation_preserves_native_bytes_and_checks_nonce():
    key = SyntheticKey()
    proof = key.attest(b"x" * 32)
    assert key_attestation(key.identifier, base64.b64encode(proof).decode(), b"x" * 32) == proof
    with pytest.raises(ContextNotReady):
        key_attestation(key.identifier, base64.b64encode(proof).decode(), b"y" * 32)
    with pytest.raises(ContextNotReady):
        key_attestation(SyntheticKey().identifier, base64.b64encode(proof).decode(), b"x" * 32)


@pytest.mark.parametrize(
    "change", ["format", "counter", "credential", "flags", "certificates", "trailing", "duplicate"]
)
def test_malformed_or_mismatched_attestation_is_redacted(change):
    key = SyntheticKey()
    value = cbor2.loads(key.attest(b"x" * 32))
    if change == "format":
        value["fmt"] = "other"
    elif change == "counter":
        value["authData"] = value["authData"][:36] + b"\x01" + value["authData"][37:]
    elif change == "credential":
        value["authData"] = value["authData"][:55] + b"y" * 32 + value["authData"][87:]
    elif change == "flags":
        value["authData"] = value["authData"][:32] + b"\x00" + value["authData"][33:]
    elif change == "certificates":
        value["attStmt"]["x5c"] = []
    raw = cbor2.dumps(value)
    if change == "trailing":
        raw += b"trailing"
    elif change == "duplicate":
        raw = b"\xa2\x63fmt\x61a\x63fmt\x61b"
    with pytest.raises(ContextNotReady) as error:
        key_attestation(key.identifier, base64.b64encode(raw).decode(), b"x" * 32)
    assert str(error.value) == ""


def test_assertion_rejects_other_digest_key_and_replayed_counter():
    key = SyntheticKey()
    proof = key.attest(b"p" * 32)
    assertion = key.sign(b"x" * 32)
    assert verify_assertion(assertion, proof, b"x" * 32, 0) == 1
    for other_proof, digest, counter in [
        (proof, b"y" * 32, 0),
        (proof, b"x" * 32, 1),
        (SyntheticKey().attest(b"p" * 32), b"x" * 32, 0),
    ]:
        with pytest.raises(NativeUnavailable):
            verify_assertion(assertion, other_proof, digest, counter)
