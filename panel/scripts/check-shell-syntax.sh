#!/bin/bash
##############################################################################
# Shell 语法门禁
#
# 为什么需要：仓库里的 shell（docker/entrypoint.sh、Magisk/*.sh）加起来上千行，
# 而它们【一行都不进 go test】—— 少一个 fi、多一个引号，`go test ./...` 照样全绿，
# CI 全绿，用户刷进去之后开机脚本从错误点开始整段不执行。
# Magisk 的脚本尤其危险：错误信息只会出现在管理器的安装日志里，且经常被 2>/dev/null 吞掉。
#
# 检查三件事：
#   1. 每个 .sh 过 `bash -n`；
#   2. shebang 是 /bin/sh 的再过一遍 `dash -n` —— Alpine 上真正解析 entrypoint.sh 的是
#      busybox ash，bash -n 是超集，过了不代表 POSIX sh 也过；
#   3. `ruri ... -c '<脚本>'` 这种【以字符串形式传进容器执行】的内联脚本单独抽出来检查。
#      它们对整文件的 bash -n 是透明的（就是个普通字符串），写错只会被 2>/dev/null 吞掉，
#      表现成「探测没有任何输出」。
#
# 用法：bash scripts/check-shell-syntax.sh
##############################################################################

set -u

# 默认按脚本自身位置定位仓库根；DAIDAI_REPO_ROOT 用于「把脚本复制到别处再跑」的场景
# （Windows 检出的 .sh 是 CRLF，本地验证时通常要先复制一份去掉 CR 再执行）。
cd "${DAIDAI_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}" || exit 1
FAILED=0
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Windows 检出可能是 CRLF（.gitattributes 里 *.sh 是 eol=lf，发布物一定是 LF，
# Magisk/build.sh 还会 tr -d '\r'）。检查的对象必须是去掉 CR 之后的那一份。
strip_cr() { tr -d '\r' < "$1" > "$2"; }

check_one() {
  local file="$1" shell="$2" label="$3"
  local out
  out=$("$shell" -n "$file" 2>&1)
  if [ -z "$out" ]; then
    printf '  %-8s OK\n' "$label"
    return 0
  fi
  printf '  %-8s FAIL\n' "$label"
  printf '%s\n' "$out" | sed 's/^/      /'
  FAILED=1
  return 1
}

echo "==== 1. 整文件语法 ===="
# 用 git 列文件而不是 find：自动跳过 node_modules / dist 等被忽略的目录。
# --others --exclude-standard 把「还没提交的新脚本」也算进来 ——
# 否则本地新写一个 .sh 时这道门禁看不见它，等提交完才生效就晚了。
FILES=$(git ls-files --cached --others --exclude-standard '*.sh' 2>/dev/null)
if [ -z "$FILES" ]; then
  echo "!! 没有找到任何被跟踪的 .sh，检查本身有问题"
  exit 1
fi

for f in $FILES; do
  echo "$f"
  clean="$TMP/$(printf '%s' "$f" | tr '/' '_')"
  strip_cr "$f" "$clean"
  check_one "$clean" bash "bash"
  # shebang 是 /bin/sh 的必须同时过 POSIX sh
  if head -n1 "$clean" | grep -qE '^#!/bin/sh( |$)'; then
    if command -v dash >/dev/null 2>&1; then
      check_one "$clean" dash "dash"
    else
      echo "  dash     SKIP（未安装）"
    fi
  fi
done

echo
echo "==== 2. 进容器执行的内联脚本（ruri ... -c '...'）===="
# 抽取规则：以 `-c '` 结尾的行开始，到只含收尾单引号的行为止。
INLINE_COUNT=0
for f in $FILES; do
  clean="$TMP/$(printf '%s' "$f" | tr '/' '_')"
  prefix="$TMP/inline_$(printf '%s' "$f" | tr '/' '_')"
  awk -v prefix="$prefix" '
    /-c '"'"'$/ { inblk = 1; n++; fn = sprintf("%s-%d.sh", prefix, n); next }
    inblk && /^[[:space:]]*'"'"'/ { inblk = 0; close(fn); next }
    inblk { print > fn }
  ' "$clean"
done

for blk in "$TMP"/inline_*-*.sh; do
  [ -f "$blk" ] || continue
  INLINE_COUNT=$((INLINE_COUNT + 1))
  echo "$(basename "$blk")（$(wc -l < "$blk") 行）"
  check_one "$blk" bash "bash"
  if command -v dash >/dev/null 2>&1; then
    check_one "$blk" dash "dash"
  fi
done

echo "共检查 $INLINE_COUNT 段内联脚本"
# 这几段是 Magisk 安装/状态自检的核心，抽不到就说明抽取规则失配、本检查已经空转
if [ "$INLINE_COUNT" -lt 4 ]; then
  echo "!! 内联脚本抽到的段数少于 4，抽取规则多半已经失配，本检查不再可信"
  FAILED=1
fi

echo
echo "==== 3. heredoc 里的容器脚本 ===="
# Magisk/service.sh 与 customize.sh 用 heredoc 把「进容器执行的脚本」写成文件。
# 那几段加起来几百行，对整文件的 bash -n 同样是透明的（就是一坨字符串），
# 而它们才是每次开机真正跑的东西 —— 少一个 fi，面板与 SSH 一起消失。
extract_heredoc() {  # $1=清洗过的源文件 $2=起始标记 $3=输出路径
  awk -v marker="$2" '
    index($0, "<< \x27" marker "\x27") { f = 1; next }
    $0 == marker { if (f) exit }
    f { print }
  ' "$1" > "$3"
}

HEREDOC_COUNT=0
# 第 4 个参数声明这段脚本在容器里由谁解释，决定跑哪几道检查：
#   posix —— 由 busybox ash 解释（Alpine），或两个 flavor 都会跑到（追加进两边装依赖脚本的公共段、
#            开机脚本正文），bash / dash / ash 三道都要过；
#   bash  —— 只在 Debian 容器里由 /bin/bash 解释（customize.sh 给 debian flavor 选的 $CTR_SHELL），
#            只过 bash -n。这种段落里写 [[ ]]、数组、${var//a/b} 之类 bash 专有语法是合法的，
#            再让 dash / ash 去解析只会误报，逼着人把正确的脚本改坏。
# bash 模式会少跑两道检查，所以它必须和段落自己的 shebang（#!/bin/bash）对得上：
# 对不上说明这段的解释器已经变了，门禁却还按旧假设在跳过检查。
check_heredoc() {  # $1=源文件 $2=标记 $3=最少行数 $4=posix|bash
  local clean out mode="$4"
  clean="$TMP/$(printf '%s' "$1" | tr '/' '_')"
  out="$TMP/heredoc_$2.sh"
  extract_heredoc "$clean" "$2" "$out"
  local n; n=$(wc -l < "$out")
  echo "$1 :: $2（$n 行，$mode）"
  if [ "$n" -lt "$3" ]; then
    echo "  抽到 $n 行，少于预期的 $3 行 —— 抽取规则失配，本检查已空转"
    FAILED=1
    return 1
  fi
  HEREDOC_COUNT=$((HEREDOC_COUNT + 1))
  check_one "$out" bash "bash"
  case "$mode" in
    posix) ;;
    bash)
      local shebang; shebang=$(head -n1 "$out")
      if [ "$shebang" != "#!/bin/bash" ]; then
        printf '  %-8s FAIL\n' "shebang"
        echo "      声明为 bash 专用段，但首行是「$shebang」而不是 #!/bin/bash —— 解释器假设已失效，"
        echo "      请确认它在容器里到底由谁解释，再改这里的声明（posix 段要多过 dash / ash 两道）"
        FAILED=1
        return 1
      fi
      echo "  dash/ash SKIP（#!/bin/bash 段，只由 Debian 容器的 bash 解释）"
      return 0
      ;;
    *)
      echo "  !! check_heredoc 的第 4 个参数只能是 posix 或 bash，实际是「$mode」"
      FAILED=1
      return 1
      ;;
  esac
  if command -v dash >/dev/null 2>&1; then
    check_one "$out" dash "dash"
  fi
  # Alpine 容器里真正解析它的是 busybox 的 ash，有就多验一道
  if command -v busybox >/dev/null 2>&1; then
    local o; o=$(busybox sh -n "$out" 2>&1)
    if [ -z "$o" ]; then
      printf '  %-8s OK\n' "ash"
    else
      printf '  %-8s FAIL\n' "ash"
      printf '%s\n' "$o" | sed 's/^/      /'
      FAILED=1
    fi
  fi
}

check_heredoc Magisk/service.sh   CONTAINER_EOF        100 posix
check_heredoc Magisk/customize.sh DEPS_COMMON_EOF       30 posix
check_heredoc Magisk/customize.sh DEPS_PKG_ALPINE_EOF    5 posix
# Debian 装依赖脚本（apt 加固 / 镜像源回退 / 批量装包 / Node 官方二进制下载）约 195 行。
# 下限取 120：抽取规则一旦失配（起始标记改写成 <<'X'、改名）抽到的是 0 行，远低于它；
# 删几段注释、挪一两个步骤又不至于误报。
check_heredoc Magisk/customize.sh DEPS_PKG_DEBIAN_EOF  120 bash

echo "共检查 $HEREDOC_COUNT 段容器脚本"

echo
if [ "$FAILED" = "0" ]; then
  echo "ALL PASS"
else
  echo "SOME CHECKS FAILED"
fi
exit $FAILED
