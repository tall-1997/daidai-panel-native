#!/bin/sh
# daidai-panel 容器入口脚本
# 兼容飞牛 OS / 群晖 / 绿联 / unRAID 等第三方 NAS 部署场景。

set -e

# 第一件事先装一个「收到就退」的 trap：这时面板还没起，没有要收尾的东西。
# 本脚本在容器里是 PID 1，没装 trap 的 PID 1 收到的 SIGTERM 会被内核直接丢掉 ——
# 下面 PUID 的 chown -R、find 扫描历史库这些可能跑上好一阵，这期间 docker stop 会白等满 10 秒才 SIGKILL。
# 注意 trap 要等手上那条前台命令跑完才执行（shell 的通用语义），但至少不再丢信号。
# 面板起来之前会换成下面真正的 shutdown。
trap 'exit 143' TERM INT

DATA_DIR=${DATA_DIR:-/app/Dumb-Panel}
SERVER_PID_FILE="${DATA_DIR}/run/daidai-server.pid"
PANEL_PORT=${PANEL_PORT:-5700}
APP_CONFIG_FILE=${APP_CONFIG_FILE:-/app/config.yaml}

# 时间戳格式与 Go 标准库 log 的默认格式一致（2006/01/02 15:04:05），和面板日志放在一起能直接对时间线。
log() {
  printf '%s [entrypoint] %s\n' "$(date '+%Y/%m/%d %H:%M:%S')" "$*"
}

fail() {
  printf '%s [entrypoint][ERROR] %s\n' "$(date '+%Y/%m/%d %H:%M:%S')" "$*" >&2
  exit 1
}

# --- 数据目录初始化 -----------------------------------------------------------
mkdir -p \
  "${DATA_DIR}/scripts" \
  "${DATA_DIR}/logs" \
  "${DATA_DIR}/backups" \
  "${DATA_DIR}/run" \
  "${DATA_DIR}/deps/nodejs" \
  "${DATA_DIR}/deps/python"
mkdir -p /tmp
chmod 1777 /tmp

# --- 青龙脚本兼容层（/ql） ---------------------------------------------------
# 青龙生态的脚本基本都写成 QL_DIR=${QL_DIR:-"/ql"}，再按 $QL_DIR/shell、
# $QL_DIR/data/repo 拼后续路径。容器里没有 /ql 时，这些脚本第一步
# `touch "$dir_shell/env.sh"` 就直接报错。这里把青龙布局软链到面板真实目录。
#
# 【为什么 entrypoint 必须也做一遍，不能只靠 Go 侧 EnsureQingLongCompatLayout】
# 配了 PUID 之后 daidai-server 是降权跑的，它去 mkdir /ql 必然 EACCES 且只留一行日志。
# 这一段跑在降权之前、以 root 身份执行，是容器里唯一能建出 /ql 的时机。
# Go 侧那一份仍然保留：用户自定义了 scripts_dir / log_dir 时，
# 这里按默认布局猜的目标会不对，Go 侧会在启动时把指错的软链重建过去。
#
# 【全程 || true】只读根、异常挂载下建不出来，后果只是青龙脚本跑不了；
# 裸写会被开头的 set -e 直接带出容器，用户看到的是「容器起不来、日志一行没有」。
link_ql_path() {
  # $1=软链位置 $2=目标目录
  # 已经是真实目录/文件（例如用户把自己的青龙 /ql 挂进了容器）就原样保留：
  # 对一个真实目录执行 ln -sfn 会把链接建到那个目录**里面**去，反而制造脏数据。
  if [ -e "$1" ] && [ ! -L "$1" ]; then
    return 0
  fi
  ln -sfn "$2" "$1" 2>/dev/null || true
}

# 🔴 记下 /ql 里本来是不是就有东西。下面 PUID 那段的 chown 要靠它做判据：
# 用户把宿主机上真实的青龙 /ql 挂进容器时（就是上面 link_ql_path 注释设想的场景），
# 去动它的属主就是在改人家整棵青龙数据（repo/scripts/log/config/db），
# 而我们这边因为 `|| true` 一个字都不会说。
#
# 判据必须是「存在**且非空**」而不是「存在」：容器编排、tmpfs 挂载点、
# 甚至 docker 为一个卷预创建的空目录，都会让 /ql 提前存在但里面什么都没有 ——
# 那种情况下 chown 我们自己刚建的那几个节点完全无害，不该被误伤跳过。
if [ -d /ql ] && [ -n "$(ls -A /ql 2>/dev/null)" ]; then
  QL_PREEXISTING=1
else
  QL_PREEXISTING=0
fi

if mkdir -p /ql/shell /ql/data 2>/dev/null; then
  link_ql_path /ql/data/repo "${DATA_DIR}/scripts"
  link_ql_path /ql/data/scripts "${DATA_DIR}/scripts"
  link_ql_path /ql/scripts "${DATA_DIR}/scripts"
  link_ql_path /ql/data/log "${DATA_DIR}/logs"
  link_ql_path /ql/log "${DATA_DIR}/logs"
  # config.sh 就在数据目录根下，所以 config 指的是 DATA_DIR 本身而不是它的子目录。
  link_ql_path /ql/data/config "${DATA_DIR}"
  link_ql_path /ql/config "${DATA_DIR}"
  link_ql_path /ql/data/deps "${DATA_DIR}/deps"
  # 空占位文件：脚本会 source 它，塞任何内容都可能改变脚本行为；已存在就一个字节都不碰。
  [ -f /ql/shell/env.sh ] || touch /ql/shell/env.sh 2>/dev/null || true
else
  log "创建 /ql 青龙兼容目录失败（只读根或权限不足），青龙生态脚本可能无法运行，其余功能不受影响"
fi

# --- PUID/PGID 支持（LinuxServer.io 风格，opt-in） ---------------------------
# 飞牛 OS / 群晖等 NAS 用户通常需要让容器以宿主机用户跑，方便 SMB/NFS 共享。
# 仅当显式传入 PUID 才切换用户；保持对历史部署（默认 root）的兼容。
#
# 这一段在 v3.0.7 重写，修的是三个会让 PUID 直接不可用的问题：
#   1. 降权用户没有可写的 HOME。原来建用户用的是 adduser -D -H / useradd -M，
#      两个参数都是「不创建家目录」，但 /etc/passwd 里的家目录字段照写 /home/daidai。
#      而 npm 的 cache（$HOME/.npm）、.npmrc，pip 的 pip.conf、pip --user 落点
#      全都只认 HOME —— /home 是镜像层的 root:root 0755，daidai 建不出子目录，
#      装依赖必然 EACCES: mkdir '/home/daidai'。这就是用户报障的原始现象。
#   2. UID/GID 撞车会把容器直接带崩。原来的 addgroup/groupadd、adduser/useradd
#      在 GID/UID 已被占用时全都失败，末尾又没有兜底，set -e 下整个脚本退出。
#      Debian 镜像基于 node:24-bookworm-slim，自带 uid/gid 1000 的 node 用户，
#      而 compose 注释里给的示例恰好就是最常见的 PUID=1000 / PGID=1000。
#   3. 只设 PGID 不设 PUID 时 TARGET_UID 取到 0，造出一个 uid=0 的假 daidai：
#      看起来降了权，实际仍是 root，chown 出来的属主也还是 root。
RUN_AS_USER=""
RUN_AS_SPEC=""
DAIDAI_HOME=""
if [ -n "${PUID}" ] || [ -n "${PGID}" ]; then
  TARGET_UID=${PUID:-0}
  TARGET_GID=${PGID:-${TARGET_UID}}

  if [ "${TARGET_UID}" = "0" ]; then
    log "PUID 未设置或为 0（当前 PUID='${PUID}' PGID='${PGID}'），等价于以 root 运行，已跳过降权"
    log "  需要降权请同时设置 PUID 与 PGID，例如 PUID=1000 PGID=1000（宿主机执行 id 查看真实取值）"
  elif ! command -v su-exec >/dev/null 2>&1 && ! command -v gosu >/dev/null 2>&1; then
    log "未找到 su-exec/gosu，PUID/PGID 设置已忽略（继续以 root 运行）"
  else
    DAIDAI_HOME="${DATA_DIR}/.home"

    # ---- 组：GID 已被占用就直接复用那个组 ---------------------------------
    # 复用而不是新建，是因为「GID 已存在」在 NAS 上是常态（群晖的 users=100、
    # Debian 镜像自带的 node=1000），而降权真正需要的只是「进程的 gid 等于 TARGET_GID」，
    # 组叫什么名字无所谓。
    TARGET_GROUP=$(getent group "${TARGET_GID}" 2>/dev/null | cut -d: -f1)
    if [ -z "${TARGET_GROUP}" ]; then
      if getent group daidai >/dev/null 2>&1; then
        # daidai 组已存在但 GID 不是这次要的：用户改了 PGID 之后只做了 docker restart，
        # 容器可写层还在，上一次建的组会原样保留。就地改 GID，不要重建。
        groupmod -g "${TARGET_GID}" daidai >/dev/null 2>&1 || true
      elif command -v groupadd >/dev/null 2>&1; then
        groupadd -g "${TARGET_GID}" daidai >/dev/null 2>&1 || true
      else
        addgroup -g "${TARGET_GID}" daidai >/dev/null 2>&1 || true
      fi
      TARGET_GROUP=$(getent group "${TARGET_GID}" 2>/dev/null | cut -d: -f1)
    fi

    # ---- 用户：UID 已被占用同样直接复用 -----------------------------------
    TARGET_USER=$(getent passwd "${TARGET_UID}" 2>/dev/null | cut -d: -f1)
    if [ -z "${TARGET_USER}" ]; then
      if id -u daidai >/dev/null 2>&1; then
        usermod -u "${TARGET_UID}" -g "${TARGET_GID}" -d "${DAIDAI_HOME}" daidai >/dev/null 2>&1 || true
      elif command -v useradd >/dev/null 2>&1; then
        useradd -M -d "${DAIDAI_HOME}" -u "${TARGET_UID}" -g "${TARGET_GID}" -s /sbin/nologin daidai >/dev/null 2>&1 || true
      else
        adduser -D -H -h "${DAIDAI_HOME}" -u "${TARGET_UID}" -G "${TARGET_GROUP:-daidai}" daidai >/dev/null 2>&1 || true
      fi
      TARGET_USER=$(getent passwd "${TARGET_UID}" 2>/dev/null | cut -d: -f1)
    fi

    if [ -z "${TARGET_USER}" ]; then
      # 建不出来也拿不到现成账号：继续以 root 跑，比顶着一个不存在的用户名
      # 让 su-exec 在启动时报错要好，至少面板是可用的。
      log "无法创建或复用 uid=${TARGET_UID} 的用户，PUID/PGID 设置已忽略（继续以 root 运行）"
      DAIDAI_HOME=""
    else
      # ---- 家目录：本次修复的核心 -----------------------------------------
      # 放在数据目录下而不是 /home/daidai，理由有三：
      #   1. 已经被下面那条 chown -R "${DATA_DIR}" 覆盖，不会再漏掉一处属主；
      #   2. 在挂载卷里，容器重建后 npm 缓存与用户改过的镜像源配置都还在；
      #   3. 备份只收集 scripts/ 下带扩展名白名单的脚本文件，这个目录不会被打进备份包。
      #
      # 【必须带 || true】数据目录以 :ro 挂载、或 NFS root_squash 把容器内 root 压成
      # nobody 时，这句会返回 EACCES。裸写会被 set -e 直接带出容器 ——
      # 用户看到的是「容器无限重启、docker logs 一行输出都没有」，
      # 而下面那道可写性预检本来能给出「数据目录不可写 + 三条原因 + 修复命令」。
      # 建不出来不致命：预检会替我们报错并说清怎么办。
      mkdir -p "${DAIDAI_HOME}" 2>/dev/null || true

      log "应用 PUID=${TARGET_UID} PGID=${TARGET_GID}（运行用户 ${TARGET_USER}，HOME=${DAIDAI_HOME}），正在调整数据目录所有权..."
      chown -R "${TARGET_UID}:${TARGET_GID}" "${DATA_DIR}" /tmp 2>/dev/null || true
      # /ql 也要让降权后的 daidai 能写：青龙脚本第一步就是 touch "$QL_DIR/shell/env.sh"，
      # Go 侧还要能重建指错的软链。
      #
      # 🔴 但**绝不能 chown -R /ql**，而且只在 /ql 是我们自己建出来时才动它：
      #   1) 用户可能把宿主机上真实的青龙 /ql 挂进来（link_ql_path 的注释就设想了这个场景）。
      #      对它 chown -R 会递归改写人家整棵青龙数据（repo/scripts/log/config/db）的属主，
      #      青龙容器随后写自己的数据就 EACCES，得手动 chown 回去才能救；而这里带着 `|| true`，
      #      面板日志一个字都不会说。
      #   2) 即使 /ql 是我们建的，里面除 shell、data 外全是软链，busybox 的 chown -R 递归时
      #      会解引用软链，等于把 DATA_DIR 又扫一遍 —— 纯浪费。
      # 所以：非递归、只覆盖面板自己真正要写的那三个真实节点。
      if [ "${QL_PREEXISTING}" = "0" ]; then
        chown "${TARGET_UID}:${TARGET_GID}" /ql /ql/shell /ql/data /ql/shell/env.sh 2>/dev/null || true
      fi
      RUN_AS_USER="${TARGET_USER}"

      # 降权时必须把 gid 一起显式传给 su-exec / gosu，不能只传用户名。
      # 复用现成账号那条路上（例如 PUID=1000 命中 Debian 镜像自带的 node 用户），
      # 两个工具都是按 /etc/passwd 里那个用户的主组取 gid 的 —— 用户填的 PGID 会被
      # 静默丢掉。典型后果：群晖 / OMV 用户填 PUID=1000 PGID=100(users)，
      # 数据目录被 chown 成 1000:100，面板进程却以 gid=1000 跑，
      # 新写出来的文件属组全错，而 PGID 存在的唯一目的就是让属组对。
      # su-exec 与 gosu 都支持 user:group 形式，group 可以是数字。
      RUN_AS_SPEC="${TARGET_USER}:${TARGET_GID}"
    fi
  fi
fi

# --- 数据目录可写性预检 -----------------------------------------------------
WRITE_PROBE="${DATA_DIR}/.daidai-write-probe-$$"
PROBE_CMD="true"
if [ -n "${RUN_AS_USER}" ]; then
  if command -v su-exec >/dev/null 2>&1; then
    PROBE_CMD="su-exec ${RUN_AS_SPEC} touch ${WRITE_PROBE}"
  elif command -v gosu >/dev/null 2>&1; then
    PROBE_CMD="gosu ${RUN_AS_SPEC} touch ${WRITE_PROBE}"
  fi
else
  PROBE_CMD="touch ${WRITE_PROBE}"
fi

if ! sh -c "${PROBE_CMD}" 2>/dev/null; then
  log "数据目录 ${DATA_DIR} 不可写。常见原因："
  log "  1) NAS 上挂载的宿主机目录所有权与容器用户不匹配。"
  log "     在宿主机执行：sudo chown -R \$(id -u):\$(id -g) <挂载点>，或在 compose 里设置 PUID/PGID。"
  log "  2) SELinux/AppArmor 拒绝写入。"
  log "  3) 只读卷挂载（compose 配置中 :ro 标志）。"
  fail "数据目录可写性预检失败，启动中止。"
fi
rm -f "${WRITE_PROBE}" 2>/dev/null || true

# HOME 要单独再探一次：装依赖时 npm 的 cache（$HOME/.npm）、.npmrc 与 pip 的 pip.conf
# 全落在这里，它不可写的表现是「面板能开、一装依赖就 EACCES」，
# 跟上面那种「数据目录整体不可写、面板压根起不来」完全不是一回事，不能靠同一次探测代劳。
if [ -n "${RUN_AS_USER}" ] && [ -n "${DAIDAI_HOME}" ]; then
  HOME_PROBE="${DAIDAI_HOME}/.daidai-write-probe-$$"
  HOME_PROBE_CMD="touch ${HOME_PROBE}"
  if command -v su-exec >/dev/null 2>&1; then
    HOME_PROBE_CMD="su-exec ${RUN_AS_SPEC} touch ${HOME_PROBE}"
  elif command -v gosu >/dev/null 2>&1; then
    HOME_PROBE_CMD="gosu ${RUN_AS_SPEC} touch ${HOME_PROBE}"
  fi
  if sh -c "${HOME_PROBE_CMD}" 2>/dev/null; then
    rm -f "${HOME_PROBE}" 2>/dev/null || true
  else
    log "警告：运行用户 ${RUN_AS_USER} 对 HOME（${DAIDAI_HOME}）没有写权限，面板内安装 Node.js / Python 依赖会失败"
    log "  在宿主机执行：sudo chown -R ${TARGET_UID}:${TARGET_GID} <挂载点>，或去掉 PUID/PGID 改回 root 运行"
  fi
fi

# --- 字符编码 --------------------------------------------------------------
# 与 Dockerfile 的 ENV 对称，双保险：未经 ENV 注入的场景（如部分守护方式）也能拿到 UTF-8 locale，
# 避免任务执行与终端里的中文文件名/输出乱码。C.UTF-8 在 Alpine musl 与 Debian glibc 均内置。
export LANG="${LANG:-C.UTF-8}"
export LC_ALL="${LC_ALL:-C.UTF-8}"

# --- PATH / NODE_PATH ------------------------------------------------------
export NODE_PATH="${DATA_DIR}/deps/nodejs/node_modules"
export DAIDAI_PYTHON_RUNTIME_ROOT="${DAIDAI_PYTHON_RUNTIME_ROOT:-/opt/daidai-python}"
DAIDAI_PYTHON_BIN_PATH=""
DAIDAI_PYTHON_LIB_PATH=""
for py_ver in 3.12 3.11 3.10; do
  py_root="${DAIDAI_PYTHON_RUNTIME_ROOT}/${py_ver}"
  if [ -d "${py_root}/bin" ]; then
    DAIDAI_PYTHON_BIN_PATH="${DAIDAI_PYTHON_BIN_PATH:+${DAIDAI_PYTHON_BIN_PATH}:}${py_root}/bin"
  fi
  if [ -d "${py_root}/lib" ]; then
    DAIDAI_PYTHON_LIB_PATH="${DAIDAI_PYTHON_LIB_PATH:+${DAIDAI_PYTHON_LIB_PATH}:}${py_root}/lib"
  fi
done
DEFAULT_PYTHON_VERSION="${DAIDAI_PYTHON_VERSION:-3.12}"
export PATH="${DATA_DIR}/deps/nodejs/node_modules/.bin:${DATA_DIR}/deps/python/${DEFAULT_PYTHON_VERSION}/bin:${DAIDAI_PYTHON_BIN_PATH:+${DAIDAI_PYTHON_BIN_PATH}:}${PATH}"
if [ -n "${DAIDAI_PYTHON_LIB_PATH}" ]; then
  export LD_LIBRARY_PATH="${DAIDAI_PYTHON_LIB_PATH}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
fi

if [ -d "${DATA_DIR}/deps/python/${DEFAULT_PYTHON_VERSION}" ]; then
  PY_MINOR=$(python3 -c 'import sys;print(f"{sys.version_info.minor}")' 2>/dev/null || echo "")
  if [ -n "${PY_MINOR}" ]; then
    PY_SITE="${DATA_DIR}/deps/python/${DEFAULT_PYTHON_VERSION}/lib/python3.${PY_MINOR}/site-packages"
    if [ -d "${PY_SITE}" ]; then
      export PYTHONPATH="${PY_SITE}"
    fi
  fi
fi

# 清理可能与面板内部 pip 调用冲突的环境变量（与代码侧 SanitizePipEnv 对称，双保险）。
# 用户在 docker run -e / systemd Environment= 中预设的 PIP_PREFIX 等会触发
# "Cannot set --home and --prefix together" 等冲突。
unset PIP_PREFIX PIP_HOME PIP_TARGET PIP_ROOT PIP_USER PIP_INSTALL_OPTION PYTHONUSERBASE

# --- nginx 监听地址替换（#162）----------------------------------------------
# PANEL_HOST 为空 / 0.0.0.0 / :: / [::] 时监听全部网卡（与旧版一致）；
# 设成具体 IP（localhost 按 127.0.0.1）时 nginx 只听这个地址，Go 后端随之只绑 127.0.0.1，
# 否则 host 网络下 5701 仍对局域网敞开。不是 IP 字面量就直接报错退出，绝不静默回落到全部网卡。
PANEL_HOST=${PANEL_HOST:-}
PANEL_HOST=${PANEL_HOST#\[}
PANEL_HOST=${PANEL_HOST%\]}
if [ "${PANEL_HOST}" = "localhost" ]; then
  PANEL_HOST=127.0.0.1
fi
case "${PANEL_HOST}" in
  ''|0.0.0.0|::)
    NGINX_LISTEN="${PANEL_PORT}"
    ;;
  *)
    if printf '%s' "${PANEL_HOST}" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
      NGINX_LISTEN="${PANEL_HOST}:${PANEL_PORT}"
    elif printf '%s' "${PANEL_HOST}" | grep -Eq '^[0-9A-Fa-f.]*:[0-9A-Fa-f:.]*$'; then
      NGINX_LISTEN="[${PANEL_HOST}]:${PANEL_PORT}"
    else
      fail "PANEL_HOST=${PANEL_HOST} 不是合法的 IP 地址：可填 127.0.0.1、局域网 IP、::1 或 localhost，留空表示监听全部网卡"
    fi
    export SERVER_HOST=127.0.0.1
    ;;
esac

NGINX_CONF_PATH=${NGINX_DEFAULT_CONF:-}
if [ -z "${NGINX_CONF_PATH}" ]; then
  for candidate in /etc/nginx/http.d/default.conf /etc/nginx/conf.d/default.conf /etc/nginx/sites-enabled/default; do
    if [ -f "${candidate}" ]; then
      NGINX_CONF_PATH="${candidate}"
      break
    fi
  done
fi

if [ -n "${NGINX_CONF_PATH}" ] && [ -f "${NGINX_CONF_PATH}" ]; then
  # 按整条 listen 指令替换：docker restart 时 conf 已经是 `listen 127.0.0.1:5700;`，再跑一次结果不变
  sed -i "s/^\([[:space:]]*\)listen [^;]*;/\1listen ${NGINX_LISTEN};/" "${NGINX_CONF_PATH}"
fi

# --- config.yaml 幂等生成 --------------------------------------------------
# 历史背景：
#   v2.2.5 及更早：每次启动 cat 覆盖 config.yaml，用户在面板里改过的 CORS /
#     信任代理 / JWT 过期时间等会被强制丢失。
#   v2.2.6：改成"幂等"——文件存在就不动。但 Dockerfile 把 server/config.yaml
#     里的 `path: ./data/daidai.db` 这种相对路径占位 COPY 到了 /app/config.yaml，
#     幂等逻辑保留了占位，daidai-server 按 cwd 解析得到 /app/data/daidai.db，
#     新建空库，旧数据（/app/Dumb-Panel/daidai.db）找不到 → "面板像刚装好"。
#   v2.2.7：用硬编码字符串 `./data/daidai.db` / `dir: ./data` 识别"占位"。
#     用户改过 config 但 path 仍是任意相对路径的情形仍会漏检。
#
# v2.2.9 修复策略（与代码层 cfg.Database.Path 也转绝对路径配合）：
#   只要 database.path 或 data.dir 不是绝对路径，就视为"必须重写"。占位形态、
#   用户笔误的相对路径、其他相对路径变体一并被纠正成 ${DATA_DIR}/... 绝对路径。
#   用户用绝对路径 (含自定义 data dir) 的不会被误覆盖。
#   重写后再扫一遍已知的历史 db 位置，发现非空 db 但与当前 DATA_DIR 不一致就
#   打印 WARN，把恢复命令直接喂给用户，避免极端场景下还要人肉排查路径。
build_cors_origins_yaml() {
  # CORS_ORIGINS 支持逗号/换行/空格分隔，例如：
  #   CORS_ORIGINS=https://nas.example.com,http://192.168.1.10:5700
  default_lines="    - http://localhost:5173
    - http://localhost:${PANEL_PORT}"
  user_input=${CORS_ORIGINS:-}
  if [ -z "${user_input}" ]; then
    printf '%s\n' "${default_lines}"
    return
  fi

  printf '%s\n' "${default_lines}"
  printf '%s' "${user_input}" | tr ',\n' '  ' | tr -s ' ' '\n' | while IFS= read -r origin; do
    origin=$(printf '%s' "${origin}" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
    [ -z "${origin}" ] && continue
    printf '    - %s\n' "${origin}"
  done
}

extract_yaml_scalar() {
  # 取顶层 database.path / data.dir 的字面值。awk 比 grep+sed 更稳，能容忍前后空白。
  # 参数：$1=文件 $2=key（path / dir）
  awk -v key="$2" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*:" {
      sub("^[[:space:]]*" key "[[:space:]]*:[[:space:]]*", "", $0)
      sub("[[:space:]]+#.*$", "", $0)
      sub("[[:space:]]+$", "", $0)
      gsub(/^["\047]|["\047]$/, "", $0)
      print $0
      exit
    }
  ' "$1" 2>/dev/null
}

config_needs_rewrite() {
  # 文件缺失 → 必须生成
  [ -f "$1" ] || return 0
  db_path=$(extract_yaml_scalar "$1" path)
  data_dir=$(extract_yaml_scalar "$1" dir)
  # 任何一项为空或不是绝对路径就视为"未初始化"，需要重写
  case "${db_path}" in
    /*) ;;
    *) return 0 ;;
  esac
  case "${data_dir}" in
    /*) ;;
    *) return 0 ;;
  esac
  return 1
}

scan_legacy_db_locations() {
  # v2.2.6 受害用户的数据可能残留在两类位置：
  #   1) /app/data/daidai.db —— v2.2.6 错位生成的空库/半库
  #   2) 任意自定义挂载点下的旧库 —— 用户用 `-v /host/x:/data` + DATA_DIR=/data
  #      或者类似 /config /opt/daidai /share/... 的 NAS 习惯挂载点。
  #
  # 第一阶段：扫已知常见挂载点；第二阶段：浅 find 兜底覆盖任意自定义挂载点。
  # 用临时文件汇总而不是 shell 变量——pipe to while 在 POSIX sh 里跑在子 shell，
  # 父 shell 拿不到变量修改。
  current="${DATA_DIR}/daidai.db"
  tmp_scanned=$(mktemp 2>/dev/null || echo /tmp/.daidai-scan-$$)
  : > "${tmp_scanned}"

  consider_candidate() {
    candidate=$1
    [ "${candidate}" = "${current}" ] && return 0
    [ -s "${candidate}" ] || return 0
    grep -Fxq "${candidate}" "${tmp_scanned}" 2>/dev/null && return 0
    printf '%s\n' "${candidate}" >> "${tmp_scanned}"
  }

  # 第一阶段：已知常见挂载点
  for candidate in \
      /app/data/daidai.db \
      /app/Dumb-Panel/daidai.db \
      /data/daidai.db \
      /config/daidai.db \
      /opt/daidai/daidai.db \
      /app/daidai.db; do
    consider_candidate "${candidate}"
  done

  # 第二阶段：浅扫描兜底（深度 4 平衡性能与覆盖面）。跳过系统目录避免噪音。
  if command -v find >/dev/null 2>&1; then
    find / -maxdepth 4 -name 'daidai.db' -type f \
      -not -path '/proc/*' -not -path '/sys/*' -not -path '/tmp/*' \
      -not -path '/dev/*' -not -path '/run/*' -not -path '/var/cache/*' \
      2>/dev/null | while IFS= read -r found_path; do
      consider_candidate "${found_path}"
    done
  fi

  # 汇总输出
  if [ -s "${tmp_scanned}" ]; then
    log "================================================================"
    log "检测到历史数据库残留（当前配置使用：${current}）："
    while IFS= read -r p; do
      size=$(stat -c%s "${p}" 2>/dev/null || echo '?')
      mtime=$(date -r "${p}" '+%F %T' 2>/dev/null || echo '?')
      log "  ${p}  (${size} 字节, 修改时间 ${mtime})"
    done < "${tmp_scanned}"
    log ""
    log "如其中某个是你的真实旧数据（v2.2.6 升级时被错位创建），执行恢复："
    log "  1) 选定要恢复的源路径 SRC（推荐挑文件最大、修改时间最新的）"
    log "  2) docker exec <容器名> sh -c \"cp -a SRC ${current}; \\"
    log "       cp -a SRC-shm ${current}-shm 2>/dev/null; \\"
    log "       cp -a SRC-wal ${current}-wal 2>/dev/null\""
    log "  3) docker restart <容器名>"
    log ""
    log "⚠️ 若残留只是 v2.2.6 错位生成的几 KB 空库，可忽略——直接用当前数据目录即可。"
    log "================================================================"
  fi

  rm -f "${tmp_scanned}" 2>/dev/null || true
}

NEEDS_REGENERATE=0
if [ ! -f "${APP_CONFIG_FILE}" ]; then
  NEEDS_REGENERATE=1
  log "首次启动，生成默认配置：${APP_CONFIG_FILE}"
elif config_needs_rewrite "${APP_CONFIG_FILE}"; then
  NEEDS_REGENERATE=1
  log "检测到 ${APP_CONFIG_FILE} 含相对路径（database.path / data.dir 未指向绝对位置），重写为绝对路径以恢复数据访问"
else
  log "检测到已有配置：${APP_CONFIG_FILE}，跳过覆盖（保留用户自定义）"
fi

if [ "${NEEDS_REGENERATE}" = "1" ]; then
  CORS_BLOCK=$(build_cors_origins_yaml)
  cat > "${APP_CONFIG_FILE}" <<YAML
server:
  port: 5701
  mode: release

database:
  path: ${DATA_DIR}/daidai.db

jwt:
  secret: ""
  access_token_expire: 480h
  refresh_token_expire: 1440h

data:
  dir: ${DATA_DIR}
  scripts_dir: ${DATA_DIR}/scripts
  log_dir: ${DATA_DIR}/logs

cors:
  origins:
${CORS_BLOCK}
YAML
fi

scan_legacy_db_locations

# --- 自定义 ENTRYPOINT 透传 -------------------------------------------------
if [ $# -gt 0 ]; then
  exec "$@"
fi

# --- 启动 nginx + daidai-server ---------------------------------------------
nginx

# 停机：docker stop / compose down / watchtower 换镜像都只给 PID 1（本脚本）发 SIGTERM，默认 10 秒后 SIGKILL。
# PID 1 一退出，内核就把容器里剩下的进程整组 SIGKILL —— 所以这里不能 kill 完立刻 exit
# （v3.3.4 及以前就是这样，面板一个收尾动作都来不及做：任务停在运行中、软链残留、WAL 没 checkpoint），
# 必须把信号转给面板、等它自己收完尾（停调度、结算运行中的任务、清理软链、关库）再退出。
# 面板自己保证 8 秒内退出（server/main.go 的关停预算），这里不另设超时，兜底交给 Docker 的宽限期。
shutdown() {
  # 只处理第一次：收尾期间再来的 TERM / INT（例如 Ctrl+C 连按）一律忽略，避免重入。
  trap '' TERM INT
  if [ -n "${SERVER_PID:-}" ] && kill -TERM "${SERVER_PID}" 2>/dev/null; then
    log "收到停止信号，等待面板收尾..."
    # 必须带 || true：面板退出码非 0 时，裸 wait 会被开头的 set -e 带出脚本。
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  # nginx 没有要落盘的东西，quit 只是让它把手上的响应发完；不等它，PID 1 退出时内核会一并收走。
  nginx -s quit 2>/dev/null || true
  rm -f "${SERVER_PID_FILE}"
  # 直接在 trap 里 exit，不回到下面的重启循环：因停机而退出不能被当成「异常退出」再拉起来。
  exit 0
}
trap shutdown TERM INT

# 降权时必须用 env 把 HOME 重新钉一遍，不能只靠 /etc/passwd 里的家目录字段：
# su-exec 会按 passwd 覆写 HOME，gosu 则只在 HOME 为空时才设置（Docker 默认已经注入
# HOME=/root，所以 gosu 那条路上 HOME 会原样保持 /root，daidai 照样写不进去）。
# 两个工具行为不一致，靠 env 显式指定才能让两个镜像得到同一个结果。
# env 是 exec 掉自己，PID 不变，下面的 SERVER_PID / wait / trap 全部照旧成立。
#
# 【必须写绝对路径 /usr/bin/env】上面 export 的 PATH 首位是
# ${DATA_DIR}/deps/nodejs/node_modules/.bin —— 那是面板用户可写的目录。
# 用户在依赖页装到一个 bin 名恰好叫 env 的 npm 包（或自己往那儿放个同名脚本），
# 裸写 env 就会解析到它，daidai-server 根本不会被执行，
# 表现成「容器每 2 秒重启一次、日志里只有一个看不懂的退出码」。
# /usr/bin/env 在 Alpine（busybox）与 Debian（coreutils）上都在这个位置。
while true; do
  if [ -n "${RUN_AS_USER}" ] && command -v su-exec >/dev/null 2>&1; then
    su-exec "${RUN_AS_SPEC}" /usr/bin/env "HOME=${DAIDAI_HOME}" /app/daidai-server &
  elif [ -n "${RUN_AS_USER}" ] && command -v gosu >/dev/null 2>&1; then
    gosu "${RUN_AS_SPEC}" /usr/bin/env "HOME=${DAIDAI_HOME}" /app/daidai-server &
  else
    /app/daidai-server &
  fi
  SERVER_PID=$!
  echo "${SERVER_PID}" > "${SERVER_PID_FILE}"
  # PID 文件是 root 写的，daidai 覆写会 EACCES —— 后端只打一行日志不影响功能，
  # 但那行日志在排障时非常误导（看起来像数据目录权限没配好）。顺手把属主改过去。
  if [ -n "${RUN_AS_USER}" ]; then
    chown "${TARGET_UID}:${TARGET_GID}" "${SERVER_PID_FILE}" 2>/dev/null || true
  fi
  # 关闭 set -e 包住 wait：server 异常退出时仍要走重启循环，不能让 set -e 把脚本带出。
  # 收到停止信号时 wait 会被打断、转去执行上面的 shutdown，它自己 exit，不会回到这里。
  set +e
  wait "${SERVER_PID}"
  EXIT_CODE=$?
  set -e
  rm -f "${SERVER_PID_FILE}"
  [ ${EXIT_CODE} -eq 0 ] && exit 0
  log "daidai-server 异常退出 (code=${EXIT_CODE})，2 秒后重启"
  sleep 2
done
