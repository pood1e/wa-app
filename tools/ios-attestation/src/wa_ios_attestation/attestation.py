"""Validate native proof structure, key identity and challenge binding."""

import base64
import hashlib
import hmac
import io

import cbor2
from cryptography import x509
from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec

from wa_ios_attestation.binding import ContextNotReady, NativeUnavailable


def decode_cbor(raw: bytes) -> dict:
    stream = io.BytesIO(raw)
    value = cbor2.CBORDecoder(
        stream, max_depth=5, allow_indefinite=False, allow_duplicate_keys=False
    ).decode()
    if stream.tell() != len(raw) or not isinstance(value, dict):
        raise ValueError
    return value


def key_attestation(identifier: str, encoded: str, digest: bytes) -> bytes:
    """Check the SDK result; Apple chain trust and acceptance belong to the server."""
    try:
        key_id = base64.b64decode(identifier, validate=True)
        if len(key_id) != 32 or base64.b64encode(key_id).decode() != identifier:
            raise ValueError
        if len(digest) != 32 or len(encoded) > 24 * 1024:
            raise ValueError
        proof = base64.b64decode(encoded, validate=True)
        if not 1 <= len(proof) <= 16 * 1024:
            raise ValueError
        value = decode_cbor(proof)
        if set(value) != {"fmt", "attStmt", "authData"} or value["fmt"] != "apple-appattest":
            raise ValueError
        statement, auth = value["attStmt"], value["authData"]
        if (
            not isinstance(statement, dict)
            or set(statement) != {"x5c", "receipt"}
            or not isinstance(statement["x5c"], list)
            or len(statement["x5c"]) != 2
            or not all(isinstance(cert, bytes) and cert for cert in statement["x5c"])
            or not isinstance(statement["receipt"], bytes)
            or not statement["receipt"]
            or not isinstance(auth, bytes)
            or len(auth) < 87
            or not auth[32] & 0x40
            or auth[33:37] != bytes(4)
            or auth[37:53] != b"appattest" + bytes(7)
            or int.from_bytes(auth[53:55], "big") != 32
            or not hmac.compare_digest(auth[55:87], key_id)
        ):
            raise ValueError
        cert = x509.load_der_x509_certificate(statement["x5c"][0])
        public = cert.public_key()
        if not isinstance(public, ec.EllipticCurvePublicKey) or public.curve.name != "secp256r1":
            raise ValueError
        point = public.public_bytes(
            serialization.Encoding.X962, serialization.PublicFormat.UncompressedPoint
        )
        nonce = cert.extensions.get_extension_for_oid(
            x509.ObjectIdentifier("1.2.840.113635.100.8.2")
        )
        expected = bytes.fromhex("3024a1220420") + hashlib.sha256(auth + digest).digest()
        if (
            not hmac.compare_digest(hashlib.sha256(point).digest(), key_id)
            or not isinstance(nonce.value, x509.UnrecognizedExtension)
            or not hmac.compare_digest(nonce.value.value, expected)
        ):
            raise ValueError
        return proof
    except (ValueError, cbor2.CBORDecodeError, x509.ExtensionNotFound):
        raise ContextNotReady from None


def verify_assertion(proof: bytes, key_proof: bytes, digest: bytes, previous_counter: int) -> int:
    try:
        assertion, attestation = decode_cbor(proof), decode_cbor(key_proof)
        auth = assertion["authenticatorData"]
        counter = int.from_bytes(auth[33:37], "big")
        if auth[:32] != attestation["authData"][:32] or counter <= previous_counter:
            raise ValueError
        cert = x509.load_der_x509_certificate(attestation["attStmt"]["x5c"][0])
        public = cert.public_key()
        if not isinstance(public, ec.EllipticCurvePublicKey):
            raise ValueError
        nonce = hashlib.sha256(auth + digest).digest()
        public.verify(assertion["signature"], nonce, ec.ECDSA(hashes.SHA256()))
        return counter
    except (ValueError, KeyError, cbor2.CBORDecodeError, InvalidSignature):
        raise NativeUnavailable from None
