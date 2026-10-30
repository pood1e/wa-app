"""Device failures translated at the private RPC boundary."""


class NativeUnavailable(Exception):
    pass


class ContextNotReady(Exception):
    pass
