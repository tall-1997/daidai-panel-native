#!/system/bin/sh
##########################################################################
# 呆呆面板 Magisk 模块 - 快捷操作脚本（停止 / 启动 toggle + 状态摘要）
#
# 点击管理器卡片上的「运行 / Action」按钮触发。
#
# 调用约定（Magisk v26+ / KernelSU / APatch 一致，别指望能改）：
#   无参数、单次执行、只回显 stdout，不能传参也不能交互。
# 所以「停止」和「启动」只能共用这一个按钮，由脚本自己读当前状态决定本次做什么。
##########################################################################

MODDIR=${0%/*}
PERSIST_DIR=/data/adb/daidai-panel
SERVICE_LOG="$PERSIST_DIR/service.log"
PORTS_CONF="$PERSIST_DIR/ports.conf"
# 跨重启的手动停止开关，与 service.sh / customize.sh / uninstall.sh 用的是同一个路径。
STOP_FLAG="$PERSIST_DIR/stopped"
RURIMA=$MODDIR/system/bin/rurima

# flavor（容器基础系统）：与 customize.sh / service.sh 同一套规则，
# 读不到 / 不认识就回落 alpine。Debian 容器里没有 /bin/ash。
FLAVOR=alpine
if [ -f "$MODDIR/flavor" ]; then
  read -r flavor_raw < "$MODDIR/flavor" 2>/dev/null || true
  case "$flavor_raw" in
    debian*) FLAVOR=debian ;;
    *) FLAVOR=alpine ;;
  esac
fi
CTR_SHELL=/bin/ash
[ "$FLAVOR" = "debian" ] && CTR_SHELL=/bin/bash

rootfs=/data/daidai
[ ! -d "$rootfs" ] && rootfs=/data/local/daidai

SERVER_LOG="$rootfs/app/Dumb-Panel/daidai.log"
TAIL_LINES=60

# 端口配置（有则读，无则默认）
PANEL_PORT=5700
SSH_PORT=22
SSH_USER=root
SSH_PASSWORD=123456
EXTRA_CORS_ORIGINS=""
# shellcheck disable=SC1090
[ -f "$PORTS_CONF" ] && . "$PORTS_CONF" 2>/dev/null

if ! command -v ui_print >/dev/null 2>&1; then
  ui_print() { echo "$1"; }
fi

# ---- 面板进程探测（宿主侧读 /proc，不进容器） ---------------------------
# 容器走的是 chroot（ruri 不传 -u），没有 PID namespace，容器里的进程在宿主 /proc
# 里同样可见。读 /proc 比进容器 pgrep 更快，也不会额外累积 ruri 挂载。
#
# 注意 read 的退出码：/proc/<pid>/cmdline 是 NUL 分隔且【不以换行结尾】的，
# POSIX 规定 read 读到 EOF 而没遇到分隔符时返回非 0 —— 但变量已经赋好值了。
# 这里【绝对不能】写成 `read ... || continue`：那会让下面的 case 永远执行不到，
# 函数恒返回「未运行」，于是动作按钮永远只会去「启动」，停止功能直接失效。
#
# 匹配的是完整路径 /usr/local/bin/daidai-server（不是裸 daidai-server）：
# 这个 argv0 是 Go 常量与两个 shell 共同依赖的契约，也避免误伤自身命令行。
panel_pids() {
  _pids=""
  for proc_dir in /proc/[0-9]*; do
    [ -r "$proc_dir/cmdline" ] || continue
    proc_cmdline=""
    read -r proc_cmdline 2>/dev/null < "$proc_dir/cmdline"
    case "$proc_cmdline" in
      /usr/local/bin/daidai-server*) _pids="$_pids ${proc_dir#/proc/}" ;;
    esac
  done
  _pids=${_pids# }
  [ -n "$_pids" ] || return 1
  printf '%s\n' "$_pids"
}

ui_print "========================================="
ui_print " 呆呆面板 - 运行状态"
ui_print "========================================="
ui_print "- 容器基础系统: ${FLAVOR}"
ui_print "- 端口配置: PANEL=${PANEL_PORT} (绑定 0.0.0.0)  SSH=${SSH_PORT}"
ui_print "- SSH 凭据: 用户=${SSH_USER}  密码=${SSH_PASSWORD}"
ui_print "           ($PORTS_CONF)"
if [ -n "$EXTRA_CORS_ORIGINS" ]; then
  ui_print "- 额外 CORS: $EXTRA_CORS_ORIGINS"
fi

# ---- 本次动作：停止 / 启动 toggle ---------------------------------------
# 判定规则（必须打印出来，否则用户完全猜不到这次点下去会发生什么）：
#   以【进程状态】为准，停止开关只作辅助说明。
#   两者不一致时按用户意图走 —— 面板刚崩、但停止开关不存在，说明它本应在运行，
#   本次就执行启动。
ACTION_PIDS=$(panel_pids)
STOP_FLAG_PRESENT=0
[ -f "$STOP_FLAG" ] && STOP_FLAG_PRESENT=1

ui_print " "
ui_print "--- 本次动作 ---"
ui_print "- 判定规则: 以面板进程状态为准（在跑=停止，没跑=启动），再点一次即可反向操作"

if [ -n "$ACTION_PIDS" ]; then
  # ======== 停止 ========
  ui_print "- 面板正在运行 (PID=$ACTION_PIDS)，本次执行：停止"
  if [ "$STOP_FLAG_PRESENT" = "1" ]; then
    ui_print "  (停止开关本就存在，但进程还在跑——多半是停止后又手动跑过 service.sh；本次仍按停止处理)"
  fi

  # 1. 先写停止开关，再杀进程。顺序不能反：
  #    先杀的话，service.sh fork 的守护子 shell 可能在几秒内就把面板拉回来了。
  #    守护读到这个开关会自退，所以杀完不会再被拉起；重启手机同样不会自动启动。
  mkdir -p "$PERSIST_DIR" 2>/dev/null
  printf '%s\n' "stopped by action.sh at $(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null)" > "$STOP_FLAG" 2>/dev/null

  # 2. 先 TERM 后 KILL。用 kill + 已探到的 PID，不用 `pkill -f daidai-server`：
  #    后者会连带命中任何 cmdline 里含这串字符的进程（包括执行它的 sh -c 本身）。
  for _pid in $ACTION_PIDS; do
    kill -TERM "$_pid" 2>/dev/null
  done
  sleep 2
  ACTION_PIDS_LEFT=$(panel_pids)
  if [ -n "$ACTION_PIDS_LEFT" ]; then
    for _pid in $ACTION_PIDS_LEFT; do
      kill -KILL "$_pid" 2>/dev/null
    done
    sleep 1
  fi

  # 3. 释放休眠抑制 —— 省电正是用户按停止的主要动机。
  echo "noSuspend" > /sys/power/wake_unlock 2>/dev/null
  dumpsys deviceidle enable 2>/dev/null || true

  if [ -n "$(panel_pids)" ]; then
    ui_print "! 面板进程仍在，可能是权限不足；请用 root shell 重试"
  else
    ui_print "- 已停止：面板进程已退出，重启手机也不会自动启动"
  fi
  ui_print "- 存活守护会在 10 秒内读到停止开关并自行退出，不会再把面板拉回来"
  ui_print "- 保留未动：容器内 sshd 仍在跑、ruri 挂载不卸载（停了面板 Web 就没了，SSH 是唯一排障退路）"
  ui_print "- 想启动：再点一次本按钮"
else
  # ======== 启动 ========
  if [ "$STOP_FLAG_PRESENT" = "1" ]; then
    ui_print "- 面板处于已停止状态，本次执行：启动"
  else
    ui_print "- 面板未在运行，且没有停止开关（多半是崩溃或被手动杀掉），本次执行：启动"
  fi

  # 1. 先删停止开关，否则 service.sh 会在早退点直接退出。
  rm -f "$STOP_FLAG" 2>/dev/null

  # 2. 直接重跑 service.sh，不要在这里自己拼 ruri 命令：
  #    模块→容器条件同步、deps 回填、ports.conf 注入、写新的守护代次全在它里面。
  #    重定向到 /dev/null 是必需的 —— service.sh 末尾 fork 的守护子 shell 会继承 fd，
  #    不切断的话管理器会一直等这个管道关闭，动作弹窗看起来就像卡死。
  if [ -f "$MODDIR/service.sh" ]; then
    sh "$MODDIR/service.sh" >/dev/null 2>&1 </dev/null
    sleep 1
    ACTION_PIDS=$(panel_pids)
    if [ -n "$ACTION_PIDS" ]; then
      ui_print "- 已启动 (PID=$ACTION_PIDS)"
      ui_print "- 访问地址: http://127.0.0.1:${PANEL_PORT}"
    else
      ui_print "! 启动后没有探测到面板进程，请看下面的 service.log / daidai.log"
    fi
  else
    ui_print "! 找不到 $MODDIR/service.sh，模块可能未安装完整，请重刷模块 ZIP"
  fi
  ui_print "- 想停止：再点一次本按钮"
fi

# ---- 进程状态（动作执行之后的实际状态） ---------------------------------
ui_print " "
PID=$(panel_pids)
if [ -n "$PID" ]; then
  ui_print "- 状态: 运行中"
  ui_print "- PID : $PID"
else
  ui_print "- 状态: 未运行"
fi

# ---- 端口监听（宿主侧 PANEL_PORT） -------------------------------------
PORT_INFO=$(netstat -ltn 2>/dev/null | grep ":${PANEL_PORT}\b" | head -n2)
if [ -n "$PORT_INFO" ]; then
  ui_print "- 监听端口:"
  echo "$PORT_INFO" | while IFS= read -r line; do
    ui_print "    $line"
  done
else
  ui_print "- 监听端口: 未检测到 (${PANEL_PORT} 未监听)"
fi

ui_print "- 访问地址: http://127.0.0.1:${PANEL_PORT}"
ui_print "- rootfs  : $rootfs"
ui_print "- 数据目录: $rootfs/app/Dumb-Panel"

# ---- 容器运行时自检 ----------------------------------------------------
if [ -x "$RURIMA" ] && [ -d "$rootfs" ]; then
  ui_print " "
  ui_print "--- 容器运行时 ---"
  "$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" -c '
    export DAIDAI_MAGISK_MODULE=1
    export DAIDAI_ANDROID_RUNTIME_BIN_DIR=/data/adb/daidai-panel/bin
    export PATH=/data/adb/daidai-panel/bin/python/bin:/data/adb/daidai-panel/bin/node/bin:/data/adb/daidai-panel/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/app
    for c in python3 node npm git curl bash; do
      p=$(command -v $c 2>/dev/null)
      if [ -n "$p" ]; then
        v=$($c --version 2>&1 | head -n1)
        echo "$c: $p | $v"
      else
        echo "$c: 缺失"
      fi
    done
  ' 2>/dev/null | while IFS= read -r line; do
    ui_print "$line"
  done
fi

# ---- service.log --------------------------------------------------------
ui_print " "
ui_print "--- service.log (最近 ${TAIL_LINES} 行) ---"
if [ -f "$SERVICE_LOG" ]; then
  tail -n "$TAIL_LINES" "$SERVICE_LOG" 2>/dev/null | while IFS= read -r line; do
    ui_print "$line"
  done
else
  ui_print "(暂无 $SERVICE_LOG)"
fi

# ---- daidai.log (容器内后端日志) ----------------------------------------
ui_print " "
ui_print "--- daidai.log (最近 ${TAIL_LINES} 行) ---"
if [ -f "$SERVER_LOG" ]; then
  tail -n "$TAIL_LINES" "$SERVER_LOG" 2>/dev/null | while IFS= read -r line; do
    ui_print "$line"
  done
else
  ui_print "(暂无 $SERVER_LOG)"
fi

ui_print " "
ui_print "========================================="
ui_print " 常用命令 (adb shell / Termux):"
ui_print "   进入容器:"
ui_print "     su -c \"$RURIMA ruri -p -N -S -A $rootfs /bin/bash\""
ui_print "   停止 / 启动面板（与本按钮完全等价的 toggle）:"
ui_print "     su -c \"sh $MODDIR/action.sh\""
ui_print " "
ui_print " 不要再用 \"pkill -f daidai-server\" 停面板："
ui_print " 存活守护最多 60 秒就会把它重新拉起来（刚拉起过则最坏要等 5 分钟）。"
ui_print "========================================="
