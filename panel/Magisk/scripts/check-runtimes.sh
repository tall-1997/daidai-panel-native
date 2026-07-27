#!/system/bin/sh
##########################################################################
# 脚本运行时自检工具
#
# 用法:
#   su -c "sh /data/adb/modules/daidai-panel/scripts/check-runtimes.sh"
#
# 用途:
#   检查呆呆面板依赖的常见脚本运行时是否可用，并给出建议。
#   调用方式可以是 adb shell，也可以是模块卡片上的「运行」按钮。
##########################################################################

PANEL_DIR=/data/adb/daidai-panel

# 组装与 service.sh 一致的 PATH，确保自检与实际运行时一致
MODDIR="/data/adb/modules/daidai-panel"
TERMUX_PATHS=""
for p in \
  /data/data/com.termux/files/usr/bin \
  /data/data/com.termux/files/usr/local/bin \
  /data/user/0/com.termux/files/usr/bin; do
  [ -d "$p" ] && TERMUX_PATHS="${TERMUX_PATHS:+$TERMUX_PATHS:}$p"
done
PANEL_RUNTIME_PATHS=""
for p in \
  "$PANEL_DIR/bin/python/bin" \
  "$PANEL_DIR/bin/node/bin" \
  "$PANEL_DIR/bin"; do
  [ -d "$p" ] && PANEL_RUNTIME_PATHS="${PANEL_RUNTIME_PATHS:+$PANEL_RUNTIME_PATHS:}$p"
done
export PATH="${PANEL_RUNTIME_PATHS:+$PANEL_RUNTIME_PATHS:}$MODDIR${TERMUX_PATHS:+:$TERMUX_PATHS}:/sbin:/system/bin:/system/xbin:/vendor/bin:$PATH"

say() { echo "$1"; }

say "============================================"
say " 呆呆面板 - 脚本运行时自检"
say "============================================"
say "PATH = $PATH"
say ""

check() {
  local name="$1"; shift
  local bin
  bin=$(command -v "$name" 2>/dev/null)
  if [ -n "$bin" ]; then
    local ver
    ver=$("$@" 2>&1 | head -n1)
    say "✅ $name  -> $bin"
    [ -n "$ver" ] && say "     $ver"
  else
    say "❌ $name  (未检测到)"
  fi
}

say "-- 基础 Shell --"
check sh    sh -c 'echo $0'
check bash  bash --version
check busybox busybox --help
say ""

say "-- 脚本解释器 --"
check python3 python3 --version
check python  python --version
check node    node --version
check npm     npm --version
check pnpm    pnpm --version
check yarn    yarn --version
check "ts-node" ts-node --version
check go      go version
say ""

say "-- 工具链 --"
check git   git --version
check curl  curl --version
check wget  wget --version
check unzip unzip -v
check tar   tar --version
say ""

TERMUX_OK=0
for p in /data/data/com.termux/files/usr/bin /data/user/0/com.termux/files/usr/bin; do
  [ -d "$p" ] && TERMUX_OK=1
done

say "============================================"
if [ "$TERMUX_OK" = "1" ]; then
  say " 检测到 Termux，其 bin 已加入面板 PATH"
  say "  如缺少某个解释器，在 Termux 内执行:"
  say "    pkg update && pkg install python nodejs git"
  say "  再重启手机或重启面板即可。"
else
  say " 未检测到 Termux。Android 默认不带 Python / Node，"
  say " 建议任选以下一种方案，让面板能跑脚本："
  say ""
  say "  方案 A (推荐): 安装 Termux"
  say "    1. 从 F-Droid 安装 Termux"
  say "    2. 打开 Termux，执行:"
  say "         pkg update"
  say "         pkg install python nodejs git curl"
  say "    3. 重启手机或面板"
  say ""
  say "  方案 B: 把静态编译的 python/node 放到"
  say "    /data/adb/daidai-panel/bin/"
  say "    它会在面板 PATH 最前面被发现"
fi
say "============================================"
