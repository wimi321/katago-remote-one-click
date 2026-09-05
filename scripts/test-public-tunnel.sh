#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CACHE="${ROOT}/.test-output/cloudflared"
VERSION="2026.8.3"
mkdir -p "${CACHE}"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

download_verified() {
  local url="$1"
  local target="$2"
  local expected="$3"
  if [[ ! -f "${target}" ]] || [[ "$(sha256_file "${target}")" != "${expected}" ]]; then
    curl --fail --location --retry 5 --retry-all-errors --output "${target}.part" "${url}"
    [[ "$(sha256_file "${target}.part")" == "${expected}" ]]
    mv -f "${target}.part" "${target}"
  fi
}

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)
    archive="${CACHE}/cloudflared-darwin-arm64.tgz"
    download_verified \
      "https://github.com/cloudflare/cloudflared/releases/download/${VERSION}/cloudflared-darwin-arm64.tgz" \
      "${archive}" \
      "40c9144d86df8937c5b43293a1f7d2d2107029aa74725023dd46b1b27154352f"
    tar -xzf "${archive}" -C "${CACHE}"
    cloudflared="${CACHE}/cloudflared"
    ;;
  Linux-x86_64|Linux-amd64)
    cloudflared="${CACHE}/cloudflared"
    download_verified \
      "https://github.com/cloudflare/cloudflared/releases/download/${VERSION}/cloudflared-linux-amd64" \
      "${cloudflared}" \
      "f29324fe934d1e100617484c78deef803c4dc2cd351d645bbde42e96b4fccc5e"
    ;;
  *)
    echo "Public tunnel test supports macOS arm64 and Linux x86_64 only." >&2
    exit 1
    ;;
esac

chmod 700 "${cloudflared}"
KATAGO_REMOTE_PUBLIC_E2E=1 \
KATAGO_REMOTE_CLOUDFLARED="${cloudflared}" \
go test -count=1 -run TestPublicQuickTunnelRoundTrip -v ./integration
