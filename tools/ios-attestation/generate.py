"""Generate the worker contract from the repository's single Proto source."""

from pathlib import Path

import grpc_tools
from grpc_tools import protoc

root = Path(__file__).resolve().parents[2]
output = Path(__file__).resolve().parent / "generated"
output.mkdir(exist_ok=True)
result = protoc.main(
    [
        "grpc_tools.protoc",
        f"-I{root / 'proto'}",
        f"-I{Path(grpc_tools.__file__).parent / '_proto'}",
        f"--python_out={output}",
        f"--pyi_out={output}",
        f"--grpc_python_out={output}",
        "byte/v/forge/waapp/v1/app_attest.proto",
        "byte/v/forge/waapp/v1/recaptcha.proto",
    ]
)
raise SystemExit(result)
