#!/bin/bash
##############################################################################
# docker/entrypoint.sh 的 PUID/PGID 降权回归测试
#
# 为什么需要它：entrypoint.sh 此前【零测试零 CI 覆盖】，改坏了 go test 照样全绿。
# Go 侧那些静态字符串断言（server/service/docker_entrypoint_assets_test.go）只能防住
# 关键行被删掉，防不住 shell 逻辑写错 —— 这个脚本补的就是那一块：它把 entrypoint.sh
# 原样跑起来，验证最终 uid、HOME 指向、以及「HOME 到底能不能写」。
#
# 用法（需要 root）：
#   sudo bash docker/test-entrypoint-puid.sh
#
# 依赖：util-linux 的 unshare / setpriv、shadow 的 useradd/usermod/groupadd/groupmod。
# ubuntu-latest 的 GitHub runner 与常见开发机（含 WSL）都自带。
#
# 打桩范围（只桩掉与被测逻辑无关的部分）：
#   nginx            -> 空操作（本机没装，而 entrypoint 在 set -e 下会被它带出）
#   find             -> 空操作（跳过 scan_legacy_db_locations 的全盘扫描）
#   su-exec / gosu   -> 用 setpriv 真降权。两个桩的 HOME 行为【刻意不同】，
#                       分别建模这两个工具的真实差异，见下面各自的注释。
#   /app/daidai-server -> 打印 uid/gid/HOME 并真的往 $HOME/.npm 里建目录
#                       （这正是用户报障时 npm 失败的那一步）
##############################################################################

set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
SRC="${ENTRYPOINT_SRC:-$SCRIPT_DIR/entrypoint.sh}"

if [ "$(id -u)" != "0" ]; then
  echo "!! 需要 root：要建用户、chown、以及用 setpriv 降权"
  exit 2
fi

# entrypoint 会 chown -R 整个 /tmp（这是它既有的行为，容器里无害）。
# 直接在开发机 / CI runner 上跑会把宿主 /tmp 的属主改掉，影响后续步骤，
# 所以整个测试必须在私有 mount namespace 里跑，并给 /tmp 挂一份独立 tmpfs。
# 重入时用 bash "$0" 而不是直接 exec "$0"：本文件不一定带可执行位
# （Windows 检出、或从 zip 解出来时都可能丢），直接 exec 会得到一句
# 很难对上号的 "Permission denied"。
if [ "${DAIDAI_PUID_TEST_ISOLATED:-0}" != "1" ]; then
  exec unshare --mount --fork --propagation private \
    env DAIDAI_PUID_TEST_ISOLATED=1 ENTRYPOINT_SRC="$SRC" bash "$0" "$@"
fi

if [ -e /app ] && [ -n "$(ls -A /app 2>/dev/null)" ]; then
  echo "!! /app 已存在且非空，为避免误伤本机环境直接退出"
  exit 2
fi

# entrypoint 的青龙兼容层会在【根目录】建 /ql 并把它 chown 给降权用户。
# mount namespace 只隔离挂载点、不隔离对根文件系统的写，所以不额外挂一份 tmpfs 的话，
# 直接在开发机 / CI runner 上跑本脚本会在宿主真实根目录留下一个属主被改掉的 /ql。
if [ -e /ql ] && [ -n "$(ls -A /ql 2>/dev/null)" ]; then
  echo "!! /ql 已存在且非空（本机可能真的在跑青龙），为避免误伤直接退出"
  exit 2
fi

# 账号不受 mount namespace 隔离，收尾时要 userdel。所以本机上已经存在的
# daidai 账号绝不能碰 —— 仓库自己推荐的二进制部署方式
# （packaging/linux/daidai-panel.service 的 User=daidai）就会建这么一个服务账号，
# 在那种机器上跑本脚本会把它连同 /etc/shadow 条目一起删掉，面板下次直接起不来。
if id -u daidai >/dev/null 2>&1 || getent group daidai >/dev/null 2>&1; then
  echo "!! 本机已存在 daidai 用户或用户组，本脚本会在收尾时删除同名账号，为避免误删直接退出"
  echo "   （如果这台机器上没有在跑面板，可先 userdel daidai / groupdel daidai 再重跑）"
  exit 2
fi

# 🔴 先记下 /app、/ql 这两个挂载点是不是本脚本自己建的，收尾只删自己建的那些。
# 上面的守卫只拦「已存在且非空」，所以宿主上一个【已存在但为空】的 /ql 是放行的 ——
# 而那恰恰是「用户准备把青龙数据 bind-mount 到 /ql」或「fstab 里 /ql 是个当前未挂载的挂载点」
# 的典型形态。无条件 rmdir 会把人家的挂载点删掉，下次 mount 直接报 mount point does not exist。
APP_CREATED=0
QL_CREATED=0

# trap 必须在第一次 mkdir/mount 之前注册：否则下面任何一条 mount 失败走 exit 2，
# 宿主根目录会留下刚建出来的空 /app 与 /ql 而收尾根本不会跑。
cleanup() {
  umount /app 2>/dev/null
  [ "$APP_CREATED" = "1" ] && rmdir /app 2>/dev/null
  umount /ql 2>/dev/null
  [ "$QL_CREATED" = "1" ] && rmdir /ql 2>/dev/null
  # 账号不受 mount namespace 隔离，必须显式清理
  id -u daidai >/dev/null 2>&1 && userdel daidai 2>/dev/null
  getent group daidai >/dev/null 2>&1 && groupdel daidai 2>/dev/null
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

echo "被测脚本: $SRC"
tr -d '\r' < "$SRC" > "$WORK/entrypoint.sh"

mkdir -p "$WORK/bin"
printf '#!/bin/sh\nexit 0\n' > "$WORK/bin/nginx"
printf '#!/bin/sh\nexit 0\n' > "$WORK/bin/find"

# 两个桩都要认 user:group 形式 —— entrypoint 必须把 gid 一起传进来，
# 否则复用现成账号（例如 PUID=1000 命中 Debian 镜像自带的 node）时，
# 两个工具都会按 passwd 里那个用户的主组取 gid，用户填的 PGID 被静默丢掉。
cat > "$WORK/bin/_resolve" <<'STUB'
# 解析 "user" 或 "user:group"，输出 "uid gid"
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

# su-exec 的真实行为：无条件把 HOME 覆写成 /etc/passwd 里的家目录。
{
  echo '#!/bin/sh'
  cat "$WORK/bin/_resolve"
  echo 'shift'
  echo 'export HOME="$(getent passwd "$_uid" | cut -d: -f6)"'
  echo 'exec setpriv --reuid="$_uid" --regid="$_gid" --clear-groups "$@"'
} > "$WORK/bin/su-exec"

# gosu 的真实行为：HOME 已有值时【不覆写】。Docker 默认注入 HOME=/root，
# 所以 Debian 镜像那条路上降权用户会拿到一个 root:root 0700 的 HOME ——
# entrypoint 里那句 env "HOME=..." 就是为这条路存在的。
{
  echo '#!/bin/sh'
  cat "$WORK/bin/_resolve"
  echo 'shift'
  echo 'exec setpriv --reuid="$_uid" --regid="$_gid" --clear-groups "$@"'
} > "$WORK/bin/gosu"
rm -f "$WORK/bin/_resolve"

cat > /app/daidai-server <<'STUB'
#!/bin/sh
echo "SERVER uid=$(id -u) gid=$(id -g) HOME=$HOME"
# npm 启动时无条件初始化 cacache（$HOME/.npm/_cacache），
# 用户报障的那条 EACCES 就是死在这一步。
if mkdir -p "$HOME/.npm/_cacache" 2>/dev/null; then
  echo "SERVER npm-cache-writable=yes"
else
  echo "SERVER npm-cache-writable=NO"
fi
exit 0
STUB
chmod +x "$WORK/bin"/* /app/daidai-server

run_case() {
  local desc="$1"; shift
  local data
  data=$(mktemp -d)
  echo "=================================================================="
  echo "CASE: $desc"
  LAST_OUT=$(env -i PATH="$WORK/bin:/usr/sbin:/usr/bin:/sbin:/bin" HOME=/root \
    DATA_DIR="$data" APP_CONFIG_FILE="$data/config.yaml" "$@" \
    sh "$WORK/entrypoint.sh" 2>&1)
  LAST_RC=$?
  printf '%s\n' "$LAST_OUT" | sed 's/^/  /'
  echo "  exit=$LAST_RC"
  LAST_DATA="$data"
}

expect() {
  if printf '%s\n' "$LAST_OUT" | grep -q -- "$2"; then
    echo "  [PASS] $1"
  else
    echo "  [FAIL] $1  （期望输出里含: $2）"
    FAILED=1
  fi
}

expect_rc0() {
  if [ "$LAST_RC" -eq 0 ]; then
    echo "  [PASS] entrypoint 正常退出"
  else
    echo "  [FAIL] entrypoint 退出码 $LAST_RC（set -e 把脚本带出了？）"
    FAILED=1
  fi
}

expect_owner() {
  local got
  got=$(stat -c '%u:%g' "$1" 2>/dev/null)
  if [ "$got" = "$2" ]; then
    echo "  [PASS] $1 属主 $got"
  else
    echo "  [FAIL] $1 属主 want=$2 got=${got:-不存在}"
    FAILED=1
  fi
}

# 软链断言：$1=软链位置 $2=期望指向。用 readlink 只读链接本身，
# 不用 [ -d ]（目标存在与否都不该影响「链接建对了没有」这个判定）。
expect_symlink() {
  local got
  got=$(readlink "$1" 2>/dev/null)
  if [ "$got" = "$2" ]; then
    echo "  [PASS] $1 -> $got"
  else
    echo "  [FAIL] $1 want-> $2 got-> ${got:-不是软链或不存在}"
    FAILED=1
  fi
}

# ---- 1. 全新降权 -----------------------------------------------------------
run_case "PUID=5000 PGID=5000（全新）" PUID=5000 PGID=5000
expect_rc0
expect "以 5000 身份运行"             "SERVER uid=5000 gid=5000"
expect "HOME 指向数据目录下的 .home"  "HOME=$LAST_DATA/.home"
expect "npm cache 可写"               "SERVER npm-cache-writable=yes"
expect_owner "$LAST_DATA/.home" "5000:5000"
# 青龙兼容层（issue #110）：软链要建对，且 /ql 必须跟着 chown 给降权用户 ——
# 青龙脚本第一步就是 touch "$QL_DIR/shell/env.sh"，属主不对它就直接退出码 1。
expect_symlink /ql/data/repo "$LAST_DATA/scripts"
expect_symlink /ql/data/config "$LAST_DATA"
expect_symlink /ql/data/log "$LAST_DATA/logs"
expect_owner /ql/shell/env.sh "5000:5000"
userdel daidai 2>/dev/null; groupdel daidai 2>/dev/null

# ---- 2. UID/GID 撞车 -------------------------------------------------------
# Debian 镜像基于 node:24-bookworm-slim，自带 uid/gid 1000 的 node 用户，
# 而 compose 注释里给的示例恰好就是最常见的 PUID=1000 —— 原来会直接把容器带崩。
#
# 这里刻意用一个自建的 uid/gid 7000 账号而不是「机器上碰巧存在的 uid 1000」：
# 前提条件由脚本自己保证，撞车场景才不会在某些机器上静默退化成「无冲突」，
# 把 entrypoint 的复用逻辑整段删掉也照样绿。
# 并且把这个账号的【主组设成 100(users)】—— 与稍后要传的 PGID=7000 不同，
# 这样才能验出「gid 取的是 PGID 而不是 passwd 里的主组」。
groupadd -g 7000 daidai-collide-grp 2>/dev/null || true
useradd -M -u 7000 -g 100 -s /usr/sbin/nologin daidai-collide 2>/dev/null || true
if [ -z "$(getent passwd 7000)" ] || [ -z "$(getent group 7000)" ]; then
  echo "!! 无法预置 uid/gid 7000 的撞车场景，这条用例会退化成无冲突路径，判为失败"
  FAILED=1
else
  echo "预置完成：uid 7000 = $(getent passwd 7000 | cut -d: -f1)（主组 gid $(id -g daidai-collide)）, gid 7000 = $(getent group 7000 | cut -d: -f1)"
  run_case "PUID=7000 PGID=7000（uid/gid 均已被占用，且现成账号主组≠PGID）" PUID=7000 PGID=7000
  expect_rc0
  expect "复用现成账号运行"                 "SERVER uid=7000"
  # 这一条是关键：passwd 里 daidai-collide 的主组是 100，
  # 只把用户名传给 su-exec/gosu 的话进程 gid 会是 100，用户填的 PGID 被丢掉。
  expect "gid 取自 PGID 而不是账号主组"     "SERVER uid=7000 gid=7000"
  expect "npm cache 可写"                   "SERVER npm-cache-writable=yes"
  expect_owner "$LAST_DATA/.home" "7000:7000"
fi
userdel daidai-collide 2>/dev/null; groupdel daidai-collide-grp 2>/dev/null
userdel daidai 2>/dev/null; groupdel daidai 2>/dev/null

# ---- 3. 只设 PGID：不该造出 uid=0 的假降权 ---------------------------------
run_case "只设 PGID=1000" PGID=1000
expect_rc0
expect "跳过降权，仍以 root 运行" "SERVER uid=0"
expect "有明确说明"               "等价于以 root 运行，已跳过降权"

# ---- 4. 改了 PUID 之后 docker restart（容器可写层里已有旧 daidai）----------
run_case "PUID=6000 PGID=6000（先建一次）" PUID=6000 PGID=6000
expect_rc0
expect "首次以 6000 运行" "SERVER uid=6000"
run_case "PUID=6001 PGID=6001（同一容器层改 PUID）" PUID=6001 PGID=6001
expect_rc0
expect "usermod 就地修正后以 6001 运行" "SERVER uid=6001 gid=6001"
expect "npm cache 可写"                  "SERVER npm-cache-writable=yes"
expect_owner "$LAST_DATA/.home" "6001:6001"
userdel daidai 2>/dev/null; groupdel daidai 2>/dev/null

# ---- 5. gosu 分支（Debian 镜像没有 su-exec，且 gosu 不覆写 HOME）-----------
# 这一条专门证明 entrypoint 里那句 env "HOME=..." 是「有用的」而不是冗余：
# 去掉它，HOME 会停在 Docker 注入的 /root，降权用户写不进去。
mv -f "$WORK/bin/su-exec" "$WORK/bin/su-exec.disabled"
run_case "PUID=5002 PGID=5002（只有 gosu，且 gosu 不覆写 HOME）" PUID=5002 PGID=5002
expect_rc0
expect "以 5002 身份运行"                    "SERVER uid=5002 gid=5002"
expect "HOME 没停在 /root，被 env 钉到可写目录" "HOME=$LAST_DATA/.home"
expect "npm cache 可写"                      "SERVER npm-cache-writable=yes"
# 必须还原【原来那个】 su-exec 桩（它会覆写 HOME），不能拿 gosu 桩顶替 ——
# 否则后面新增的用例会以为自己在测 su-exec 路径，实际跑的是 gosu 的语义。
mv -f "$WORK/bin/su-exec.disabled" "$WORK/bin/su-exec"
userdel daidai 2>/dev/null; groupdel daidai 2>/dev/null

# ---- 6. 数据目录只读：必须给出友好诊断，不能静默退出 ------------------------
# 这是本轮差点引入的回归：新加的 mkdir "$DAIDAI_HOME" 一旦不带 || true，
# 在 :ro 挂载 / NFS root_squash 下会被 set -e 直接带出 —— docker logs 里
# 一行输出都没有，而下面那道可写性预检本来能说清「数据目录不可写 + 怎么修」。
#
# 场景要精确：子目录必须【已经存在】（上一次成功运行留下的），
# 这样开头那批 mkdir -p 是空操作，第一个真正要新建的目录就是 .home。
RO_DATA=$(mktemp -d)
mount -t tmpfs tmpfs "$RO_DATA" 2>/dev/null && RO_MOUNTED=1 || RO_MOUNTED=0
if [ "$RO_MOUNTED" = "1" ]; then
  mkdir -p "$RO_DATA/scripts" "$RO_DATA/logs" "$RO_DATA/backups" "$RO_DATA/run" \
           "$RO_DATA/deps/nodejs" "$RO_DATA/deps/python"
  : > "$RO_DATA/config.yaml"
  mount -o remount,ro "$RO_DATA"

  echo "=================================================================="
  echo "CASE: 数据目录只读 + PUID（必须给出友好诊断）"
  RO_OUT=$(env -i PATH="$WORK/bin:/usr/sbin:/usr/bin:/sbin:/bin" HOME=/root \
    DATA_DIR="$RO_DATA" APP_CONFIG_FILE="$RO_DATA/config.yaml" PUID=5004 PGID=5004 \
    sh "$WORK/entrypoint.sh" 2>&1)
  RO_RC=$?
  printf '%s\n' "$RO_OUT" | sed 's/^/  /'
  echo "  exit=$RO_RC"

  if [ "$RO_RC" -ne 0 ]; then
    echo "  [PASS] 只读数据目录下仍然以非 0 退出"
  else
    echo "  [FAIL] 只读数据目录下竟然启动成功了"
    FAILED=1
  fi
  if printf '%s\n' "$RO_OUT" | grep -q "数据目录 .* 不可写"; then
    echo "  [PASS] 打出了「数据目录不可写」的友好诊断（而不是静默退出）"
  else
    echo "  [FAIL] 没有友好诊断 —— mkdir 家目录多半又变回裸写、被 set -e 带出了"
    FAILED=1
  fi
  if printf '%s\n' "$RO_OUT" | grep -q "sudo chown -R"; then
    echo "  [PASS] 诊断里带了可执行的修复命令"
  else
    echo "  [FAIL] 诊断里没有修复命令"
    FAILED=1
  fi

  umount "$RO_DATA" 2>/dev/null
  userdel daidai 2>/dev/null; groupdel daidai 2>/dev/null
else
  echo "  [SKIP] 无法挂 tmpfs 到临时目录，跳过只读数据目录用例"
fi

# ---- 7. 完全不设 PUID：历史部署必须零影响 ----------------------------------
run_case "不设 PUID/PGID（历史默认）"
expect_rc0
expect "仍以 root 运行" "SERVER uid=0 gid=0"
if [ -e "$LAST_DATA/.home" ]; then
  echo "  [FAIL] 不设 PUID 时不该创建 .home，历史部署应零影响"
  FAILED=1
else
  echo "  [PASS] 不设 PUID 时没有创建 .home"
fi

echo "=================================================================="
if [ "$FAILED" = "0" ]; then
  echo "ALL PASS"
else
  echo "SOME CHECKS FAILED"
fi
exit $FAILED
