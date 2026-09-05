#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

bash -n install.sh scripts/*.sh
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck install.sh scripts/*.sh
fi
if command -v actionlint >/dev/null 2>&1; then
  actionlint
fi

if grep -RInE '(password|api[_-]?key|secret)[[:space:]]*=[[:space:]]*["'"'][^"'"']+["'"']' \
  --exclude-dir=.git --exclude='*_test.go' .; then
  echo "Potential embedded credential found" >&2
  exit 1
fi

python3 -m json.tool colab/KataGo_Remote_One_Click.ipynb >/dev/null

if grep -q '__CONFIG_SHA256__' install.sh; then
  echo "Installer config checksum placeholder remains" >&2
  exit 1
fi

if [[ "${ALLOW_UNRELEASED_BINARY_SHA:-0}" != "1" ]] && grep -q '__BINARY_SHA256__' install.sh; then
  echo "Installer binary checksum placeholder remains" >&2
  exit 1
fi

go mod verify
go vet ./...
go test -race ./...
git diff --check
