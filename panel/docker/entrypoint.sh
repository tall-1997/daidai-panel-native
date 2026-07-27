#!/bin/sh
# daidai-panel 容器入口脚本
# 兼容飞牛 OS / 群晖 / 绿联 / unRAID 等第三方 NAS 部署场景。

set -e

DATA_DIR=${DATA_DIR:-/app/Dumb-Panel}
SERVER_PID_FILE="${DATA_DIR}/run/daidai-server.pid"
PANEL_PORT=${PANEL_PORT:-5700}
APP_CONFIG_FILE=${APP_CONFIG_FILE:-/app/config.yaml}

log() {
  printf '[entrypoint] %s\n' "$*"
}

fail() {
  printf '[entrypoint][ERROR] %s\n' "$*" >&2
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

# --- PUID/PGID 支持（LinuxServer.io 风格，opt-in） ---------------------------
# 飞牛 OS / 群晖等 NAS 用户通常需要让容器以宿主机用户跑，方便 SMB/NFS 共享。
# 仅当显式传入 PUID 才切换用户；保持对历史部署（默认 root）的兼容。
RUN_AS_USER=""
if [ -n "${PUID}" ] || [ -n "${PGID}" ]; then
  TARGET_UID=${PUID:-0}
  TARGET_GID=${PGID:-${TARGET_UID}}

  if ! command -v su-exec >/dev/null 2>&1 && ! command -v gosu >/dev/null 2>&1; then
    log "未找到 su-exec/gosu，PUID/PGID 设置已忽略（继续以 root 运行）"
  else
    if command -v addgroup >/dev/null 2>&1; then
      if ! getent group daidai >/dev/null 2>&1; then
        addgroup -g "${TARGET_GID}" daidai 2>/dev/null || groupadd -g "${TARGET_GID}" daidai
      fi
    else
      groupadd -g "${TARGET_GID}" daidai 2>/dev/null || true
    fi

    if command -v adduser >/dev/null 2>&1; then
      if ! id -u daidai >/dev/null 2>&1; then
        adduser -D -H -u "${TARGET_UID}" -G daidai daidai 2>/dev/null || \
          useradd -M -u "${TARGET_UID}" -g "${TARGET_GID}" -s /sbin/nologin daidai
      fi
    else
      useradd -M -u "${TARGET_UID}" -g "${TARGET_GID}" -s /sbin/nologin daidai 2>/dev/null || true
    fi

    log "应用 PUID=${TARGET_UID} PGID=${TARGET_GID}，正在调整数据目录所有权..."
    chown -R "${TARGET_UID}:${TARGET_GID}" "${DATA_DIR}" /tmp 2>/dev/null || true
    RUN_AS_USER="daidai"
  fi
fi

# --- 数据目录可写性预检 -----------------------------------------------------
WRITE_PROBE="${DATA_DIR}/.daidai-write-probe-$$"
PROBE_CMD="true"
if [ -n "${RUN_AS_USER}" ]; then
  if command -v su-exec >/dev/null 2>&1; then
    PROBE_CMD="su-exec ${RUN_AS_USER} touch ${WRITE_PROBE}"
  elif command -v gosu >/dev/null 2>&1; then
    PROBE_CMD="gosu ${RUN_AS_USER} touch ${WRITE_PROBE}"
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

# --- nginx 监听端口替换 ----------------------------------------------------
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
  sed -i "s/listen [0-9]*/listen ${PANEL_PORT}/" "${NGINX_CONF_PATH}"
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

shutdown() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -f "${SERVER_PID_FILE}"
  exit 0
}
trap shutdown TERM INT

while true; do
  if [ -n "${RUN_AS_USER}" ] && command -v su-exec >/dev/null 2>&1; then
    su-exec "${RUN_AS_USER}" /app/daidai-server &
  elif [ -n "${RUN_AS_USER}" ] && command -v gosu >/dev/null 2>&1; then
    gosu "${RUN_AS_USER}" /app/daidai-server &
  else
    /app/daidai-server &
  fi
  SERVER_PID=$!
  echo "${SERVER_PID}" > "${SERVER_PID_FILE}"
  # 关闭 set -e 包住 wait：server 异常退出时仍要走重启循环，不能让 set -e 把脚本带出。
  set +e
  wait "${SERVER_PID}"
  EXIT_CODE=$?
  set -e
  rm -f "${SERVER_PID_FILE}"
  [ ${EXIT_CODE} -eq 0 ] && exit 0
  log "daidai-server 异常退出 (code=${EXIT_CODE})，2 秒后重启"
  sleep 2
done
