#!/system/bin/sh
##########################################################################
# 呆呆面板 Magisk 模块卸载脚本
#
# 默认会清理：
#   - 运行中的 daidai-server 进程
#   - 容器 rootfs (/data/daidai 或 /data/local/daidai)，Alpine / Debian 路径相同
#   - 持久化目录 /data/adb/daidai-panel
#
# 如需保留数据以便重装后继续用，卸载前先：
#   su -c "touch /data/adb/daidai-panel/.keep_on_uninstall"
##########################################################################

PERSIST_DIR=/data/adb/daidai-panel
KEEP_FLAG="$PERSIST_DIR/.keep_on_uninstall"
STOP_FLAG="$PERSIST_DIR/stopped"
WATCHDOG_GEN_FILE="$PERSIST_DIR/watchdog.gen"
LOG_TAG="daidai-panel-uninstall"

_log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >&2
  log -t "$LOG_TAG" "$1" 2>/dev/null
}

_log "卸载脚本开始执行"

# 0. 先让 service.sh fork 的存活守护自退。
#    守护的 argv 继承自 service.sh，`pkill -f daidai-server` 根本打不到它；
#    不收口的话，卸载之后守护还会活到下次重启，对着已被 rm -rf 的 rootfs 反复 ruri。
mkdir -p "$PERSIST_DIR" 2>/dev/null
printf '%s\n' "stopped by uninstall.sh at $(date '+%Y-%m-%d %H:%M:%S')" > "$STOP_FLAG" 2>/dev/null

# 1. 停止面板进程
pkill -f "daidai-server" 2>/dev/null
sleep 1
pkill -9 -f "daidai-server" 2>/dev/null

# 2. 清理 rootfs（除非保留）
if [ -f "$KEEP_FLAG" ]; then
  _log "检测到保留标记 $KEEP_FLAG，跳过 rootfs / 持久化目录清理"
  _log "如需彻底删除：su -c \"rm -rf /data/daidai /data/local/daidai $PERSIST_DIR\""
else
  for rfs in /data/daidai /data/local/daidai; do
    if [ -d "$rfs" ]; then
      _log "清理 rootfs: $rfs"
      rm -rf "$rfs"
    fi
  done
  if [ -d "$PERSIST_DIR" ]; then
    _log "清理持久化目录: $PERSIST_DIR"
    rm -rf "$PERSIST_DIR"
  fi
fi

# 3. 无条件收口 —— 【必须放在上面 KEEP_FLAG 判断之外】。
#
#  - 删守护代次标记：守护每轮拿它跟自己启动时记下的值比对，读到空值就自退。
#    保留数据卸载（.keep_on_uninstall）时 PERSIST_DIR 整个不删，
#    不显式删这个文件的话守护会一直活到下次重启。
#  - 删停止开关：上面第 0 步刚写过它，这里必须删掉。
#    否则「停止 → 保留数据卸载 → 重装」会得到一个永远起不来的新模块：
#    新 service.sh 每次开机都在早退点退出，而用户完全看不出线索。
#    「刚装完/刚卸载完的模块必须能起来」优先级高于「记住我停过」。
rm -f "$WATCHDOG_GEN_FILE" 2>/dev/null
rm -f "$STOP_FLAG" 2>/dev/null

# 4. 清理历史版本可能写入的其它路径
rm -f /system/etc/init.d/99daidai 2>/dev/null
rm -f /data/adb/service.d/daidai-panel.sh 2>/dev/null
rm -f /data/local/tmp/daidai-panel.* 2>/dev/null

_log "卸载完成；重启后模块本体目录会被 Magisk 自动清除"
