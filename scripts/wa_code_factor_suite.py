#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from dataclasses import dataclass, field, replace
from pathlib import Path
import random
import requests
import string
import time
from typing import Any
from urllib.parse import quote

import wa_code_param_probe as probe
from wa_code_random_device_experiment import DeviceProfile, build_device

ABPROP_URL = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/reg_onboard_abprop&"


@dataclass(frozen=True)
class FactorArm:
    group: str
    label: str
    device_label: str = "xiaomi-a11"
    transport: str = "requests"
    patches: tuple[str, ...] = ()
    sets: tuple[str, ...] = ()
    omits: tuple[str, ...] = ()
    envelope: str = "signed"
    preflight: str = ""
    stable_install: bool = False
    prefix: str = ""
    app_version: str = "2.26.23.71"


@dataclass
class SuiteArgs:
    proxy: str
    timeout: float
    show_fields: bool = False
    show_response: bool = False
    dry_run: bool = False
    variant: str = "current"
    set_param: list[str] = field(default_factory=list)
    omit: list[str] = field(default_factory=list)
    unsigned: bool = False
    empty_h: bool = False
    user_agent: str = ""
    device_display_id: str = ""
    device_ram: str = ""
    transport: str = "requests"


@dataclass(frozen=True)
class RegistrationProxyLease:
    lease_id: str
    account_id: str
    purpose: str
    proxy_url: str
    listener_id: str = ""
    exit_country: str = ""
    exit_state: str = ""
    exit_city: str = ""


class RegistrationProxyLeaseClient:
    def __init__(self, api_base: str, auth_token: str, timeout: float, egress_host: str, egress_port: int, egress_scheme: str) -> None:
        self._api_base = api_base.rstrip("/")
        self._timeout = max(timeout, 1)
        self._egress_host = egress_host.strip()
        self._egress_port = egress_port
        self._egress_scheme = egress_scheme.strip()
        self._session = requests.Session()
        self._session.trust_env = False
        if auth_token:
            self._session.headers.update({"Authorization": "Bearer " + auth_token})

    def acquire(self, account_id: str, purpose: str, ttl_seconds: int, job_key: str) -> RegistrationProxyLease:
        labels = {
            "purpose": "wa-app-probe",
            "job_id": job_key,
            "session_id": probe.short_hash(job_key),
        }
        payload = {
            "accountId": account_id,
            "purpose": purpose,
            "forceNew": True,
            "policy": {
                "stickyTtl": f"{max(30, int(ttl_seconds or 120))}s",
                "labels": labels,
            },
            "selectionPolicy": {"purpose": purpose, "maxAttempts": 1},
        }
        response = self._session.post(self._api_base + "/leases/acquire", json=payload, timeout=self._timeout)
        response.raise_for_status()
        body = response.json() or {}
        lease = dict_value(body, "lease")
        egress = dict_value(body, "egress") or dict_value(lease, "egress")
        lease_id = text_value(lease, "leaseId", "lease_id") or text_value(dict_value(egress, "labels"), "lease_id", "leaseId")
        if not lease_id:
            raise RuntimeError("registration proxy lease_id is empty")
        lease_account_id = text_value(lease, "accountId", "account_id") or account_id
        lease_purpose = text_value(lease, "purpose") or purpose
        return RegistrationProxyLease(
            lease_id=lease_id,
            account_id=lease_account_id,
            purpose=lease_purpose,
            proxy_url=self._proxy_url(egress),
            listener_id=registration_proxy_lease_listener_id(lease, body),
            exit_country=registration_proxy_lease_exit_text(("exitCountry", "exit_country", "countryCode", "country_code"), egress, lease, body).upper(),
            exit_state=registration_proxy_lease_exit_text(("exitState", "exit_state", "region", "state"), egress, lease, body).upper(),
            exit_city=registration_proxy_lease_exit_text(("exitCity", "exit_city", "city"), egress, lease, body),
        )

    def release(self, lease: RegistrationProxyLease) -> str:
        payload = {"lease_id": lease.lease_id, "account_id": lease.account_id, "purpose": lease.purpose}
        response = self._session.post(self._api_base + "/leases/release", json=payload, timeout=self._timeout)
        response.raise_for_status()
        return "released"

    def close(self) -> None:
        self._session.close()

    def _proxy_url(self, egress: dict[str, Any]) -> str:
        host = self._egress_host or text_value(egress, "host")
        port = self._egress_port or int(text_value(egress, "port") or "0")
        if not host or port <= 0:
            raise RuntimeError("registration proxy lease egress is invalid")
        protocol = text_value(egress, "protocol").upper()
        scheme = self._egress_scheme or ("socks5h" if "SOCKS5" in protocol else "http")
        labels = dict_value(egress, "labels")
        username = text_value(labels, "proxy_username", "proxyUsername")
        password = text_value(labels, "proxy_password", "proxyPassword")
        auth = ""
        if username or password:
            auth = f"{quote(username, safe='-._~')}:{quote(password, safe='-._~')}@"
        return f"{scheme}://{auth}{host}:{port}"


def dict_value(item: dict[str, Any], key: str) -> dict[str, Any]:
    value = item.get(key) if isinstance(item, dict) else None
    return value if isinstance(value, dict) else {}


def text_value(item: dict[str, Any], *keys: str) -> str:
    if not isinstance(item, dict):
        return ""
    for key in keys:
        value = item.get(key)
        if value is not None:
            return str(value).strip()
    return ""


def registration_proxy_lease_listener_id(*items: dict[str, Any]) -> str:
    queue = list(items)
    while queue:
        item = queue.pop(0)
        if not isinstance(item, dict):
            continue
        value = text_value(item, "listenerId", "listener_id")
        if value:
            return value
        for key in ("listener", "egressListener", "egress_listener"):
            nested = item.get(key)
            if isinstance(nested, dict):
                queue.append(nested)
        labels = item.get("labels")
        if isinstance(labels, dict):
            queue.append(labels)
    return ""


def registration_proxy_lease_exit_text(keys: tuple[str, ...], *items: dict[str, Any]) -> str:
    queue = list(items)
    while queue:
        item = queue.pop(0)
        if not isinstance(item, dict):
            continue
        value = text_value(item, *keys)
        if value:
            return value
        labels = item.get("labels")
        if isinstance(labels, dict):
            queue.append(labels)
    return ""


def lease_log_fields(lease: RegistrationProxyLease) -> dict[str, Any]:
    out: dict[str, Any] = {
        "lease_hash": probe.short_hash(lease.lease_id),
    }
    if lease.listener_id:
        out["listener_hash"] = probe.short_hash(lease.listener_id)
    if lease.exit_country:
        out["lease_exit_country"] = lease.exit_country
    if lease.exit_state:
        out["lease_exit_state"] = lease.exit_state
    if lease.exit_city:
        out["lease_exit_city"] = lease.exit_city
    return out


def classify(row: dict[str, Any]) -> str:
    if row.get("error"):
        return "transport_error"
    status = str(row.get("status") or "").lower()
    reason = str(row.get("reason") or "").lower()
    if status in {"sent", "ok"}:
        return "sent"
    if reason == "no_routes":
        return "no_routes"
    if reason == "blocked":
        return "blocked"
    if reason == "too_recent":
        return "too_recent"
    if reason == "bad_token":
        return "bad_token"
    if row.get("request_failed"):
        return "request_failed"
    if status == "fail":
        return "other_fail"
    return "unknown"


def random_colombia_phone_with_prefix(prefix: str) -> tuple[str, str]:
    return "57", prefix + "".join(random.choice(string.digits) for _ in range(7))


def next_phone(arm: FactorArm) -> tuple[str, str]:
    if arm.prefix:
        return random_colombia_phone_with_prefix(arm.prefix)
    return probe.random_colombia_phone()


def device_for_arm(arm: FactorArm) -> DeviceProfile:
    return build_device(arm.device_label)


def args_for_arm(base: argparse.Namespace, arm: FactorArm) -> SuiteArgs:
    device = device_for_arm(arm)
    user_agent = device.user_agent
    if arm.app_version != "2.26.23.71":
        user_agent = user_agent.replace("WhatsApp/2.26.23.71", "WhatsApp/" + arm.app_version, 1)
    return SuiteArgs(
        proxy=base.proxy,
        timeout=base.timeout,
        show_fields=base.show_fields,
        show_response=base.show_response,
        dry_run=base.dry_run,
        set_param=list(arm.sets),
        omit=list(arm.omits),
        unsigned=arm.envelope == "unsigned",
        empty_h=arm.envelope == "empty-h",
        user_agent=user_agent,
        device_display_id=device.display_id,
        device_ram=device.ram_gib,
        transport=arm.transport,
    )


def config_for_arm(arm: FactorArm, args: SuiteArgs) -> probe.ShapeConfig:
    config = probe.config_for_variant(args.variant)
    for patch in arm.patches:
        config = probe.apply_patch_name(config, patch)
    return probe.apply_cli_config_overrides(config, args)


def stable_material(base_material: probe.ProbeMaterial, fresh: probe.ProbeMaterial) -> probe.ProbeMaterial:
    return replace(
        fresh,
        fdid=base_material.fdid,
        expid=base_material.expid,
        expid_uuid=base_material.expid_uuid,
        access_session_id=base_material.access_session_id,
        access_session_id_uuid=base_material.access_session_id_uuid,
        id_raw=base_material.id_raw,
        backup_token_raw=base_material.backup_token_raw,
        authkey=base_material.authkey,
        key_bundle=base_material.key_bundle,
        advertising_id=base_material.advertising_id,
        created_at_unix=base_material.created_at_unix,
    )


def build_material(repo_root: Path, arm: FactorArm, stable_cache: dict[str, probe.ProbeMaterial]) -> probe.ProbeMaterial:
    cc, national = next_phone(arm)
    fresh = probe.new_probe_material(repo_root, cc, national)
    if not arm.stable_install:
        return fresh
    base = stable_cache.get(arm.label)
    if base is None:
        stable_cache[arm.label] = fresh
        return fresh
    return stable_material(base, fresh)


def build_abprop_params(material: probe.ProbeMaterial) -> list[probe.Param]:
    params: list[probe.Param] = []
    probe.add_param(params, "cc", material.cc)
    probe.add_param(params, "in", material.national)
    probe.add_param(params, "lg", "en")
    probe.add_param(params, "lc", "US")
    probe.add_param(params, "fdid", material.fdid)
    probe.add_param(params, "expid", material.expid)
    probe.add_param(params, "access_session_id", material.access_session_id)
    probe.add_param(params, "authkey", material.authkey)
    for key in ["e_ident", "e_keytype", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig"]:
        probe.add_param(params, key, material.key_bundle[key])
    return params


def post_abprop(material: probe.ProbeMaterial, args: SuiteArgs) -> dict[str, Any]:
    plain = probe.render_plain(build_abprop_params(material))
    envelope = probe.build_signed_wasafe_envelope(plain, material, "unsigned")
    headers = {
        "Content-Type": "application/x-www-form-urlencoded",
        "User-Agent": args.user_agent,
        "WaMsysRequest": "1",
        "X-Forwarded-Host": "v.whatsapp.net",
    }
    try:
        status, parsed = probe.post_form(args.transport, ABPROP_URL, headers, envelope.body, args.proxy, args.timeout)
        if not isinstance(parsed, dict):
            parsed = {"raw": parsed}
        return {
            "ab_http_status": status,
            "ab_status": parsed.get("status"),
            "ab_reason": parsed.get("reason") or parsed.get("failure_reason"),
            "ab_has_hash": bool(parsed.get("ab_hash")),
            "ab_has_exp_cfg": bool(parsed.get("exp_cfg")),
        }
    except Exception as exc:  # noqa: BLE001 - CLI probe must summarize failures.
        return {"ab_error": probe.sanitize_text(str(exc), args.proxy)}


def run_arm_once(
    repo_root: Path,
    base_args: argparse.Namespace,
    arm: FactorArm,
    stable_cache: dict[str, probe.ProbeMaterial],
    lease_client: RegistrationProxyLeaseClient | None = None,
) -> dict[str, Any]:
    material = build_material(repo_root, arm, stable_cache)
    args = args_for_arm(base_args, arm)
    config = config_for_arm(arm, args)
    preflight_result: dict[str, Any] = {}
    lease: RegistrationProxyLease | None = None
    release_status = ""
    try:
        if lease_client is not None and not base_args.dry_run:
            lease = lease_client.acquire(
                base_args.registration_proxy_lease_account_id,
                base_args.registration_proxy_lease_purpose,
                base_args.registration_proxy_lease_ttl,
                f"{base_args.run_id}:{arm.label}:{material.e164}:{time.time_ns()}",
            )
            args.proxy = lease.proxy_url
        if arm.preflight == "abprop":
            preflight_result = post_abprop(material, args)
        row = probe.post_code(material, config, args)
        row.update(preflight_result)
        if lease is not None:
            row.update(lease_log_fields(lease))
    except Exception as exc:  # noqa: BLE001 - CLI probe must summarize lease and network failures.
        row = {"error": probe.sanitize_text(str(exc), args.proxy), "outcome": "transport_error"}
        if lease is not None:
            row.update(lease_log_fields(lease))
    finally:
        if lease is not None and lease_client is not None:
            try:
                release_status = lease_client.release(lease)
            except Exception as exc:  # noqa: BLE001 - release failure must not hide request outcome.
                release_status = "release_error:" + probe.sanitize_text(str(exc), args.proxy)
    if release_status:
        row["lease_release"] = release_status
    row["group"] = arm.group
    row["label"] = arm.label
    row["transport"] = arm.transport
    row["device_label"] = arm.device_label
    row["app_version"] = arm.app_version
    row["outcome"] = row.get("outcome") or classify(row)
    if arm.prefix:
        row["prefix"] = arm.prefix
    if arm.stable_install:
        row["stable_install"] = True
    return row


def rate(numerator: int, denominator: int) -> float | None:
    if denominator <= 0:
        return None
    return round(numerator / denominator, 4)


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    labels = sorted({str(row.get("label") or "") for row in rows})
    summary: dict[str, Any] = {}
    for label in labels:
        group = [row for row in rows if row.get("label") == label]
        counts = {
            key: 0
            for key in [
                "sent",
                "no_routes",
                "blocked",
                "bad_token",
                "too_recent",
                "request_failed",
                "transport_error",
                "other_fail",
                "unknown",
            ]
        }
        for row in group:
            outcome = str(row.get("outcome") or "unknown")
            counts[outcome] = counts.get(outcome, 0) + 1
        total = len(group)
        target = counts["sent"] + counts["no_routes"]
        summary[label] = {
            "group": str(group[0].get("group") or "") if group else "",
            "total": total,
            **counts,
            "target_decisions": target,
            "sent_rate_on_target": rate(counts["sent"], target),
        }
    return summary


def markdown_table(summary: dict[str, Any]) -> str:
    headers = ["group", "label", "total", "sent", "no_routes", "blocked", "bad_token", "target", "sent/target"]
    lines = ["| " + " | ".join(headers) + " |", "|" + "---|" * len(headers)]
    for label, item in sorted(summary.items(), key=lambda pair: (str(pair[1].get("group")), pair[0])):
        values = [
            str(item.get("group")),
            label,
            str(item.get("total", 0)),
            str(item.get("sent", 0)),
            str(item.get("no_routes", 0)),
            str(item.get("blocked", 0)),
            str(item.get("bad_token", 0)),
            str(item.get("target_decisions", 0)),
            str(item.get("sent_rate_on_target")),
        ]
        lines.append("| " + " | ".join(values) + " |")
    return "\n".join(lines)


def factor_arms() -> list[FactorArm]:
    ghcr_patches = (
        "gpia-error-minus-two",
        "gpia-data-digest-ghcr",
        "gpia-source-ghcr",
        "gpia-json-no-slash-escape",
        "wamsys-ghcr",
    )
    wamsys_omits = ("gpia", "_ga", "_gi", "_gp", "_ge", "aid", "_gg", "_gs")
    co_locale_sets = ("lg=es", "lc=CO")
    co_operator_patch = ("operator-co-732101",)
    combo_arms = []
    for prefix in ("314", "350"):
        combo_arms.extend(
            [
                FactorArm(
                    "combo",
                    f"combo-{prefix}-current",
                    patches=co_operator_patch,
                    sets=co_locale_sets,
                    prefix=prefix,
                ),
                FactorArm(
                    "combo",
                    f"combo-{prefix}-ghcr",
                    patches=(*co_operator_patch, *ghcr_patches),
                    sets=co_locale_sets,
                    prefix=prefix,
                ),
                FactorArm(
                    "combo",
                    f"combo-{prefix}-omit-wamsys",
                    patches=co_operator_patch,
                    sets=co_locale_sets,
                    omits=wamsys_omits,
                    prefix=prefix,
                ),
            ]
        )
    routing_base = FactorArm(
        "routing",
        "routing-baseline-350",
        patches=co_operator_patch,
        sets=co_locale_sets,
        prefix="350",
    )
    routing_arms = [
        routing_base,
        replace(routing_base, label="routing-us-locale", sets=()),
        replace(routing_base, label="routing-zero-operator", patches=()),
        replace(routing_base, label="routing-operator-omit", patches=("operator-omit",)),
        replace(routing_base, label="routing-simnum-one", sets=(*co_locale_sets, "simnum=1")),
        replace(routing_base, label="routing-sim-type-zero", sets=(*co_locale_sets, "sim_type=0")),
        replace(routing_base, label="routing-no-sim-signal", patches=(*co_operator_patch, "no-sim-signal")),
        replace(routing_base, label="routing-radio-zero", sets=(*co_locale_sets, "network_radio_type=0")),
        replace(routing_base, label="routing-radio-two", sets=(*co_locale_sets, "network_radio_type=2")),
        replace(routing_base, label="routing-radio-thirteen", sets=(*co_locale_sets, "network_radio_type=13")),
        replace(routing_base, label="routing-cellular-zero", sets=(*co_locale_sets, "cellular_strength=0")),
        replace(routing_base, label="routing-cellular-three", sets=(*co_locale_sets, "cellular_strength=3")),
        replace(routing_base, label="routing-roaming-one", sets=(*co_locale_sets, "roaming_type=1")),
        replace(routing_base, label="routing-airplane-one", sets=(*co_locale_sets, "airplane_mode_type=1")),
        replace(routing_base, label="routing-feo2-omit", omits=("feo2_query_status",)),
        replace(routing_base, label="routing-feo2-security-error", sets=(*co_locale_sets, "feo2_query_status=error_security_exception")),
        replace(routing_base, label="routing-mnc-102", sets=(*co_locale_sets, "mnc=102", "sim_mnc=102")),
        replace(routing_base, label="routing-mnc-103", sets=(*co_locale_sets, "mnc=103", "sim_mnc=103")),
        replace(routing_base, label="routing-mnc-123", sets=(*co_locale_sets, "mnc=123", "sim_mnc=123")),
        replace(routing_base, label="routing-mnc-130", sets=(*co_locale_sets, "mnc=130", "sim_mnc=130")),
    ]
    candidate_arms = [
        FactorArm("candidate", "candidate-xiaomi-301-default", device_label="xiaomi-a11", prefix="301"),
        FactorArm("candidate", "candidate-xiaomi-350-default", device_label="xiaomi-a11", prefix="350"),
        FactorArm("candidate", "candidate-random-a11-301-default", device_label="random-generic-a11", prefix="301"),
        FactorArm("candidate", "candidate-random-a11-350-default", device_label="random-generic-a11", prefix="350"),
    ]
    boost_base = FactorArm("boost", "boost-baseline-random-a11-350", device_label="random-generic-a11", prefix="350")
    boost_arms = [
        boost_base,
        replace(boost_base, label="boost-omit-wamsys", omits=wamsys_omits),
        replace(boost_base, label="boost-ghcr-wamsys", patches=ghcr_patches),
        replace(boost_base, label="boost-no-sim-signal", patches=("no-sim-signal",)),
        replace(boost_base, label="boost-client-metrics-google-play", patches=("client-metrics-google-play",)),
        replace(
            boost_base,
            label="boost-client-metrics-attempts-2",
            sets=('client_metrics={"attempts":2,"app_campaign_download_source":"unknown|unknown"}',),
        ),
        replace(boost_base, label="boost-db-zero", patches=("db-zero",)),
        replace(boost_base, label="boost-hasav-zero", sets=("hasav=0",)),
        replace(boost_base, label="boost-hasinrc-zero", sets=("hasinrc=0",)),
        replace(boost_base, label="boost-abprop-then-code", preflight="abprop"),
        replace(boost_base, label="boost-transport-curl", transport="curl"),
        replace(boost_base, label="boost-transport-curl-http1", transport="curl-http1.1"),
        replace(boost_base, label="boost-random-xiaomi-like-a11", device_label="random-xiaomi-like-a11"),
        replace(boost_base, label="boost-random-oppo-like-a12", device_label="random-oppo-like-a12"),
        replace(boost_base, label="boost-consistent-generic-a11", device_label="consistent-generic-a11"),
        replace(boost_base, label="boost-ram-450", device_label="ram-a11-450"),
        replace(boost_base, label="boost-ram-650", device_label="ram-a11-650"),
        replace(boost_base, label="boost-prefix-301", prefix="301"),
    ]
    return [
        *combo_arms,
        *routing_arms,
        *candidate_arms,
        *boost_arms,
        FactorArm("transport", "transport-requests"),
        FactorArm("transport", "transport-curl", transport="curl"),
        FactorArm("transport", "transport-curl-http1", transport="curl-http1.1"),
        FactorArm("install", "install-fresh"),
        FactorArm("install", "install-stable", stable_install=True),
        FactorArm("integrity", "integrity-signed"),
        FactorArm("integrity", "integrity-unsigned", envelope="unsigned"),
        FactorArm("integrity", "integrity-empty-h", envelope="empty-h"),
        FactorArm("integrity", "integrity-omit-wamsys", omits=wamsys_omits),
        FactorArm("integrity", "integrity-ghcr-wamsys", patches=ghcr_patches),
        FactorArm("number", "prefix-300", prefix="300"),
        FactorArm("number", "prefix-301", prefix="301"),
        FactorArm("number", "prefix-310", prefix="310"),
        FactorArm("number", "prefix-314", prefix="314"),
        FactorArm("number", "prefix-350", prefix="350"),
        FactorArm("context", "context-zero"),
        FactorArm("context", "context-co-operator", patches=("operator-co-732101",)),
        FactorArm("context", "context-co-locale", patches=("operator-co-732101",), sets=("lg=es", "lc=CO")),
        FactorArm("context", "context-no-sim-signal", patches=("no-sim-signal",)),
        FactorArm("app", "app-current"),
        FactorArm("app", "app-old-2.26.21.73", app_version="2.26.21.73"),
        FactorArm("abprop", "abprop-code-only"),
        FactorArm("abprop", "abprop-then-code", preflight="abprop"),
        FactorArm("metrics", "metrics-default"),
        FactorArm("metrics", "metrics-attempts-2", sets=('client_metrics={"attempts":2,"app_campaign_download_source":"unknown|unknown"}',)),
        FactorArm("metrics", "metrics-google-play", patches=("client-metrics-google-play",)),
        FactorArm("debug", "debug-db-one"),
        FactorArm("debug", "debug-db-zero", patches=("db-zero",)),
        FactorArm("debug", "debug-hasav-zero", sets=("hasav=0",)),
        FactorArm("debug", "debug-hasinrc-zero", sets=("hasinrc=0",)),
        FactorArm("device", "device-xiaomi-a11", device_label="xiaomi-a11"),
        FactorArm("device", "device-random-generic-a11", device_label="random-generic-a11"),
        FactorArm("device", "device-oneplus-a14", device_label="oneplus-known-a14"),
    ]


def selected_arms(groups: set[str], labels: set[str]) -> list[FactorArm]:
    arms = factor_arms()
    if groups:
        arms = [arm for arm in arms if arm.group in groups]
    if labels:
        arms = [arm for arm in arms if arm.label in labels]
    return arms


def output_paths(args: argparse.Namespace) -> tuple[Path, Path]:
    run_id = args.run_id or time.strftime("%Y%m%d-%H%M%S")
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    return out_dir / f"{run_id}.ndjson", out_dir / f"{run_id}.summary.json"


def is_target_decision(row: dict[str, Any]) -> bool:
    return row.get("outcome") in {"sent", "no_routes"}


def sleep_after_request(args: argparse.Namespace) -> None:
    if not args.dry_run and args.sleep > 0:
        time.sleep(args.sleep + random.random() * max(args.jitter, 0))


def run_fixed_rounds(
    repo_root: Path,
    args: argparse.Namespace,
    arms: list[FactorArm],
    stable_cache: dict[str, probe.ProbeMaterial],
    lease_client: RegistrationProxyLeaseClient | None,
    handle: Any,
) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for round_index in range(1, args.samples + 1):
        round_arms = list(arms)
        random.shuffle(round_arms)
        for arm in round_arms:
            row = run_arm_once(repo_root, args, arm, stable_cache, lease_client)
            row["round"] = round_index
            rows.append(row)
            line = json.dumps(row, ensure_ascii=False, sort_keys=True)
            print(line, flush=True)
            handle.write(line + "\n")
            handle.flush()
            sleep_after_request(args)
    return rows


def run_until_target_decisions(
    repo_root: Path,
    args: argparse.Namespace,
    arms: list[FactorArm],
    stable_cache: dict[str, probe.ProbeMaterial],
    lease_client: RegistrationProxyLeaseClient | None,
    handle: Any,
) -> list[dict[str, Any]]:
    max_samples = args.max_samples if args.max_samples > 0 else args.samples
    sample_counts = {arm.label: 0 for arm in arms}
    decision_counts = {arm.label: 0 for arm in arms}
    rows: list[dict[str, Any]] = []
    batch_index = 0
    while True:
        active_arms = [
            arm
            for arm in arms
            if sample_counts[arm.label] < max_samples and decision_counts[arm.label] < args.target_decisions
        ]
        if not active_arms:
            return rows
        batch_index += 1
        random.shuffle(active_arms)
        for arm in active_arms:
            row = run_arm_once(repo_root, args, arm, stable_cache, lease_client)
            sample_counts[arm.label] += 1
            if is_target_decision(row):
                decision_counts[arm.label] += 1
            row["round"] = batch_index
            row["sample_index"] = sample_counts[arm.label]
            row["target_decision_index"] = decision_counts[arm.label]
            rows.append(row)
            line = json.dumps(row, ensure_ascii=False, sort_keys=True)
            print(line, flush=True)
            handle.write(line + "\n")
            handle.flush()
            sleep_after_request(args)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run one-by-one SMS /v2/code factor probes with a Xiaomi Android 11 baseline.")
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--target-decisions", type=int, default=0, help="stop each arm after this many sent/no_routes decisions")
    parser.add_argument("--max-samples", type=int, default=0, help="per-arm cap when --target-decisions is set; defaults to --samples")
    parser.add_argument("--groups", default="", help="comma-separated factor groups")
    parser.add_argument("--labels", default="", help="comma-separated exact labels")
    parser.add_argument("--proxy", default="", help="HTTP proxy URL; WA_PROBE_PROXY_URL is used when omitted")
    parser.add_argument("--lease-per-request", action="store_true", help="acquire and release a registration proxy lease for each outbound request")
    parser.add_argument("--registration-proxy-lease-api-base", default="", help="registration proxy lease API base; WA_REGISTRATION_PROXY_LEASE_API_BASE_URL is used when omitted")
    parser.add_argument("--registration-proxy-lease-auth-token", default="", help="registration proxy lease API auth token; WA_REGISTRATION_PROXY_LEASE_AUTH_TOKEN is used when omitted")
    parser.add_argument("--registration-proxy-lease-account-id", default="", help="registration proxy lease account id")
    parser.add_argument("--registration-proxy-lease-purpose", default="wa-app-probe")
    parser.add_argument("--registration-proxy-lease-ttl", type=int, default=120)
    parser.add_argument("--registration-proxy-lease-egress-host", default="", help="public data-plane host override for lease egress")
    parser.add_argument("--registration-proxy-lease-egress-port", type=int, default=0, help="public data-plane port override for lease egress")
    parser.add_argument("--registration-proxy-lease-egress-scheme", default="", help="public data-plane scheme override for lease egress")
    parser.add_argument("--timeout", type=float, default=25)
    parser.add_argument("--sleep", type=float, default=0.6)
    parser.add_argument("--jitter", type=float, default=0.3)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--show-fields", action="store_true")
    parser.add_argument("--show-response", action="store_true")
    parser.add_argument("--out-dir", default=".temp/wa-code-param-experiments")
    parser.add_argument("--run-id", default="")
    args = parser.parse_args()

    args.registration_proxy_lease_api_base = (args.registration_proxy_lease_api_base or os.environ.get("WA_REGISTRATION_PROXY_LEASE_API_BASE_URL", "")).strip()
    args.registration_proxy_lease_auth_token = (args.registration_proxy_lease_auth_token or os.environ.get("WA_REGISTRATION_PROXY_LEASE_AUTH_TOKEN", "")).strip()
    args.registration_proxy_lease_account_id = (args.registration_proxy_lease_account_id or os.environ.get("WA_REGISTRATION_PROXY_LEASE_ACCOUNT_ID", "")).strip()
    args.registration_proxy_lease_egress_host = (args.registration_proxy_lease_egress_host or os.environ.get("WA_REGISTRATION_PROXY_LEASE_EGRESS_HOST", "")).strip()
    args.registration_proxy_lease_egress_scheme = (args.registration_proxy_lease_egress_scheme or os.environ.get("WA_REGISTRATION_PROXY_LEASE_EGRESS_SCHEME", "")).strip()
    if args.registration_proxy_lease_egress_port <= 0:
        args.registration_proxy_lease_egress_port = int(os.environ.get("WA_REGISTRATION_PROXY_LEASE_EGRESS_PORT", "0") or "0")
    args.proxy = probe.normalize_proxy(args.proxy or os.environ.get("WA_PROBE_PROXY_URL", ""))
    if args.lease_per_request:
        missing = [
            name
            for name, value in {
                "WA_REGISTRATION_PROXY_LEASE_API_BASE_URL": args.registration_proxy_lease_api_base,
                "WA_REGISTRATION_PROXY_LEASE_AUTH_TOKEN": args.registration_proxy_lease_auth_token,
                "WA_REGISTRATION_PROXY_LEASE_ACCOUNT_ID": args.registration_proxy_lease_account_id,
            }.items()
            if not value
        ]
        if missing and not args.dry_run:
            print(json.dumps({"error": "missing " + ",".join(missing)}, ensure_ascii=False))
            return 2
        args.proxy = ""
    if not args.proxy and not args.dry_run and not args.lease_per_request:
        print(json.dumps({"error": "set WA_PROBE_PROXY_URL or pass --proxy"}, ensure_ascii=False))
        return 2
    groups = {item.strip() for item in args.groups.split(",") if item.strip()}
    labels = {item.strip() for item in args.labels.split(",") if item.strip()}
    arms = selected_arms(groups, labels)
    if not arms:
        print(json.dumps({"error": "no factor arms selected"}, ensure_ascii=False))
        return 2

    repo_root = Path(__file__).resolve().parents[1]
    stable_cache: dict[str, probe.ProbeMaterial] = {}
    lease_client = (
        RegistrationProxyLeaseClient(
            args.registration_proxy_lease_api_base,
            args.registration_proxy_lease_auth_token,
            min(args.timeout, 10),
            args.registration_proxy_lease_egress_host,
            args.registration_proxy_lease_egress_port,
            args.registration_proxy_lease_egress_scheme,
        )
        if args.lease_per_request and not args.dry_run
        else None
    )
    ndjson_path, summary_path = output_paths(args)
    try:
        with ndjson_path.open("w", encoding="utf-8") as handle:
            if args.target_decisions > 0:
                rows = run_until_target_decisions(repo_root, args, arms, stable_cache, lease_client, handle)
            else:
                rows = run_fixed_rounds(repo_root, args, arms, stable_cache, lease_client, handle)
    finally:
        if lease_client is not None:
            lease_client.close()
    summary = summarize(rows)
    payload = {
        "samples_per_arm": args.samples,
        "target_decisions": args.target_decisions,
        "max_samples": args.max_samples,
        "groups": sorted(groups) if groups else "all",
        "labels": [arm.label for arm in arms],
        "summary": summary,
    }
    summary_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"result_file": str(ndjson_path), "summary_file": str(summary_path), "summary": summary}, ensure_ascii=False, sort_keys=True))
    print(markdown_table(summary))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
