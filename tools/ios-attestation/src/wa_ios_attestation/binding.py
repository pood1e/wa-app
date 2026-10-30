"""Private key identity shared by the RPC boundary and native provider."""

from dataclasses import dataclass, field


class ContextNotReady(Exception):
    pass


class NativeUnavailable(Exception):
    pass


class DeviceBusy(Exception):
    pass


@dataclass(frozen=True)
class NativeBinding:
    key_identifier: str = field(repr=False)
    bundle_identifier: str
