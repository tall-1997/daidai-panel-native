#!/usr/bin/env bash
##########################################################################
# 呆呆面板 Magisk 模块打包脚本 (容器方案 v2.0.6+)
#
# 用法（版本号必填）:
#   bash Magisk/build.sh 3.0.6                # arm64 + alpine
#   bash Magisk/build.sh 3.0.6 all            # 同时打包 arm64 + amd64
#   bash Magisk/build.sh 3.0.6 arm64 debian   # Debian(glibc) flavor
#
# 产物:
#   alpine（默认）: dist/daidai-panel-magisk-v<版本>.zip
#   debian        : dist/daidai-panel-magisk-debian-v<版本>.zip
#
# 模块内部不再内置 Python/Node；改为在 customize.sh 里用 rurima + rootfs 构建一个
# 容器，再用 apk / apt-get 装出 python3 / nodejs / npm / git 等：
#   alpine —— Alpine 3.18 minirootfs（musl，体积小）
#   debian —— CI 自建的 Debian bookworm 精简 rootfs（glibc，能跑官方预编译产物）
##########################################################################

set -euo pipefail

# 版本号必填。原来这里有个默认值，但它每次发版都会漏更新（v3.0.1 就漏了），
# 结果本地不传参会打出一个标着旧版本号的包 —— 那种包看不出错，装上才发现不对。
# CI 一直是显式传参的（release.yml 的 magisk-module job），所以改成必填不影响它。
VERSION="${1:?用法: bash Magisk/build.sh <版本号> [arm64|amd64|all] [alpine|debian]}"
TARGETS="${2:-arm64}"     # arm64 / amd64 / all
FLAVOR="${3:-alpine}"     # alpine / debian —— 不传时行为与产物名与历史完全一致

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

# alpine 走空后缀，保证默认产物名 / staging 目录名与历史版本逐字节一致
case "$FLAVOR" in
  alpine) FLAVOR_SUFFIX="" ;;
  debian) FLAVOR_SUFFIX="-debian" ;;
  *) printf "\033[1;31m[ERR ]\033[0m 未知 flavor: %s （支持: alpine / debian）\n" "$FLAVOR" >&2; exit 1 ;;
esac

MODDIR="$ROOT/Magisk"
DIST="$ROOT/dist"
STAGING="$DIST/magisk-staging${FLAVOR_SUFFIX}"
OUTZIP="$DIST/daidai-panel-magisk${FLAVOR_SUFFIX}-v${VERSION}.zip"

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
info "打包模块文件... (flavor=$FLAVOR)"
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

# flavor 标记文件：customize.sh / service.sh / action.sh 都读它来决定容器 rootfs
# 来源、容器内 shell、包管理器。刻意不用 sed 把 flavor 烤进脚本 —— 那样 ZIP 里的
# 脚本和仓库里的源码就不是同一份，排障时看到的和实际跑的对不上。
printf '%s\n' "$FLAVOR" > "$STAGING/flavor"

# 容器二进制（rurima）—— 从 Magisk/system/bin/ 拷到 staging/system/bin/
if [ -f "$MODDIR/system/bin/rurima" ]; then
  cp -f "$MODDIR/system/bin/rurima" "$STAGING/system/bin/rurima"
  chmod +x "$STAGING/system/bin/rurima"
else
  error "缺少 $MODDIR/system/bin/rurima（容器运行时），请先放置静态 rurima 二进制"
  exit 1
fi

# 离线 apk（linux-pam / shadow）—— 只有 alpine flavor 需要。
# 这两个包是 aarch64 Alpine 专用，Debian 侧同等能力由 apt 的 passwd / libpam 提供，
# 塞进 Debian ZIP 只会白白撑大体积、并让 customize.sh 里多一条永远走不到的分支。
if [ "$FLAVOR" = "alpine" ] && [ -d "$MODDIR/apk" ]; then
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

# updateJson 按 flavor 分开。
# 两个 flavor 共用同一个 module id（daidai-panel），而 updateJson 只能填一个 zipUrl，
# 所以以前 Debian 用户在管理器里点「更新」会被静默刷成 Alpine 版 —— 容器基础系统
# 直接从 glibc 换成 musl，装好的依赖全部失效。这里让 Debian ZIP 指向自己的
# update-debian.json，两条更新线彻底分开。
# 只把文件名从 update.json 换成 update-debian.json，仓库地址原样保留 ——
# fork 的用户会把 module.prop 里的 updateJson 改成自己的仓库，写死上游地址会把它覆盖掉。
if [ "$FLAVOR" = "debian" ]; then
  sed -i.bak \
    -e "s|^updateJson=\(.*\)/update\.json$|updateJson=\1/update-debian.json|" \
    "$STAGING/module.prop"
  rm -f "$STAGING/module.prop.bak"
  if ! grep -q '^updateJson=.*/update-debian\.json$' "$STAGING/module.prop"; then
    error "Debian flavor 的 updateJson 改写失败，请检查 Magisk/module.prop 里 updateJson 的写法"
    exit 1
  fi
fi

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

info "完成: $OUTZIP (flavor=$FLAVOR)"
info "用法: 在 Magisk / KernelSU / APatch 管理器中选择此 ZIP 安装即可。"
