#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="v0.1.0"
REPOSITORY="wimi321/katago-remote-one-click"

APP_URL="https://github.com/${REPOSITORY}/releases/download/${VERSION}/katago-remote-linux-amd64"
APP_SHA256="c81845071203e5615b2cb0369e746015165afefa3b13b22fd7ba0cc8ea8bf318"

CLOUDFLARED_VERSION="2026.8.3"
CLOUDFLARED_URL="https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/cloudflared-linux-amd64"
CLOUDFLARED_SHA256="f29324fe934d1e100617484c78deef803c4dc2cd351d645bbde42e96b4fccc5e"

KATAGO_VERSION="1.18.2"
KATAGO_CUDNN9_URL="https://github.com/lightvector/KataGo/releases/download/v${KATAGO_VERSION}/katago-v${KATAGO_VERSION}-cuda12.1-cudnn9.8.0-linux-x64.zip"
KATAGO_CUDNN9_SHA256="3e30b486b7bc38287eeead350aa832ff8ca50d1f0c0a83b5409cabf8a3bc9c5d"
KATAGO_CUDNN8_URL="https://github.com/lightvector/KataGo/releases/download/v${KATAGO_VERSION}/katago-v${KATAGO_VERSION}-cuda12.1-cudnn8.9.7-linux-x64.zip"
KATAGO_CUDNN8_SHA256="16c69f42291fe8c6d196d722d92299a5b04de852611d6c022a0dd9e0e83b5688"

MODEL_NAME="b10c512h8nbt3tflrs-fson-silu-rsnh.bin.gz"
MODEL_URL="https://github.com/lightvector/KataGo/releases/download/v1.17.0/${MODEL_NAME}"
MODEL_SHA256="c04db4a503721d948bb720324f3cbdac6088cc9eb243632f020e4b6846f58995"

ANALYSIS_CONFIG_URL="https://raw.githubusercontent.com/${REPOSITORY}/${VERSION}/config/analysis.cfg"
ANALYSIS_CONFIG_SHA256="15c1223ae21391f3cdb52c13fbda8c1a7f76297cf9f58b22f13b28fe7c999be9"

INSTALL_HOME="${KATAGO_REMOTE_HOME:-${HOME}/.local/share/katago-remote-one-click}"
USER_BIN_DIR="${HOME}/.local/bin"
START_AFTER_INSTALL=1
DRY_RUN=0

usage() {
  cat <<'EOF'
KataGo Remote One-Click / KataGo 远程算力一键部署

Usage:
  curl -fsSL https://raw.githubusercontent.com/wimi321/katago-remote-one-click/v0.1.0/install.sh | bash
  bash install.sh [--no-start] [--dry-run]

Options:
  --no-start  Install and verify files without starting the service.
  --dry-run   Print the installation plan without changing the computer.
  --help      Show this help.

Optional environment variables:
  KATAGO_REMOTE_HOME         Installation directory.
  KATAGO_REMOTE_KATAGO_PATH Use an existing KataGo executable.
  KATAGO_REMOTE_MODEL_PATH  Use an existing neural network model.
  KATAGO_REMOTE_CONFIG_PATH Use an existing analysis config.
EOF
}

for argument in "$@"; do
  case "${argument}" in
    --no-start) START_AFTER_INSTALL=0 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown option: ${argument}" >&2; usage >&2; exit 2 ;;
  esac
done

say() {
  printf '\n%s\n' "$*"
}

fail() {
  printf '\nInstallation failed / 安装失败: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing command: $1 / 缺少命令：$1"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "sha256sum or shasum is required / 需要 SHA-256 校验工具"
  fi
}

verify_file() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "${path}")"
  [[ "${actual}" == "${expected}" ]] || fail "SHA-256 mismatch for ${path}"
}

download() {
  local url="$1"
  local destination="$2"
  local expected="$3"
  if [[ -f "${destination}" ]] && [[ "$(sha256_file "${destination}")" == "${expected}" ]]; then
    printf 'Reusing / 继续使用: %s\n' "${destination}"
    return
  fi
  mkdir -p "$(dirname "${destination}")"
  local partial="${destination}.part"
  printf 'Downloading / 正在下载: %s\n' "$(basename "${destination}")"
  curl --fail --location --retry 5 --retry-all-errors --connect-timeout 20 \
    --continue-at - --output "${partial}" "${url}"
  verify_file "${partial}" "${expected}"
  mv -f "${partial}" "${destination}"
}

absolute_path() {
  local value="$1"
  local directory
  directory="$(cd "$(dirname "${value}")" && pwd -P)"
  printf '%s/%s\n' "${directory}" "$(basename "${value}")"
}

extract_katago() {
  local archive="$1"
  local destination="$2"
  rm -rf "${destination}.tmp"
  mkdir -p "${destination}.tmp"
  unzip -q -o "${archive}" -d "${destination}.tmp"
  local executable
  executable="$(find "${destination}.tmp" -type f -name katago -print -quit)"
  [[ -n "${executable}" ]] || fail "KataGo executable was not found in ${archive}"
  chmod 700 "${executable}"
  rm -rf "${destination}"
  mv "${destination}.tmp" "${destination}"
  executable="$(find "${destination}" -type f -name katago -print -quit)"
  printf '%s\n' "${executable}"
}

katago_works() {
  local executable="$1"
  "${executable}" version >/dev/null 2>"${INSTALL_HOME}/logs/katago-version-check.log"
}

if [[ "${DRY_RUN}" == "1" ]]; then
  cat <<EOF
KataGo Remote One-Click ${VERSION}
Target: ${INSTALL_HOME}
Platform: Linux x86_64 with an NVIDIA GPU
Downloads: service binary, cloudflared ${CLOUDFLARED_VERSION}, KataGo ${KATAGO_VERSION}, ${MODEL_NAME}
Security: localhost-only listener, random private token, encrypted WSS tunnel
EOF
  exit 0
fi

[[ "$(uname -s)" == "Linux" ]] || fail "This installer currently supports Linux GPU servers only / 目前仅支持 Linux GPU 服务器"
case "$(uname -m)" in
  x86_64|amd64) ;;
  *) fail "This release supports x86_64 servers only / 当前版本仅支持 x86_64 服务器" ;;
esac

umask 077
require_command curl
require_command unzip
require_command find
require_command awk
require_command nvidia-smi

if [[ "${APP_SHA256}" == __* || "${ANALYSIS_CONFIG_SHA256}" == __* ]]; then
  fail "This installer belongs to an unreleased development build"
fi

mkdir -p "${INSTALL_HOME}"/{bin,downloads,engine,models,config,state,logs}
chmod 700 "${INSTALL_HOME}" "${INSTALL_HOME}"/{bin,downloads,engine,models,config,state,logs}

say "1/5 Installing the secure bridge / 安装安全连接服务"
APP_PATH="${INSTALL_HOME}/bin/katago-remote"
download "${APP_URL}" "${APP_PATH}" "${APP_SHA256}"
chmod 700 "${APP_PATH}"

CLOUDFLARED_PATH="${INSTALL_HOME}/bin/cloudflared"
download "${CLOUDFLARED_URL}" "${CLOUDFLARED_PATH}" "${CLOUDFLARED_SHA256}"
chmod 700 "${CLOUDFLARED_PATH}"

say "2/5 Preparing KataGo / 准备 KataGo"
if [[ -n "${KATAGO_REMOTE_KATAGO_PATH:-}" ]]; then
  KATAGO_PATH="$(absolute_path "${KATAGO_REMOTE_KATAGO_PATH}")"
  [[ -x "${KATAGO_PATH}" ]] || fail "Existing KataGo is not executable: ${KATAGO_PATH}"
else
  CUDNN9_ARCHIVE="${INSTALL_HOME}/downloads/katago-${KATAGO_VERSION}-cuda12.1-cudnn9.zip"
  download "${KATAGO_CUDNN9_URL}" "${CUDNN9_ARCHIVE}" "${KATAGO_CUDNN9_SHA256}"
  KATAGO_PATH="$(extract_katago "${CUDNN9_ARCHIVE}" "${INSTALL_HOME}/engine/cudnn9")"
  if ! katago_works "${KATAGO_PATH}"; then
    say "cuDNN 9 build did not start; trying the compatibility build / cuDNN 9 无法启动，尝试兼容版本"
    CUDNN8_ARCHIVE="${INSTALL_HOME}/downloads/katago-${KATAGO_VERSION}-cuda12.1-cudnn8.zip"
    download "${KATAGO_CUDNN8_URL}" "${CUDNN8_ARCHIVE}" "${KATAGO_CUDNN8_SHA256}"
    KATAGO_PATH="$(extract_katago "${CUDNN8_ARCHIVE}" "${INSTALL_HOME}/engine/cudnn8")"
  fi
fi
if ! katago_works "${KATAGO_PATH}"; then
  cat "${INSTALL_HOME}/logs/katago-version-check.log" >&2 || true
  fail "KataGo cannot use this server's CUDA/cuDNN runtime. Choose a CUDA 12 + cuDNN GPU image, then rerun. / 当前服务器缺少兼容的 CUDA/cuDNN 运行环境，请切换到 CUDA 12 + cuDNN 镜像后重试。"
fi

say "3/5 Preparing the model / 准备权重"
if [[ -n "${KATAGO_REMOTE_MODEL_PATH:-}" ]]; then
  MODEL_PATH="$(absolute_path "${KATAGO_REMOTE_MODEL_PATH}")"
  [[ -f "${MODEL_PATH}" ]] || fail "Existing model was not found: ${MODEL_PATH}"
else
  MODEL_PATH="${INSTALL_HOME}/models/${MODEL_NAME}"
  download "${MODEL_URL}" "${MODEL_PATH}" "${MODEL_SHA256}"
fi

if [[ -n "${KATAGO_REMOTE_CONFIG_PATH:-}" ]]; then
  CONFIG_PATH="$(absolute_path "${KATAGO_REMOTE_CONFIG_PATH}")"
  [[ -f "${CONFIG_PATH}" ]] || fail "Existing analysis config was not found: ${CONFIG_PATH}"
else
  CONFIG_PATH="${INSTALL_HOME}/config/analysis.cfg"
  download "${ANALYSIS_CONFIG_URL}" "${CONFIG_PATH}" "${ANALYSIS_CONFIG_SHA256}"
fi

say "4/5 Saving the private configuration / 保存私密配置"
"${APP_PATH}" stop --home "${INSTALL_HOME}" >/dev/null 2>&1 || true
"${APP_PATH}" init \
  --home "${INSTALL_HOME}" \
  --katago "${KATAGO_PATH}" \
  --model "${MODEL_PATH}" \
  --config "${CONFIG_PATH}" \
  --cloudflared "${CLOUDFLARED_PATH}"

mkdir -p "${USER_BIN_DIR}"
if [[ ! -e "${USER_BIN_DIR}/katago-remote" || -L "${USER_BIN_DIR}/katago-remote" ]]; then
  ln -sfn "${APP_PATH}" "${USER_BIN_DIR}/katago-remote"
fi

say "5/5 Checking the installation / 检查安装"
if ! "${APP_PATH}" doctor --home "${INSTALL_HOME}"; then
  fail "Environment validation failed. Run '${APP_PATH} doctor' after fixing the item above."
fi

if [[ "${START_AFTER_INSTALL}" == "1" ]]; then
  "${APP_PATH}" start --home "${INSTALL_HOME}"
else
  say "Installed without starting / 已安装但未启动"
  printf 'Start later / 稍后启动: %s start --home %q\n' "${APP_PATH}" "${INSTALL_HOME}"
fi
