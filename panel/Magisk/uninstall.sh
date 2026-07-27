#!/system/bin/sh
##########################################################################
# 呆呆面板 Magisk 模块卸载脚本
#
# 默认会清理：
#   - 运行中的 daidai-server 进程
#   - Alpine rootfs (/data/daidai 或 /data/local/daidai)
#   - 持久化目录 /data/adb/daidai-panel
#
# 如需保留数据以便重装后继续用，卸载前先：
#   su -c "touch /data/adb/daidai-panel/.keep_on_uninstall"
##########################################################################

PERSIST_DIR=/data/adb/daidai-panel
KEEP_FLAG="$PERSIST_DIR/.keep_on_uninstall"
LOG_TAG="daidai-panel-uninstall"

_log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >&2
  log -t "$LOG_TAG" "$1" 2>/dev/null
}

_log "卸载脚本开始执行"

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

# 3. 清理历史版本可能写入的其它路径
rm -f /system/etc/init.d/99daidai 2>/dev/null
rm -f /data/adb/service.d/daidai-panel.sh 2>/dev/null
rm -f /data/local/tmp/daidai-panel.* 2>/dev/null

_log "卸载完成；重启后模块本体目录会被 Magisk 自动清除"
