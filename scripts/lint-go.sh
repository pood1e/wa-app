#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

modules=(".")

unformatted="$(
  git ls-files -z --cached --others --exclude-standard '*.go' |
    while IFS= read -r -d '' source_file; do
      if [[ -f "${source_file}" ]]; then gofmt -l "${source_file}"; fi
    done | sort -u
)"

if [[ -n "${unformatted}" ]]; then
  echo "gofmt required for:"
  echo "${unformatted}"
  exit 1
fi

go list -deps ./cmd/... |
  while IFS= read -r dependency; do
    case "${dependency}" in
      go.mau.fi/whatsmeow/proto/* | go.mau.fi/whatsmeow/binary | go.mau.fi/whatsmeow/binary/* | go.mau.fi/whatsmeow/types) ;;
      go.mau.fi/whatsmeow | go.mau.fi/whatsmeow/*)
        echo "native protocol dependency boundary violated: ${dependency}"
        exit 1
        ;;
    esac
  done

for module in "${modules[@]}"; do
  if [[ -f "${module}/go.mod" ]]; then
    echo "go vet ${module}"
    (cd "${module}" && go vet ./...)
  fi
done
