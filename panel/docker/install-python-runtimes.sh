#!/bin/sh
# Install versioned CPython runtimes for Docker images.
#
# The panel creates per-version venvs at runtime, but Docker images must still
# provide the matching python3.10 / python3.11 / python3.12 bootstrap binaries.

set -eu

RUNTIME_FLAVOR=${1:-alpine}
TARGET_ARCH=${2:-}
TARGET_VARIANT=${3:-}
PYTHON_STANDALONE_RELEASE=${4:-20260602}
PYTHON_RUNTIME_310=${5:-3.10.20}
PYTHON_RUNTIME_311=${6:-3.11.15}
PYTHON_RUNTIME_312=${7:-3.12.13}
PYTHON_RUNTIME_MODE=${8:-single}
PYTHON_RUNTIME_VERSION=${9:-3.12}

INSTALL_ROOT=${PYTHON_RUNTIME_ROOT:-/opt/daidai-python}
BASE_URL="https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON_STANDALONE_RELEASE}"

log() {
  printf '[python-runtime] %s\n' "$*"
}

case "$PYTHON_RUNTIME_MODE" in
  all|single)
    ;;
  *)
    log "unknown PYTHON_RUNTIME_MODE=${PYTHON_RUNTIME_MODE}, fallback to single"
    PYTHON_RUNTIME_MODE=single
    ;;
esac

case "$PYTHON_RUNTIME_VERSION" in
  3.10|3.11|3.12)
    ;;
  *)
    log "unknown PYTHON_RUNTIME_VERSION=${PYTHON_RUNTIME_VERSION}, fallback to 3.12"
    PYTHON_RUNTIME_VERSION=3.12
    ;;
esac

fetch() {
  url=$1
  out=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    log "curl/wget unavailable, skip Python runtime installation"
    return 1
  fi
}

python_platform() {
  flavor=$1
  arch=$2
  variant=$3

  case "${flavor}:${arch}" in
    alpine:amd64)
      printf '%s' 'x86_64-unknown-linux-musl'
      ;;
    alpine:arm64)
      printf '%s' 'aarch64-unknown-linux-musl'
      ;;
    debian:amd64)
      printf '%s' 'x86_64-unknown-linux-gnu'
      ;;
    debian:arm64)
      printf '%s' 'aarch64-unknown-linux-gnu'
      ;;
    debian:arm)
      case "$variant" in
        v7|'')
          printf '%s' 'armv7-unknown-linux-gnueabihf'
          ;;
      esac
      ;;
  esac
}

install_python() {
  minor=$1
  full_version=$2
  platform=$3

  archive="cpython-${full_version}+${PYTHON_STANDALONE_RELEASE}-${platform}-install_only.tar.gz"
  url="${BASE_URL}/${archive}"
  tmp="/tmp/${archive}"
  stage="${INSTALL_ROOT}/${minor}.tmp"
  dest="${INSTALL_ROOT}/${minor}"

  log "install Python ${minor} from ${archive}"
  rm -rf "$stage" "$dest"
  mkdir -p "$stage" "$INSTALL_ROOT"
  fetch "$url" "$tmp"
  tar -xzf "$tmp" -C "$stage"
  mv "$stage/python" "$dest"
  rm -rf "$stage" "$tmp"

  ln -sf "${dest}/bin/python${minor}" "/usr/local/bin/python${minor}"
  ln -sf "${dest}/bin/pip${minor}" "/usr/local/bin/pip${minor}"

  export PATH="${dest}/bin:${PATH}"
  export LD_LIBRARY_PATH="${dest}/lib${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"

  "python${minor}" --version
  "python${minor}" -m pip --version
}

should_install_python() {
  minor=$1
  if [ "$PYTHON_RUNTIME_MODE" = "all" ]; then
    return 0
  fi
  [ "$minor" = "$PYTHON_RUNTIME_VERSION" ]
}

PLATFORM=$(python_platform "$RUNTIME_FLAVOR" "$TARGET_ARCH" "$TARGET_VARIANT" || true)

if [ -z "$PLATFORM" ]; then
  # 默认 3.12 镜像在 Alpine 32 位平台上可以继续使用发行版 python3；
  # 但 3.10 / 3.11 / all 镜像如果没有独立运行时资产，必须直接失败，避免推送“名字是 3.10，实际却只有系统 Python”的假镜像。
  if [ "$PYTHON_RUNTIME_MODE" = "single" ] && [ "$PYTHON_RUNTIME_VERSION" = "3.12" ]; then
    log "no standalone CPython asset for flavor=${RUNTIME_FLAVOR} arch=${TARGET_ARCH} variant=${TARGET_VARIANT}; keep distro python only"
    exit 0
  fi
  log "no standalone CPython asset for flavor=${RUNTIME_FLAVOR} arch=${TARGET_ARCH} variant=${TARGET_VARIANT}; cannot build mode=${PYTHON_RUNTIME_MODE} version=${PYTHON_RUNTIME_VERSION}"
  exit 1
fi

if should_install_python "3.10"; then
  install_python "3.10" "$PYTHON_RUNTIME_310" "$PLATFORM"
fi
if should_install_python "3.11"; then
  install_python "3.11" "$PYTHON_RUNTIME_311" "$PLATFORM"
fi
if should_install_python "3.12"; then
  install_python "3.12" "$PYTHON_RUNTIME_312" "$PLATFORM"
fi

# 让通用 python3 / pip3 落到当前镜像默认版本；all 镜像仍默认 3.12。
# 这样 latest3.10 / debian3.10 这类单版本镜像里，任务和 venv 创建都会优先使用对应小版本。
default_root="${INSTALL_ROOT}/${PYTHON_RUNTIME_VERSION}"
if [ ! -d "${default_root}/bin" ] && [ -d "${INSTALL_ROOT}/3.12/bin" ]; then
  default_root="${INSTALL_ROOT}/3.12"
  PYTHON_RUNTIME_VERSION=3.12
fi
if [ -d "${default_root}/bin" ]; then
  ln -sf "${default_root}/bin/python${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/python3"
  ln -sf "${default_root}/bin/pip${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/pip3"
  ln -sf "${default_root}/bin/pip${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/pip"
fi

log "Python runtimes installed under ${INSTALL_ROOT} (mode=${PYTHON_RUNTIME_MODE}, default=${PYTHON_RUNTIME_VERSION})"
