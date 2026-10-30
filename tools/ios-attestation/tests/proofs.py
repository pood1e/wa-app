"""Synthetic certificates and proofs, generated entirely in test memory."""

import base64
import hashlib
from datetime import UTC, datetime, timedelta

import cbor2
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec

from wa_ios_attestation.binding import NativeBinding


class SyntheticKey:
    def __init__(self):
        self.private = ec.generate_private_key(ec.SECP256R1())
        point = self.private.public_key().public_bytes(
            serialization.Encoding.X962, serialization.PublicFormat.UncompressedPoint
        )
        self.key_id = hashlib.sha256(point).digest()
        self.identifier = base64.b64encode(self.key_id).decode()
        self.rp_hash = hashlib.sha256(b"synthetic.app").digest()
        self.counter = 0

    def binding(self):
        return NativeBinding(self.identifier, "synthetic.app")

    def attest(self, digest):
        auth = self.rp_hash + b"\x40" + bytes(4) + b"appattest" + bytes(7)
        auth += b"\x00\x20" + self.key_id + b"synthetic-cose"
        name = x509.Name([x509.NameAttribute(x509.NameOID.COMMON_NAME, "synthetic")])
        now = datetime.now(UTC)
        nonce = bytes.fromhex("3024a1220420") + hashlib.sha256(auth + digest).digest()
        cert = (
            x509.CertificateBuilder()
            .subject_name(name)
            .issuer_name(name)
            .public_key(self.private.public_key())
            .serial_number(1)
            .not_valid_before(now - timedelta(minutes=1))
            .not_valid_after(now + timedelta(hours=1))
            .add_extension(
                x509.UnrecognizedExtension(x509.ObjectIdentifier("1.2.840.113635.100.8.2"), nonce),
                critical=False,
            )
            .sign(self.private, hashes.SHA256())
        )
        return cbor2.dumps(
            {
                "fmt": "apple-appattest",
                "attStmt": {
                    "x5c": [cert.public_bytes(serialization.Encoding.DER), b"synthetic-ca"],
                    "receipt": b"synthetic-receipt",
                },
                "authData": auth,
            }
        )

    def sign(self, digest):
        self.counter += 1
        auth = self.rp_hash + b"\x00" + self.counter.to_bytes(4, "big")
        nonce = hashlib.sha256(auth + digest).digest()
        signature = self.private.sign(nonce, ec.ECDSA(hashes.SHA256()))
        return cbor2.dumps({"signature": signature, "authenticatorData": auth})
