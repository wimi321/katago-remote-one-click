#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-v0.1.0}"
DIST="${ROOT}/dist"
mkdir -p "${DIST}"
rm -f "${DIST}"/*

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X main.version=${VERSION}" \
  -o "${DIST}/katago-remote-linux-amd64" \
  "${ROOT}/cmd/katago-remote"

chmod 755 "${DIST}/katago-remote-linux-amd64"
cp "${ROOT}/install.sh" "${DIST}/install.sh"
cp "${ROOT}/LICENSE" "${DIST}/LICENSE"

(
  cd "${DIST}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum katago-remote-linux-amd64 install.sh LICENSE > SHA256SUMS
  else
    shasum -a 256 katago-remote-linux-amd64 install.sh LICENSE > SHA256SUMS
  fi
)

expected="$(sed -n 's/^APP_SHA256="\([0-9a-f]\{64\}\)"$/\1/p' "${ROOT}/install.sh")"
actual="$(sha256_file "${DIST}/katago-remote-linux-amd64")"
if [[ -z "${expected}" || "${actual}" != "${expected}" ]]; then
  printf 'Installer binary SHA mismatch: expected=%s actual=%s\n' "${expected:-unset}" "${actual}" >&2
  exit 1
fi

printf 'Release artifacts created in %s\n' "${DIST}"
