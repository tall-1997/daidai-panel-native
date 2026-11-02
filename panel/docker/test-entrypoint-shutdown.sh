#!/bin/bash
##############################################################################
# docker/entrypoint.sh 的停机信号回归测试
#
# 为什么需要它：停容器时 entrypoint 是 PID 1，它怎么处理 SIGTERM 决定了面板有没有机会收尾。
# v3.3.4 及以前 trap 里 kill 完立刻 exit 0：PID 1 一退出，内核就把容器里剩下的进程整组 SIGKILL，
# 面板一个收尾动作都做不了（任务停在运行中、软链残留、WAL 没 checkpoint）。现在 shutdown()
# 把信号转给面板、等它自己退出。这些语义只有真把 entrypoint 当 PID 1 跑起来才看得出来，
# go test 与 server/service/docker_entrypoint_assets_test.go 的静态断言都管不到。
#
# 用法（需要 root）：
#   sudo bash docker/test-entrypoint-shutdown.sh
#
# 依赖：util-linux 的 unshare / setpriv、procps 的 pgrep。ubuntu-latest 的 GitHub runner 与 WSL 都自带。
#
# 被测的是【整份】entrypoint.sh。打桩范围：
#   nginx              -> 只记录调用（本机没装，而 entrypoint 在 set -e 下会被它带出）
#   find               -> 空操作（跳过全盘扫描历史库；busybox sh 下 find 是内建 applet，桩不生效，真跑一遍也很快）
#   chown              -> 转给真正的 chown；只在第 2 条用例里把给 PID 文件改属主那一下拉长 1 秒（busybox 下同样不生效）
#   python3            -> 只在「装 trap 之前收到 TERM」那条里出场：模拟启动前半段一条跑得慢的前台命令
#   su-exec / gosu     -> 用 setpriv 真降权（与 test-entrypoint-puid.sh 同法）
#   /app/daidai-server -> shell 桩：收到 TERM 后收尾 1 秒、写标记、按参数决定退出码
# 用 unshare --pid --fork --mount-proc 让 entrypoint 当 PID 1，从外面对它发信号。
# dash 跑一遍；装了 busybox 再用 busybox sh 跑一遍（Alpine 镜像里解析 entrypoint 的是 busybox ash），没装就 SKIP。
#
# 计时一律用 /proc/uptime（单调，不受墙钟校时影响 —— WSL 的墙钟会跳），并且只断言上界。
#
# 【怎么让它失败一次】用 ENTRYPOINT_SRC 指向一个改坏的 entrypoint：
#   ENTRYPOINT_SRC=<文件> sudo -E bash docker/test-entrypoint-shutdown.sh
# 已实测的突变：
#   - shutdown() 换回「kill 完立刻 exit 0」       -> 「TERM 转发给面板」变红
#   - 删掉 shutdown() 里 wait 后面的 || true      -> 「面板收尾后以非 0 退出」变红
#   - 删掉脚本开头的 trap 'exit 143' TERM INT     -> 「启动前半段收到 TERM」变红
##############################################################################

set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
SRC="${ENTRYPOINT_SRC:-$SCRIPT_DIR/entrypoint.sh}"

if [ "$(id -u)" != "0" ]; then
  echo "!! 需要 root：要用 unshare 建 PID / mount 命名空间、挂 tmpfs、用 setpriv 降权"
  exit 2
fi

# 与 test-entrypoint-puid.sh 同理：entrypoint 会 chmod / chown -R /tmp、在根目录建 /ql，
# 必须在私有 mount namespace 里跑，并给 /tmp、/app、/ql 各挂一份独立 tmpfs。
# 重入用 bash "$0"：本文件不一定带可执行位（Windows 检出、从 zip 解出来都可能丢）。
if [ "${DAIDAI_SHUTDOWN_TEST_ISOLATED:-0}" != "1" ]; then
  exec unshare --mount --fork --propagation private \
    env DAIDAI_SHUTDOWN_TEST_ISOLATED=1 ENTRYPOINT_SRC="$SRC" bash "$0" "$@"
fi

if [ -e /app ] && [ -n "$(ls -A /app 2>/dev/null)" ]; then
  echo "!! /app 已存在且非空，为避免误伤本机环境直接退出"
  exit 2
fi
if [ -e /ql ] && [ -n "$(ls -A /ql 2>/dev/null)" ]; then
  echo "!! /ql 已存在且非空（本机可能真的在跑青龙），为避免误伤直接退出"
  exit 2
fi

# 只删自己建的挂载点目录（理由见 test-entrypoint-puid.sh 同名段落）。
APP_CREATED=0
QL_CREATED=0
cleanup() {
  umount /app 2>/dev/null
  [ "$APP_CREATED" = "1" ] && rmdir /app 2>/dev/null
  umount /ql 2>/dev/null
  [ "$QL_CREATED" = "1" ] && rmdir /ql 2>/dev/null
  return 0
}
trap cleanup EXIT

mount -t tmpfs tmpfs /tmp || { echo "!! 无法在私有命名空间里挂 tmpfs 到 /tmp"; exit 2; }
[ -d /app ] || { mkdir -p /app && APP_CREATED=1; }
mount -t tmpfs tmpfs /app || { echo "!! 无法挂 tmpfs 到 /app"; exit 2; }
[ -d /ql ] || { mkdir -p /ql && QL_CREATED=1; }
mount -t tmpfs tmpfs /ql || { echo "!! 无法挂 tmpfs 到 /ql"; exit 2; }

WORK=$(mktemp -d)
FAILED=0
FAIL_COUNT=0

echo "被测脚本: $SRC"
tr -d '\r' < "$SRC" > "$WORK/entrypoint.sh"

mkdir -p "$WORK/bin"
cat > "$WORK/bin/nginx" <<'STUB'
#!/bin/sh
if [ "${1:-}" = "-s" ]; then
  echo "nginx -s ${2:-}" >> "$NGINX_LOG"
else
  echo "nginx start" >> "$NGINX_LOG"
fi
exit 0
STUB
printf '#!/bin/sh\nexit 0\n' > "$WORK/bin/find"
# chown 桩：直接转给真正的 chown；只有 SLOW_PIDFILE_CHOWN=1 时，给 PID 文件改属主那一下多停 1 秒（见第 2 条用例）。
cat > "$WORK/bin/chown" <<'STUB'
#!/bin/sh
if [ "${SLOW_PIDFILE_CHOWN:-0}" = "1" ]; then
  for arg in "$@"; do
    case "$arg" in
      */run/daidai-server.pid)
        echo "pidfile chown entered" >> "$EARLY_MARK"
        sleep 1
        ;;
    esac
  done
fi
for real in /usr/bin/chown /bin/chown; do
  [ -x "$real" ] && exec "$real" "$@"
done
echo "chown stub: real chown not found" >&2
exit 1
STUB
# 启动前半段一条跑得慢的前台命令（真实场景是 PUID 的 chown -R、find 扫描历史库）。
# 只有数据目录里有 deps/python/<版本> 时 entrypoint 才会调 python3，所以只在那一条用例里出场。
cat > "$WORK/bin/python3" <<'STUB'
#!/bin/sh
echo "slow early step entered" >> "$EARLY_MARK"
sleep 2
echo 12
STUB

# 降权桩：两个工具都认 user:group，用 setpriv 真降权（细节见 test-entrypoint-puid.sh）。
cat > "$WORK/bin/_resolve" <<'STUB'
_spec="$1"
case "$_spec" in
  *:*) _u="${_spec%%:*}"; _g="${_spec#*:}" ;;
  *)   _u="$_spec";       _g="" ;;
esac
_uid="$(id -u "$_u")"
if [ -n "$_g" ]; then
  case "$_g" in
    ''|*[!0-9]*) _gid="$(getent group "$_g" | cut -d: -f3)" ;;
    *)           _gid="$_g" ;;
  esac
else
  _gid="$(id -g "$_u")"
fi
STUB
for tool in su-exec gosu; do
  {
    echo '#!/bin/sh'
    cat "$WORK/bin/_resolve"
    echo 'shift'
    echo 'exec setpriv --reuid="$_uid" --regid="$_gid" --clear-groups "$@"'
  } > "$WORK/bin/$tool"
done
rm -f "$WORK/bin/_resolve"

# 面板桩：先记下自己的 PID 与 uid（降权链路那条要拿它和 PID 文件比），再按 STUB_MODE 行事。
cat > /app/daidai-server <<'STUB'
#!/bin/sh
echo "start pid=$$ uid=$(id -u)" >> "$STUB_LOG"
case "${STUB_MODE:-normal}" in
  crash-once)
    if [ ! -e "$STUB_LOG.crashed" ]; then
      : > "$STUB_LOG.crashed"
      sleep 0.3
      echo "crash" >> "$STUB_LOG"
      exit 1
    fi
    ;;
  crash-always)
    sleep 0.3
    echo "crash" >> "$STUB_LOG"
    exit 1
    ;;
  self-exit)
    sleep 0.3
    echo "self-exit" >> "$STUB_LOG"
    exit 0
    ;;
esac
on_term() {
  echo "term" >> "$STUB_LOG"
  sleep 1
  echo "cleanup-done" >> "$STUB_LOG"
  exit "${STUB_TERM_EXIT:-0}"
}
trap on_term TERM
echo "ready" >> "$STUB_LOG"
# wait 会被收到的 TERM 立刻打断、转去执行 on_term；不能写成前台 sleep（要等它跑完才执行 trap）。
while :; do
  sleep 300 &
  wait $!
done
STUB
chmod +x "$WORK/bin"/* /app/daidai-server

mono_ms() { awk '{ printf "%d", $1 * 1000 }' /proc/uptime; }

# 进程还活着（僵尸不算：unshare 是本脚本的子进程，退出后在被 wait 之前是僵尸）。
proc_alive() {
  local state
  state=$(awk '{ print $3 }' "/proc/$1/stat" 2>/dev/null) || return 1
  [ -n "$state" ] && [ "$state" != "Z" ] && [ "$state" != "X" ]
}

# wait_for 文件 正则 超时毫秒
wait_for() {
  local deadline=$(( $(mono_ms) + $3 ))
  while [ "$(mono_ms)" -lt "$deadline" ]; do
    grep -qE -- "$2" "$1" 2>/dev/null && return 0
    sleep 0.05
  done
  return 1
}

count_lines() { grep -cE -- "$2" "$1" 2>/dev/null; }

# same_nonempty A B：两个值都非空且相等。
same_nonempty() { [ -n "$1" ] && [ "$1" = "$2" ]; }

pass() { echo "  [PASS] $*"; }
fail() { echo "  [FAIL] $*"; FAILED=1; FAIL_COUNT=$((FAIL_COUNT + 1)); }

check() {  # check 描述 条件命令...
  local desc="$1"; shift
  if "$@"; then pass "$desc"; else fail "$desc"; fi
}

# new_case 描述：为一条用例准备独立的数据目录与日志文件，打印用例标题。
# 需要预先摆好的数据（例如 deps/python/<版本>）要在 new_case 之后、start_pid1 之前放进 $DATA。
new_case() {
  echo "CASE: $1"
  CASE_START_FAILS=$FAIL_COUNT
  CASE_DIR=$(mktemp -d "$WORK/case.XXXXXX")
  DATA="$CASE_DIR/data"
  STUB_LOG="$CASE_DIR/stub.log"
  NGINX_LOG="$CASE_DIR/nginx.log"
  EARLY_MARK="$CASE_DIR/early.mark"
  OUT="$CASE_DIR/out.log"
  mkdir -p "$DATA"
  : > "$STUB_LOG"
  : > "$NGINX_LOG"
  # 降权那条里面板桩以 nobody 身份写日志
  chmod 666 "$STUB_LOG" "$NGINX_LOG"
}

show_case_logs() {
  echo "  --- entrypoint 输出"
  sed 's/^/    /' "$OUT"
  echo "  --- 面板桩日志"
  sed 's/^/    /' "$STUB_LOG"
}

# end_case：这条用例有失败时把 entrypoint 输出与面板桩日志打出来；PID 1 还活着就强杀，不留给下一条。
end_case() {
  if proc_alive "$UNSHARE_PID"; then
    kill -KILL "$PID1" 2>/dev/null
    wait "$UNSHARE_PID" 2>/dev/null
  fi
  if [ "$FAIL_COUNT" -ne "$CASE_START_FAILS" ]; then
    show_case_logs
  fi
}

# start_pid1 SHELL [NAME=VALUE ...]：用 SHELL 把整份 entrypoint 当 PID 1 起在后台（数据目录用 new_case 准备好的那个）。
start_pid1() {
  local shell="$1"; shift
  TIMED_OUT=0
  RC=""
  ELAPSED_MS=""
  # shellcheck disable=SC2086  # $shell 刻意不加引号：「busybox sh」要拆成两个词
  unshare --pid --fork --mount-proc \
    env -i PATH="$WORK/bin:/usr/sbin:/usr/bin:/sbin:/bin" HOME=/root \
      DATA_DIR="$DATA" APP_CONFIG_FILE="$DATA/config.yaml" \
      STUB_LOG="$STUB_LOG" NGINX_LOG="$NGINX_LOG" EARLY_MARK="$EARLY_MARK" "$@" \
      $shell "$WORK/entrypoint.sh" > "$OUT" 2>&1 &
  UNSHARE_PID=$!
  PID1=""
  local deadline=$(( $(mono_ms) + 5000 ))
  while [ -z "$PID1" ] && [ "$(mono_ms)" -lt "$deadline" ]; do
    PID1=$(pgrep -P "$UNSHARE_PID" | head -n1)
    [ -n "$PID1" ] || sleep 0.02
  done
}

# wait_pid1 超时毫秒：等 PID 1 退出，RC 是它的退出码；超时就 SIGKILL 并记 TIMED_OUT=1（模拟 Docker 的宽限期到点）。
wait_pid1() {
  local deadline=$(( $(mono_ms) + $1 ))
  while proc_alive "$UNSHARE_PID"; do
    if [ "$(mono_ms)" -ge "$deadline" ]; then
      TIMED_OUT=1
      kill -KILL "$PID1" 2>/dev/null
      break
    fi
    sleep 0.02
  done
  wait "$UNSHARE_PID"
  RC=$?
}

# signal_pid1 信号 超时毫秒：从外面给 PID 1 发信号并等它退出，ELAPSED_MS 是发信号到 PID 1 退出的耗时。
signal_pid1() {
  local t0
  t0=$(mono_ms)
  kill -"$1" "$PID1"
  wait_pid1 "$2"
  ELAPSED_MS=$(( $(mono_ms) - t0 ))
}

run_suite() {
  local shell="$1" t0 stub_pid stub_uid pid_file
  echo "=================================================================="
  echo "SHELL: $shell"

  # ---- 1. TERM 转发并等待收尾 ------------------------------------------------
  new_case "TERM 转发给面板，等它收完尾再退出"
  start_pid1 "$shell"
  if ! wait_for "$STUB_LOG" '^ready$' 10000; then
    fail "面板桩没有起来"
  else
    signal_pid1 TERM 15000
    check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
    check "PID 1 没有拖到被强杀" [ "$TIMED_OUT" -eq 0 ]
    check "面板收到 TERM 并完成收尾（标记写出）" grep -q '^cleanup-done$' "$STUB_LOG"
    check "面板只被拉起 1 次（start=$(count_lines "$STUB_LOG" '^start ')）" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 1 ]
    check "耗时在上界内（${ELAPSED_MS}ms < 5000ms）" [ "$ELAPSED_MS" -lt 5000 ]
    check "收尾后通知 nginx 退出" grep -q '^nginx -s quit$' "$NGINX_LOG"
    check "PID 文件已删除" [ ! -e "$DATA/run/daidai-server.pid" ]
    check "entrypoint 日志带 Go log 同款时间戳" grep -qE '^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} \[entrypoint\] 收到停止信号' "$OUT"
  fi
  end_case

  # ---- 2. 面板收尾后以非 0 退出 ----------------------------------------------
  # shutdown() 里 wait 后面那句 || true 防的是：面板以非 0 退出时被开头的 set -e 带出 trap，PID 1 跟着以非 0 退出。
  # 注意重启循环用 set +e 包住了 wait：TERM 落在 wait 里时 trap 是在 errexit 关着的状态下跑的，删掉 || true 也看不出来；
  # 只有 TERM 落在「面板已经起来、还没进 set +e」这一小段时 errexit 才开着。这里走降权分支，用 chown 桩把给 PID 文件
  # 改属主那一下拉长到 1 秒，让 TERM 稳稳落在这一段里。busybox sh 下 chown 是内建 applet、桩不生效，拉不长这一段，
  # 那一遍 TERM 落在 wait 里，只验「PID 1 仍以 0 退出」；本机没有 uid 65534 时同样退回不降权的跑法。
  new_case "面板收尾后以非 0 退出，PID 1 仍以 0 退出（防 wait 后面的 || true 被删）"
  local hold_window=0
  if [ "$shell" = "dash" ] && [ -n "$(getent passwd 65534)" ] && [ -n "$(getent group 65534)" ]; then
    hold_window=1
    start_pid1 "$shell" STUB_TERM_EXIT=3 PUID=65534 PGID=65534 SLOW_PIDFILE_CHOWN=1
  else
    start_pid1 "$shell" STUB_TERM_EXIT=3
  fi
  if [ "$hold_window" = "1" ] && ! wait_for "$EARLY_MARK" 'pidfile chown entered' 10000; then
    fail "没有走到给 PID 文件改属主那一步"
  elif ! wait_for "$STUB_LOG" '^ready$' 10000; then
    fail "面板桩没有起来"
  else
    signal_pid1 TERM 15000
    check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
    check "面板完成收尾" grep -q '^cleanup-done$' "$STUB_LOG"
    check "没有被当成异常退出再拉起（start=$(count_lines "$STUB_LOG" '^start ')）" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 1 ]
  fi
  end_case

  # ---- 3. 收尾期间重复信号 ----------------------------------------------------
  new_case "收尾期间再来 TERM + INT，被忽略、收尾照常完成"
  start_pid1 "$shell"
  if ! wait_for "$STUB_LOG" '^ready$' 10000; then
    fail "面板桩没有起来"
  else
    t0=$(mono_ms)
    kill -TERM "$PID1"
    if wait_for "$STUB_LOG" '^term$' 5000; then
      kill -TERM "$PID1" 2>/dev/null
      kill -INT "$PID1" 2>/dev/null
    fi
    wait_pid1 15000
    ELAPSED_MS=$(( $(mono_ms) - t0 ))
    check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
    check "收尾照常完成（标记写出）" grep -q '^cleanup-done$' "$STUB_LOG"
    check "面板只被拉起 1 次" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 1 ]
  fi
  end_case

  # ---- 4. 崩溃后照常重启 ------------------------------------------------------
  new_case "面板以 1 退出时 2 秒后照常重启，之后的 TERM 不再拉起第 3 次"
  start_pid1 "$shell" STUB_MODE=crash-once
  if ! wait_for "$STUB_LOG" '^ready$' 10000; then
    fail "崩溃后没有被重新拉起"
  else
    check "崩溃后被重新拉起（第 2 次启动）" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 2 ]
    signal_pid1 TERM 15000
    check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
    check "第 2 次启动的面板完成收尾" grep -q '^cleanup-done$' "$STUB_LOG"
    check "TERM 之后没有第 3 次启动" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 2 ]
  fi
  end_case

  # ---- 5. 重启间隙收到 TERM ---------------------------------------------------
  new_case "TERM 落在「2 秒后重启」的间隙里：直接退出，不再拉起"
  start_pid1 "$shell" STUB_MODE=crash-always
  if ! wait_for "$OUT" '2 秒后重启' 10000; then
    fail "没有进入重启间隙"
  else
    signal_pid1 TERM 15000
    check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
    check "不再拉起（start=$(count_lines "$STUB_LOG" '^start ')）" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 1 ]
    # dash 要等外部 sleep 2 跑完才执行 trap，busybox 的 sleep 是内建、立即被打断；上界取 4 秒。
    check "耗时在上界内（${ELAPSED_MS}ms < 4000ms）" [ "$ELAPSED_MS" -lt 4000 ]
  fi
  end_case

  # ---- 6. 面板自己以 0 退出 ---------------------------------------------------
  new_case "面板自己以 0 退出，entrypoint 也以 0 退出（锁住现有语义）"
  start_pid1 "$shell" STUB_MODE=self-exit
  wait_pid1 10000
  check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
  check "不是被强杀的" [ "$TIMED_OUT" -eq 0 ]
  check "面板只被拉起 1 次" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 1 ]
  check "面板确实是自己退出的" grep -q '^self-exit$' "$STUB_LOG"
  end_case

  # ---- 7. 降权链路 ------------------------------------------------------------
  # su-exec / gosu 与 /usr/bin/env 都是 exec 掉自己，PID 不变：entrypoint 记下的 SERVER_PID 就是面板本身，
  # TERM 才能送到降权后的面板。用系统自带的 nobody（65534）而不是新建账号：账号不受 mount namespace 隔离。
  new_case "降权链路（PUID），TERM 送到降权后的面板本身"
  if [ -z "$(getent passwd 65534)" ] || [ -z "$(getent group 65534)" ]; then
    echo "  [SKIP] 本机没有 uid/gid 65534，跳过降权链路（不为它新建账号）"
  else
    start_pid1 "$shell" PUID=65534 PGID=65534
    if ! wait_for "$STUB_LOG" '^ready$' 10000; then
      fail "降权后的面板桩没有起来"
    else
      stub_pid=$(sed -n 's/^start pid=\([0-9]*\) uid=.*/\1/p' "$STUB_LOG" | head -n1)
      stub_uid=$(sed -n 's/^start pid=[0-9]* uid=\([0-9]*\)$/\1/p' "$STUB_LOG" | head -n1)
      pid_file=$(tr -d '[:space:]' < "$DATA/run/daidai-server.pid" 2>/dev/null)
      check "面板以 uid 65534 运行（uid=$stub_uid）" [ "$stub_uid" = "65534" ]
      check "PID 文件里的 SERVER_PID 就是面板自己（file=$pid_file self=$stub_pid）" same_nonempty "$stub_pid" "$pid_file"
      signal_pid1 TERM 15000
      check "PID 1 以 0 退出（rc=$RC）" [ "$RC" -eq 0 ]
      check "降权后的面板收到了 TERM 并完成收尾" grep -q '^cleanup-done$' "$STUB_LOG"
    fi
    end_case
  fi

  # ---- 8. 装 trap 之前收到 TERM -----------------------------------------------
  # 没装 trap 的 PID 1 收到的 SIGTERM 会被内核丢掉：启动前半段（chown -R、find）期间 docker stop 要白等满 10 秒。
  # 现在脚本开头先装 trap 'exit 143'：手上那条前台命令跑完就退出，面板根本不会起来。
  new_case "启动前半段（面板还没起）收到 TERM，立即退出、不再启动面板"
  # 让 entrypoint 在 PY_MINOR=$(python3 ...) 那里停 2 秒（模拟一条跑得慢的前台命令）
  mkdir -p "$DATA/deps/python/3.12"
  start_pid1 "$shell"
  if ! wait_for "$EARLY_MARK" 'slow early step entered' 10000; then
    fail "没有走到启动前半段那条慢命令"
  else
    signal_pid1 TERM 8000
    check "PID 1 没有拖到被强杀" [ "$TIMED_OUT" -eq 0 ]
    check "PID 1 以 143 退出（rc=$RC）" [ "$RC" -eq 143 ]
    check "慢命令跑完就退出（${ELAPSED_MS}ms < 4000ms）" [ "$ELAPSED_MS" -lt 4000 ]
    check "面板没有被启动" [ "$(count_lines "$STUB_LOG" '^start ')" -eq 0 ]
    check "nginx 没有被启动" [ "$(count_lines "$NGINX_LOG" '^nginx start$')" -eq 0 ]
  fi
  end_case
}

run_suite dash
if command -v busybox >/dev/null 2>&1; then
  run_suite "busybox sh"
else
  echo "=================================================================="
  echo "[SKIP] 本机没有 busybox，跳过 busybox sh 那一遍（CI 里由 workflow 先装好）"
fi

echo "=================================================================="
if [ "$FAILED" = "0" ]; then
  echo "ALL PASS"
else
  echo "SOME CHECKS FAILED ($FAIL_COUNT)"
fi
exit $FAILED
