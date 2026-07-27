#!/usr/bin/env bash
##########################################################################
# 呆呆面板 Magisk 模块打包脚本 (容器方案 v2.0.6+)
#
# 用法:
#   bash Magisk/build.sh            # 默认打包 arm64
#   bash Magisk/build.sh 2.3.1     # 指定版本号
#   bash Magisk/build.sh 2.3.1 all # 同时打包 arm64 + amd64
#
# 产物: dist/daidai-panel-magisk-v<版本>.zip
#
# 模块内部不再内置 Python/Node；改为在 customize.sh 里用 rurima + Alpine
# minirootfs 构建一个 musl 容器，apk add 出 python3 / nodejs / npm / git 等。
##########################################################################

set -euo pipefail

VERSION="${1:-2.3.1}"
TARGETS="${2:-arm64}"     # arm64 / amd64 / all

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

MODDIR="$ROOT/Magisk"
DIST="$ROOT/dist"
STAGING="$DIST/magisk-staging"
OUTZIP="$DIST/daidai-panel-magisk-v${VERSION}.zip"

info()  { printf "\033[1;32m[INFO]\033[0m %s\n" "$*" >&2; }
warn()  { printf "\033[1;33m[WARN]\033[0m %s\n" "$*" >&2; }
error() { printf "\033[1;31m[ERR ]\033[0m %s\n" "$*" >&2; }

command -v go   >/dev/null || { error "缺少 go"; exit 1; }
command -v npm  >/dev/null || { error "缺少 npm"; exit 1; }

# Windows Git Bash 下通常没有 zip，用 python 兜底打包
PY_FALLBACK=""
if command -v py >/dev/null; then
  PY_FALLBACK="py"
elif command -v python3 >/dev/null; then
  PY_FALLBACK="python3"
elif command -v python >/dev/null; then
  if python -c "print(1)" >/dev/null 2>&1; then
    PY_FALLBACK="python"
  fi
fi
if ! command -v zip >/dev/null; then
  if [ -z "$PY_FALLBACK" ]; then
    error "缺少 zip 且未找到可用 python，请安装其一"
    exit 1
  fi
  warn "未找到 zip，将使用 $PY_FALLBACK 做 ZIP 打包"
fi

# 1. 前端构建
if [ ! -d "$ROOT/web/dist" ]; then
  info "前端 dist 不存在，开始构建..."
  (cd "$ROOT/web" && npm ci && npm run build)
else
  info "已存在 web/dist，跳过前端构建（如需强制重建请先删除 web/dist）"
fi

# 2. 后端交叉编译（Alpine musl 环境下也能跑 CGO_ENABLED=0 的 Go 静态二进制）
rm -rf "$STAGING"
mkdir -p "$STAGING/system/bin" "$STAGING/web" "$DIST"

build_backend() {
  local go_arch="$1"
  local suffix="$2"
  info "编译后端: GOOS=linux GOARCH=${go_arch}"
  (cd "$ROOT/server" && \
    CGO_ENABLED=0 GOOS=linux GOARCH="${go_arch}" \
    go build -trimpath \
      -ldflags="-s -w -X daidai-panel/handler.Version=${VERSION}" \
      -o "$STAGING/system/bin/daidai-server-${suffix}" .)
  (cd "$ROOT/server" && \
    CGO_ENABLED=0 GOOS=linux GOARCH="${go_arch}" \
    go build -trimpath \
      -ldflags="-s -w -X daidai-panel/handler.Version=${VERSION}" \
      -o "$STAGING/system/bin/ddp-${suffix}" ./cmd/ddp)
}

case "$TARGETS" in
  arm64) build_backend arm64 arm64 ;;
  amd64) build_backend amd64 amd64 ;;
  all)
    build_backend arm64 arm64
    build_backend amd64 amd64
    ;;
  *) error "未知架构: $TARGETS （支持: arm64 / amd64 / all）"; exit 1 ;;
esac

# 3. 拷贝模块文件（Git Bash 上 *.sh 可能带 CRLF，BusyBox sh 解析不了，统一过 tr 一遍）
info "打包模块文件..."
copy_sh() {
  tr -d '\r' < "$1" > "$2"
  chmod +x "$2" 2>/dev/null || true
}

copy_sh "$MODDIR/customize.sh"                       "$STAGING/customize.sh"
copy_sh "$MODDIR/service.sh"                         "$STAGING/service.sh"
copy_sh "$MODDIR/uninstall.sh"                       "$STAGING/uninstall.sh"
copy_sh "$MODDIR/action.sh"                          "$STAGING/action.sh"
cp -f   "$MODDIR/module.prop"                        "$STAGING/module.prop"
[ -f "$MODDIR/README.md" ] && cp -f "$MODDIR/README.md" "$STAGING/README.md"

# 容器二进制（rurima）—— 从 Magisk/system/bin/ 拷到 staging/system/bin/
if [ -f "$MODDIR/system/bin/rurima" ]; then
  cp -f "$MODDIR/system/bin/rurima" "$STAGING/system/bin/rurima"
  chmod +x "$STAGING/system/bin/rurima"
else
  error "缺少 $MODDIR/system/bin/rurima（容器运行时），请先放置静态 rurima 二进制"
  exit 1
fi

# 离线 apk（linux-pam / shadow）
if [ -d "$MODDIR/apk" ]; then
  mkdir -p "$STAGING/apk"
  cp -f "$MODDIR/apk/"*.apk "$STAGING/apk/" 2>/dev/null || true
fi

# scripts/
if [ -d "$MODDIR/scripts" ]; then
  mkdir -p "$STAGING/scripts"
  for f in "$MODDIR"/scripts/*; do
    [ -f "$f" ] || continue
    name="$(basename "$f")"
    case "$name" in
      *.sh) copy_sh "$f" "$STAGING/scripts/$name" ;;
      *)    cp -f "$f" "$STAGING/scripts/$name" ;;
    esac
  done
fi

# META-INF/
if [ -d "$MODDIR/META-INF" ]; then
  mkdir -p "$STAGING/META-INF/com/google/android"
  for f in "$MODDIR"/META-INF/com/google/android/*; do
    [ -f "$f" ] || continue
    name="$(basename "$f")"
    copy_sh "$f" "$STAGING/META-INF/com/google/android/$name"
  done
fi

# 同步版本号到 module.prop
# versionCode: 2.0.6 -> 20006 (MAJ*10000 + MIN*100 + PATCH)，与 CI 保持一致
IFS='.' read -r _MAJ _MIN _PATCH <<<"$VERSION"
_MAJ=${_MAJ:-0}; _MIN=${_MIN:-0}; _PATCH=${_PATCH:-0}
VERSIONCODE=$(( _MAJ * 10000 + _MIN * 100 + _PATCH ))
sed -i.bak \
  -e "s|^version=.*|version=v${VERSION}|" \
  -e "s|^versionCode=.*|versionCode=${VERSIONCODE}|" \
  "$STAGING/module.prop"
rm -f "$STAGING/module.prop.bak"

# 前端静态资源
cp -rf "$ROOT/web/dist/"* "$STAGING/web/"

# 4. 打包 ZIP
rm -f "$OUTZIP"
info "生成 ZIP: $OUTZIP"
if command -v zip >/dev/null; then
  (cd "$STAGING" && zip -r9 "$OUTZIP" . -x "*.DS_Store")
else
  $PY_FALLBACK - "$STAGING" "$OUTZIP" <<'PY'
import os, sys, zipfile
staging, out = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    for root, dirs, files in os.walk(staging):
        for f in files:
            if f == '.DS_Store':
                continue
            full = os.path.join(root, f)
            rel = os.path.relpath(full, staging).replace('\\', '/')
            z.write(full, rel)
print(f"wrote {out}")
PY
fi

info "完成: $OUTZIP"
info "用法: 在 Magisk / KernelSU / APatch 管理器中选择此 ZIP 安装即可。"
