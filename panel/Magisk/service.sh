#!/system/bin/sh
##########################################################################
# 呆呆面板 Magisk 模块 - late_start service
#
# 进入容器（Alpine 或 Debian，取决于 $MODDIR/flavor）启动 daidai-server，
# 端口可通过 ports.conf 配置。前端静态资源由 daidai-server 直接托管，不依赖 nginx。
##########################################################################

export PATH=/data/adb/ap/bin:/data/adb/ksu/bin:/data/adb/magisk:$PATH

# rootfs 位置探测
rootfs=/data/daidai
if [ ! -d "$rootfs" ]; then
  rootfs=/data/local/daidai
fi

# 模块目录探测
MODDIR=${MODDIR:-/data/adb/modules/daidai-panel}
[ ! -d "$MODDIR" ] && MODDIR=/data/adb/magisk/modules/daidai-panel
[ ! -d "$MODDIR" ] && MODDIR=/sbin/.magisk/modules/daidai-panel
[ ! -d "$MODDIR" ] && MODDIR=$(dirname "$0")
RURIMA=$MODDIR/system/bin/rurima

# ---- flavor（容器基础系统）----------------------------------------------
# 与 customize.sh 同一套规则：读 $MODDIR/flavor，读不到 / 不认识就回落 alpine。
# Debian 容器里没有 /bin/ash，下面所有进容器的调用都必须用 $CTR_SHELL。
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

PERSIST_DIR=/data/adb/daidai-panel
LOG_FILE="$PERSIST_DIR/service.log"
PORTS_CONF="$PERSIST_DIR/ports.conf"

# ---- 手动停止开关 / 守护代次标记 ----------------------------------------
# 两个文件都必须放在 PERSIST_DIR（宿主侧持久目录）里，理由：
#   1. 它不在 rootfs 内，customize.sh 重装时的 `rm -rf "$rootfs"` 碰不到；
#   2. 容器内也可读写（面板的 Android 运行时就往 /data/adb/daidai-panel/bin 落 Python/Node），
#      所以面板进程自己也能写停止开关；
#   3. 绝不能放 $rootfs/app/Dumb-Panel/ 下 —— 那里的 .updating 每次开机被无条件删除，
#      跨重启的停止状态放在同一个目录迟早被同类清理误伤。
#
# STOP_FLAG      存在 = 用户显式停了面板，本脚本与守护都不许把它拉起来（跨重启保持）。
# WATCHDOG_GEN_FILE  每跑一次 service.sh 就写一个新值，守护每轮比对，值变了就自退。
STOP_FLAG="$PERSIST_DIR/stopped"
WATCHDOG_GEN_FILE="$PERSIST_DIR/watchdog.gen"

mkdir -p "$PERSIST_DIR"

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE" 2>/dev/null
}

# 日志滚动
if [ -f "$LOG_FILE" ]; then
  size=$(stat -c%s "$LOG_FILE" 2>/dev/null || echo 0)
  [ "${size:-0}" -gt 2097152 ] && mv -f "$LOG_FILE" "$LOG_FILE.old" 2>/dev/null
fi

# ---- 端口配置（用户可编辑 ports.conf 自定义） ---------------------------
# 第一次运行时若文件缺失，自动补一份默认值
if [ ! -f "$PORTS_CONF" ]; then
  cat > "$PORTS_CONF" << 'PCONF'
# 呆呆面板端口配置 —— 修改后重启模块生效
PANEL_PORT=5700
SSH_PORT=22
SSH_USER=root
SSH_PASSWORD=123456
PCONF
fi

PANEL_PORT=5700
SSH_PORT=22
SSH_USER=root
SSH_PASSWORD=123456
EXTRA_CORS_ORIGINS=""
# shellcheck disable=SC1090
. "$PORTS_CONF" 2>/dev/null || true

# 合法性校验（必须是 1..65535 之间的整数）
validate_port() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}
if ! validate_port "$PANEL_PORT"; then
  log "!! ports.conf 中 PANEL_PORT='$PANEL_PORT' 非法，回退为 5700"
  PANEL_PORT=5700
fi
if ! validate_port "$SSH_PORT"; then
  log "!! ports.conf 中 SSH_PORT='$SSH_PORT' 非法，回退为 22"
  SSH_PORT=22
fi

log "========================================="
log "呆呆面板模块启动 (MODDIR=$MODDIR, rootfs=$rootfs, flavor=$FLAVOR, shell=$CTR_SHELL)"
log "端口: PANEL_PORT=$PANEL_PORT (绑定 0.0.0.0), SSH_PORT=$SSH_PORT (来源: $PORTS_CONF)"
log "SSH 凭据: 用户=$SSH_USER"
if [ -n "$EXTRA_CORS_ORIGINS" ]; then
  log "额外 CORS 来源: $EXTRA_CORS_ORIGINS"
fi
log "========================================="

# 注意：阻止休眠（wake_lock / deviceidle）已经挪到下面「手动停止开关」判断之后。
# 停止状态下不该继续阻止手机休眠 —— 省电正是用户按停止按钮的主要动机。

# 等网络就绪（尽量，失败也不阻塞）
for i in 1 2 3 4 5; do
  if busybox nslookup m.baidu.com >/dev/null 2>&1; then
    log "网络已就绪"
    break
  fi
  sleep 5
done

if [ ! -f "$RURIMA" ]; then
  log "!! 找不到 rurima 二进制: $RURIMA"
  exit 1
fi

chmod +x "$RURIMA" 2>/dev/null

if [ ! -d "$rootfs" ]; then
  log "!! 找不到 rootfs: $rootfs，模块可能未完成安装，请重装"
  exit 1
fi

# KernelSU 下 /data 可能以 ro 挂载，确保可写
if [ -d "/data/adb/ksu" ]; then
  mount -o remount,rw /data 2>/dev/null
fi

# ---- 把模块里的前端和 daidai-server 同步进容器 ---------------------------
# 这里【不能】无条件覆盖。面板支持在面板内在线升级（只换 daidai-server / ddp / web），
# 升级时会同时写模块目录和容器内路径；但 KernelSU 等场景下 /data 可能只读，
# 模块目录写不进去，此时容器内是新版、模块里还是旧版 —— 无条件 cp 会在下一次开机
# 把用户刚升上去的版本悄悄回滚掉。
#
# 规则：只有模块里的文件确实比容器里的新（或容器里根本没有）才同步。
# 真正刷入新模块 zip 时 Magisk 会重写这些文件，mtime 变新，同步照常发生。
file_needs_sync() {
  # 目标不存在：必须同步。
  [ -e "$2" ] || return 0
  [ "$1" -nt "$2" ] 2>/dev/null
  case "$?" in
    0) return 0 ;;  # 模块里的确实更新
    1) return 1 ;;  # 容器里的更新或同龄，保留容器里的
    *) return 0 ;;  # 当前 shell 不支持 -nt：回落成无条件同步。
                    # 宁可丢掉一次在线升级，也不能让刷入新模块后同步不进容器。
  esac
}

mkdir -p $rootfs/app/web $rootfs/app/Dumb-Panel $rootfs/usr/local/bin

# 清理残留的在线升级哨兵。开机意味着上一次升级窗口一定已经结束；
# 若升级途中掉电或重启，哨兵会永久留在盘上，让下面的存活守护再也不敢接管。
rm -f "$rootfs/app/Dumb-Panel/.updating" 2>/dev/null

# web 是整个目录，用 index.html 当哨兵判断新旧
if file_needs_sync "$MODDIR/web/index.html" "$rootfs/app/web/index.html"; then
  cp -rf $MODDIR/web/* $rootfs/app/web/ 2>/dev/null
  log "已从模块同步前端资源"
else
  log "容器内前端不早于模块内版本，保留容器内版本（面板在线升级的结果）"
fi

if file_needs_sync "$MODDIR/system/bin/daidai-server" "$rootfs/usr/local/bin/daidai-server"; then
  cp -f  $MODDIR/system/bin/daidai-server $rootfs/usr/local/bin/daidai-server 2>/dev/null
  log "已从模块同步 daidai-server"
else
  log "容器内 daidai-server 不早于模块内版本，保留容器内版本（面板在线升级的结果）"
fi
chmod 755 $rootfs/usr/local/bin/daidai-server 2>/dev/null

# 恢复持久化的依赖目录（容器 overlayfs 重启后可能丢失写入层）
DEPS_PERSIST="$PERSIST_DIR/deps-snapshot"
if [ -d "$DEPS_PERSIST" ]; then
  mkdir -p $rootfs/app/Dumb-Panel/deps
  cp -rf "$DEPS_PERSIST/." $rootfs/app/Dumb-Panel/deps/ 2>/dev/null
  log "已从持久化快照恢复 deps 目录"
fi

if [ -f $MODDIR/system/bin/ddp ]; then
  if file_needs_sync "$MODDIR/system/bin/ddp" "$rootfs/usr/local/bin/ddp"; then
    cp -f  $MODDIR/system/bin/ddp $rootfs/usr/local/bin/ddp 2>/dev/null
  fi
  chmod 755 $rootfs/usr/local/bin/ddp 2>/dev/null
fi

cp -f $MODDIR/module.prop $rootfs/app/module.prop 2>/dev/null

# 把持久化的 ports.conf 同步进容器，容器启动脚本直接 source
mkdir -p $rootfs/tmp
cp -f "$PORTS_CONF" "$rootfs/tmp/ports.conf" 2>/dev/null

# ---- 手动停止开关：早退点 ------------------------------------------------
# 【位置很关键，不要往上挪】必须在上面的「模块→容器条件同步 + deps 回填」之后、
# 下面的「拉起容器」之前。
#
# 放太靠前的后果：用户在停止状态下刷入新模块 zip 再重启，新的 daidai-server / web
# 根本同步不进容器；之后点动作按钮「启动」，跑起来的还是旧版本，
# 表现成「刷了新版但面板里版本号没变」，几乎无法自查。
#
# 反过来，早退必须发生在拉起容器【之前】：停止就是停止，不能进容器再起一次面板。
if [ -f "$STOP_FLAG" ]; then
  log "检测到停止开关 $STOP_FLAG，本次不启动面板"
  log "   模块内的面板程序与前端已同步进容器；到模块管理器点动作按钮即可启动"
  # 停止状态下释放休眠抑制。开机场景下本来就还没申请（上面那两行已挪到这里之后），
  # 这里再写一次是防御性的：用户手动重跑 service.sh 时也能保证不留下悬空的 wake_lock。
  echo "noSuspend" > /sys/power/wake_unlock 2>/dev/null
  dumpsys deviceidle enable 2>/dev/null || true
  exit 0
fi

# 到这里才是真的要把面板跑起来，先阻止手机休眠。
echo "noSuspend" > /sys/power/wake_lock 2>/dev/null
dumpsys deviceidle disable 2>/dev/null || true

# ---- 生成容器启动脚本（全字面 heredoc，变量由容器内 . /tmp/ports.conf 注入） ----
STARTUP=$rootfs/tmp/daidai-startup.sh

# shebang 单独写：容器 shell 随 flavor 变（Alpine=/bin/ash，Debian=/bin/bash），
# 不能烤死在 heredoc 里。脚本正文对两个 flavor 完全一致，只用 POSIX 语法。
printf '#!%s\n' "$CTR_SHELL" > "$STARTUP"
cat >> "$STARTUP" << 'CONTAINER_EOF'
# 默认值 + 用户 ports.conf 覆盖（同文件已由宿主 service.sh 校验过合法性）
PANEL_PORT=5700
SSH_PORT=22
SSH_USER=root
SSH_PASSWORD=123456
EXTRA_CORS_ORIGINS=""
[ -f /tmp/ports.conf ] && . /tmp/ports.conf

export DAIDAI_DIR=/app/Dumb-Panel
export LANG=C.UTF-8
export HOME=/root
export SHELL=/bin/bash
export DAIDAI_MAGISK_MODULE=1
# 模块外壳（本脚本 + customize.sh + action.sh + rootfs 结构）的版本号。
# 面板的在线升级只替换 daidai-server / ddp / web，覆盖不到外壳。
#
# 规则（Go 侧是两个常量，不要再当成一个）：
#   - 每改一次 Magisk/*.sh 或 rootfs 结构，这个数字加一，
#     并同步 Go 里的 currentMagiskShellVersion（magisk_assets_test.go 会静态断言两者一致）。
#   - Go 里的 requiredMagiskShellVersion 是「在线升级放行的最低外壳版本」，
#     只有当新面板【无法】在旧外壳上运行时才提 —— 提了就意味着所有老用户
#     必须先重刷一次模块 zip 才能继续在面板内一键升级。
#
# v2（v3.0.4）新增：停止开关 + 守护代次标记，面板可以被手动停止且跨重启保持。
# v3（v3.0.5）改动 customize.sh：Debian 容器 DNS 改为多源回退（宿主 net.dns* + 公共 DNS +
#   options single-request-reopen）、apt 加固（Sandbox::User / 重试 / 超时 / ForceIPv4）、
#   镜像源四级回退，并在装依赖前加了 root / _apt 双身份的 DNS 判别探测。
#   这些只在刷 ZIP 时执行，所以 requiredMagiskShellVersion 保持 1，在线升级照常放行。
export DAIDAI_MAGISK_SHELL_VERSION=3
export DAIDAI_ANDROID_RUNTIME_BIN_DIR=/data/adb/daidai-panel/bin
export PATH=/data/adb/daidai-panel/bin/python/bin:/data/adb/daidai-panel/bin/node/bin:/data/adb/daidai-panel/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/app
export NODE_PATH=/usr/local/lib/node_modules

mkdir -p $DAIDAI_DIR/scripts $DAIDAI_DIR/logs $DAIDAI_DIR/deps/nodejs $DAIDAI_DIR/deps/python $DAIDAI_DIR/backups
chmod 777 $DAIDAI_DIR

# Python 虚拟环境（第一次进入时创建）
# 模块版当前通常只有一个系统 python3，不保证真的同时有 3.10 / 3.11 / 3.12。
# 这里必须用容器里真实 python3 小版本决定托管环境目录，不能再硬编码 3.12，
# 否则当 Alpine 里的 python3 实际是 3.11 时，就会出现
# “目录叫 3.12，但里面实际是 3.11 venv”，后端版本探测会直接判定 Python 3.12 不可用。
PY_MINOR=""
if command -v python3 >/dev/null 2>&1; then
  PY_MINOR=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')" 2>/dev/null || true)
fi
case "$PY_MINOR" in
  3.10|3.11|3.12)
    export DAIDAI_PYTHON_VERSION="$PY_MINOR"
    if [ ! -d "$DAIDAI_DIR/deps/python/$PY_MINOR" ]; then
      python3 -m venv "$DAIDAI_DIR/deps/python/$PY_MINOR" 2>/dev/null || true
    fi
    ;;
esac

# 按配置写入 config.yaml（每次启动都覆盖，保证端口与 ports.conf 一致）
# 后端用 net.Listen(":PORT") 绑定 0.0.0.0，穿透/局域网直连均可；
# CORS 列表只影响浏览器跨域检查，"同源请求"已由中间件自动放行。
cat > $DAIDAI_DIR/config.yaml << YAML
server:
  port: ${PANEL_PORT}
  mode: release
  web_dir: /app/web

database:
  path: /app/Dumb-Panel/daidai.db

jwt:
  secret: ""
  access_token_expire: 480h
  refresh_token_expire: 1440h

data:
  dir: /app/Dumb-Panel
  scripts_dir: /app/Dumb-Panel/scripts
  log_dir: /app/Dumb-Panel/logs

cors:
  origins:
    - http://localhost:${PANEL_PORT}
    - http://127.0.0.1:${PANEL_PORT}
YAML

# 追加 EXTRA_CORS_ORIGINS（穿透 / 反代 / 公网域名场景显式放行）
if [ -n "${EXTRA_CORS_ORIGINS}" ]; then
  echo "${EXTRA_CORS_ORIGINS}" | tr ',;' '\n' | while IFS= read -r origin; do
    # 去首尾空白
    origin=$(echo "$origin" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -z "$origin" ] && continue
    echo "    - ${origin}" >> $DAIDAI_DIR/config.yaml
  done
fi

# ---- SSH: 同步用户名/密码，按 SSH_PORT 更新 sshd_config 并启动 --------
# 每次启动都同步密码，确保 ports.conf 改了密码后重启即生效
if [ -n "${SSH_USER}" ] && [ -n "${SSH_PASSWORD}" ]; then
  if [ "${SSH_USER}" != "root" ]; then
    # busybox(Alpine) 的 adduser 和 Debian 的 adduser 参数完全不兼容，
    # 前者不认 --disabled-password，后者不认 -D。依次尝试，最后用 useradd 兜底。
    if ! id "${SSH_USER}" >/dev/null 2>&1; then
      adduser -D -s /bin/bash "${SSH_USER}" 2>/dev/null || \
        adduser --disabled-password --gecos "" --shell /bin/bash "${SSH_USER}" 2>/dev/null || \
        useradd -m -s /bin/bash "${SSH_USER}" 2>/dev/null || true
    fi
  fi
  echo "${SSH_USER}:${SSH_PASSWORD}" | chpasswd 2>/dev/null
fi

if [ -f /etc/ssh/sshd_config ]; then
  # 清除已有 Port 行（包括注释的），再追加当前端口
  sed -i -E '/^[#[:space:]]*Port[[:space:]]+/d' /etc/ssh/sshd_config
  echo "Port ${SSH_PORT}" >> /etc/ssh/sshd_config
  # 没有 host key 的话先生成一下
  [ -f /etc/ssh/ssh_host_rsa_key ] || ssh-keygen -A >/dev/null 2>&1
  # 启动 sshd（已在跑就跳过）
  if ! pgrep -x sshd >/dev/null 2>&1; then
    mkdir -p /run/sshd
    /usr/sbin/sshd >/dev/null 2>&1 || true
  fi
fi

# 避免重复拉起 daidai-server
if pgrep -f /usr/local/bin/daidai-server >/dev/null 2>&1; then
  echo "daidai-server 已在运行" >> $DAIDAI_DIR/service.log
  exit 0
fi

cd $DAIDAI_DIR
nohup /usr/local/bin/daidai-server > $DAIDAI_DIR/daidai.log 2>&1 &
echo "daidai-server 已拉起 PID=$! (port=${PANEL_PORT})" >> $DAIDAI_DIR/service.log
exit 0
CONTAINER_EOF
chmod +x "$STARTUP" 2>/dev/null

log "进入容器启动 daidai-server (flavor=$FLAVOR, panel=$PANEL_PORT, ssh=$SSH_PORT)..."

"$RURIMA" ruri -p -N -S -A $rootfs "$CTR_SHELL" /tmp/daidai-startup.sh

sleep 2

# 容器内启动后简单验证
if "$RURIMA" ruri -p -N -S -A $rootfs "$CTR_SHELL" -c "pgrep -f /usr/local/bin/daidai-server >/dev/null 2>&1"; then
  log "面板启动成功，访问 http://127.0.0.1:${PANEL_PORT}"
else
  log "!! 面板启动失败，查看 $rootfs/app/Dumb-Panel/daidai.log"
fi

# ---- 守护代次标记 ----------------------------------------------------------
# 必须在 fork 守护【之前】写。
#
# 为什么需要：本脚本对守护子 shell 没有任何去重手段（只对 daidai-server 做了 pgrep 去重），
# 而 action.sh / 文档都在教用户重跑 service.sh —— 跑几次就有几个 while 循环，
# 它们彼此看不见对方的 revive_cooldown，会各自进容器重跑 daidai-startup.sh
# （覆盖 config.yaml、把 SSH 密码改回 ports.conf 的值、持续累积 ruri 挂载）。
#
# 做法：每次 service.sh 启动都写一个新值，守护把它读进变量并逐轮比对，
# 值变了就自退 —— 新守护上台，旧守护自动下台。这也是「停止/启动 toggle」能工作的前提。
#
# 顺带的好处：卸载脚本删掉这个文件（或整个 PERSIST_DIR）后，守护读到空值同样会自退，
# 不会再对着已被 rm -rf 的 rootfs 反复 ruri。
WATCHDOG_GEN="$(date '+%s' 2>/dev/null)-$$"
printf '%s\n' "$WATCHDOG_GEN" > "$WATCHDOG_GEN_FILE" 2>/dev/null
log "守护代次: $WATCHDOG_GEN"

# ---- 后台守护循环 ----------------------------------------------------------
# 一条循环干两件事：
#   1. 存活守护：模块版没有 supervisor，面板进程一旦退出（崩溃、OOM、在线升级失败）
#      就得重启手机才能回来。这里每 60 秒探一次，不在就重新拉起。
#   2. deps 快照：容器 overlayfs 的写入层在重启后可能丢失，每 10 分钟把 deps 目录
#      同步到宿主 /data/adb/daidai-panel/deps-snapshot/，下次开机由上面的逻辑回填。
(
  DEPS_PERSIST="$PERSIST_DIR/deps-snapshot"
  DEPS_CONTAINER="$rootfs/app/Dumb-Panel/deps"
  # 面板在线升级期间会写这个哨兵，替换窗口内守护不插手，
  # 免得在「旧进程已退出、新进程还没起来」的空档里把旧版本又拉起来。
  UPDATING_FLAG="$rootfs/app/Dumb-Panel/.updating"

  # 直接读 /proc 判断进程是否存活，不依赖 pgrep / busybox 是否可用。
  # 容器走的是 chroot（ruri 不传 -u），没有 PID namespace，
  # 容器里的进程在宿主 /proc 里同样可见。
  #
  # 注意 read 的退出码：/proc/<pid>/cmdline 是 NUL 分隔且【不以换行结尾】的，
  # POSIX 规定 read 在读到 EOF 而没遇到分隔符时返回非 0 —— 但变量已经赋好值了。
  # 所以这里【绝对不能】写成 `read ... || continue`：那会让下面的 case 永远执行不到，
  # 函数恒返回「未运行」，守护就会每轮无条件重跑启动脚本
  # （覆盖 config.yaml、把 SSH 密码改回默认值、累积 ruri 挂载）。
  panel_is_running() {
    for proc_dir in /proc/[0-9]*; do
      [ -r "$proc_dir/cmdline" ] || continue
      proc_cmdline=""
      read -r proc_cmdline 2>/dev/null < "$proc_dir/cmdline"
      case "$proc_cmdline" in
        /usr/local/bin/daidai-server*) return 0 ;;
      esac
    done
    return 1
  }

  # 守护自退判定。命中任一条就结束本守护，绝不再拉起面板：
  #   1. 停止开关：用户在管理器点了动作按钮 / 在面板里点了「停止面板服务」。
  #      守护不退的话，pkill 掉的面板最多 60 秒就被拉回来（刚 revive 过则最坏 5 个周期）。
  #   2. 守护代次变了：说明 service.sh 又跑了一次、有新守护上台，旧的必须下台。
  #      文件不存在（模块被卸载、PERSIST_DIR 被删）时读到空值，同样判为「变了」。
  #
  # 注意 read 的退出码陷阱（与下面的 panel_is_running 同源）：
  # 这里【不判】read 的返回值，先清空变量再读，只看内容。
  watchdog_should_exit() {
    if [ -f "$STOP_FLAG" ]; then
      log "守护退出：检测到停止开关 $STOP_FLAG"
      return 0
    fi
    watchdog_gen_now=""
    [ -f "$WATCHDOG_GEN_FILE" ] && read -r watchdog_gen_now 2>/dev/null < "$WATCHDOG_GEN_FILE"
    if [ "$watchdog_gen_now" != "$WATCHDOG_GEN" ]; then
      log "守护退出：代次已变更（本代=$WATCHDOG_GEN，当前=${watchdog_gen_now:-<空>}）"
      return 0
    fi
    return 1
  }

  tick=0
  revive_cooldown=0
  while true; do
    watchdog_should_exit && exit 0

    # 分片睡眠：把 60 秒切成 6 段，每段结束都看一次自退条件。
    # 目的是把「用户点停止 / 开始刷入新模块 zip」到守护真正退出的延迟从最坏 60 秒
    # 压到 10 秒 —— 刷 zip 期间 customize.sh 马上要 rm -rf rootfs，
    # 守护还在抢同一个 rootfs 的窗口越短越好。
    slept=0
    while [ "$slept" -lt 60 ]; do
      sleep 10
      slept=$((slept + 10))
      watchdog_should_exit && exit 0
    done

    tick=$((tick + 1))
    [ "$revive_cooldown" -gt 0 ] && revive_cooldown=$((revive_cooldown - 1))

    # ---- 存活守护 ----
    # 冷却是为了避免面板一直起不来时每分钟都进一次容器：
    # ruri 的挂载落在宿主全局 mount namespace，高频重入没有好处。
    if [ ! -f "$UPDATING_FLAG" ] && [ "$revive_cooldown" -le 0 ]; then
      if ! panel_is_running; then
        log "!! 面板进程不在，尝试重新拉起"
        "$RURIMA" ruri -p -N -S -A $rootfs "$CTR_SHELL" /tmp/daidai-startup.sh
        revive_cooldown=5
      fi
    fi

    # ---- 每 10 分钟快照一次 deps ----
    if [ "$tick" -ge 10 ]; then
      tick=0
      if [ -d "$DEPS_CONTAINER" ] && [ "$(ls -A "$DEPS_CONTAINER" 2>/dev/null)" ]; then
        mkdir -p "$DEPS_PERSIST"
        rsync -a --delete "$DEPS_CONTAINER/" "$DEPS_PERSIST/" 2>/dev/null || \
          cp -rf "$DEPS_CONTAINER/." "$DEPS_PERSIST/" 2>/dev/null
      fi
    fi
  done
) &
