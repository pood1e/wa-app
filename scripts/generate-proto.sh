#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
PATH="$(go env GOPATH)/bin:$PATH"

for tool in protoc protoc-gen-go protoc-gen-go-grpc; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "required generator is unavailable: $tool" >&2
    exit 1
  fi
done

rm -rf "$ROOT/gen"
mkdir -p "$ROOT/gen/go"

protoc -I "$ROOT/proto" \
  --go_out="$ROOT" \
  --go_opt=module=github.com/pood1e/wa-app \
  --go-grpc_out="$ROOT" \
  --go-grpc_opt=module=github.com/pood1e/wa-app \
  $(find "$ROOT/proto" -name '*.proto' | sort)

gofmt -w "$ROOT/gen/go"
