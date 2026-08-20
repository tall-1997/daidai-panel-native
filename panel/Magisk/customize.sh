#!/system/bin/sh
##########################################################################
# 呆呆面板 Magisk / KernelSU / APatch 模块安装脚本
#
# 方案：借鉴 v2.0.5 的容器方案
#   1. 释放 rurima (静态 arm64) 到 /system/bin （由 Magisk 魔挂）
#   2. 按 flavor 下载 rootfs 解压到 rootfs 目录
#        alpine —— Alpine 3.18 minirootfs（NJU 镜像站，musl）
#        debian —— CI 自建的 Debian bookworm 精简 rootfs（Release 资产，glibc）
#   3. 通过 rurima ruri 进入容器，用 apk / apt-get 安装 python3 / nodejs / npm / git / curl / bash 等
#   4. 面板后端 daidai-server (CGO_ENABLED=0 静态 Go 二进制) 放进容器 /usr/local/bin/
#   5. 运行时由 service.sh 通过 rurima ruri 进入容器启动 daidai-server，
#      单端口 5700 由 daidai-server 直接托管 API + 前端静态文件 (web_dir)
##########################################################################

SKIPUNZIP=0
REPLACE=""

# ---- 基础变量 ------------------------------------------------------------
export PATH=/data/adb/ap/bin:/data/adb/ksu/bin:/data/adb/magisk:$PATH:$MODPATH/system/bin

# rootfs 优先使用 /data/daidai（若历史已存在），否则 /data/local/daidai
export rootfs=/data/local/daidai
if [ -d "/data/daidai" ]; then
  export rootfs=/data/daidai
fi

MODID=daidai-panel
PERSIST_DIR=/data/adb/$MODID
UPDATE_FLAG="$PERSIST_DIR/.updated_from"
INSTALL_BACKUP_DIR="$PERSIST_DIR/update-data-backup"
INSTALL_IN_PROGRESS_FLAG="$PERSIST_DIR/.install_in_progress"
INSTALL_BACKUP_READY=0

# 容器内关键运行时全部验证通过后才置 1。
# 只有它等于 1 才允许打印「安装完成！」——见文件末尾的收尾段。
INSTALL_DEPS_OK=0

# ---- flavor（容器基础系统）----------------------------------------------
# ZIP 根目录带一个 flavor 文件（由 build.sh 写入），内容是单行 alpine 或 debian。
# 读不到 / 内容不认识时一律回落 alpine：老 ZIP 里没有这个文件，手工打包也可能漏掉，
# 这时候必须还是原来的行为，而不是装不上。
#
# 为什么不复制出两份 customize.sh：这个脚本里有 rurima 存在性检查、容器能力探测、
# 依赖装完验证、「安装完成！」门禁四道安全网。复制一份意味着以后每改一处都要改
# 两遍，漏一遍就是某个 flavor 静默退化，而且是「装完了才发现用不了」这种最难查的退化。
FLAVOR=alpine
if [ -f "$MODPATH/flavor" ]; then
  # 用 read + 前缀匹配，不用 tr / cat：不依赖任何外部命令是否被 busybox 收录，
  # 前缀匹配还能天然吃掉文件尾部可能带的 CR（Windows 上手工改过这个文件的话）。
  # 只认 debian，其余一律 alpine —— 默认值就是安全值。
  # `|| true`：文件末尾没有换行时 read 会返回非 0（但变量已赋好值），
  # 万一将来 install_module 在 set -e 下 source 本脚本，也不至于莫名其妙中断。
  read -r flavor_raw < "$MODPATH/flavor" 2>/dev/null || true
  case "$flavor_raw" in
    debian*) FLAVOR=debian ;;
    *) FLAVOR=alpine ;;
  esac
fi

# CTR_SHELL 必须贯穿【所有】进容器的调用：能力探测、装依赖、依赖验证。
# Debian 里根本没有 /bin/ash —— 任何一处漏改，表现都是「Debian 版永远探测失败、
# 装不上」，而根因藏在探测命令本身里，从报错完全看不出来。
if [ "$FLAVOR" = "debian" ]; then
  CTR_SHELL=/bin/bash
  CTR_NAME="Debian bookworm"
  CTR_PKG_TOOL="apt-get"
  # 装依赖失败时的报错文案要说清「从哪儿下」：Debian 侧现在有一串镜像源回退，
  # 只写 NJU 会让用户误以为换个源就能好，其实四个都试过了。
  CTR_PKG_SOURCE="镜像站（NJU / TUNA / 阿里云 / Debian 官方，按序自动回退）"
  CTR_DEPS_SIZE="约 300MB"
  CTR_BASHRC="/etc/bash.bashrc"
  # rootfs 与面板版本没有任何耦合，所以用 releases/latest/download/ 这个固定跳转地址，
  # 不拼版本号：URL 可硬编码，本地构建一个尚未发版的版本也照样装得上。
  ROOTFS_URL="https://github.com/linzixuanzz/daidai-panel/releases/latest/download/daidai-debian-rootfs-arm64.tar.gz"
else
  CTR_SHELL=/bin/ash
  CTR_NAME="Alpine 3.18"
  CTR_PKG_TOOL="apk"
  # Alpine 侧仍然只有 NJU 一个源，文案与改动前逐字一致
  CTR_PKG_SOURCE="mirrors.nju.edu.cn"
  CTR_DEPS_SIZE="约 50MB"
  CTR_BASHRC="/etc/bash/bashrc"
  ROOTFS_URL="https://mirrors.nju.edu.cn/alpine/v3.18/releases/aarch64/alpine-minirootfs-3.18.9-aarch64.tar.gz"
fi

# 安装中途 abort 时告诉用户：已备份的数据还在，下次安装会自动恢复。
# 两个条件都要满足才提示：
#   1. 备份真的完成（INSTALL_BACKUP_READY=1）——否则会在「压根没有旧数据」的
#      全新安装场景下凭空吓人；
#   2. 备份目录此刻确实还在——数据回填成功后这个目录会被删掉，那之后再提示
#      就是指向一个不存在的路径。
warn_backup_preserved() {
  if [ "$INSTALL_BACKUP_READY" = "1" ] && [ -d "$INSTALL_BACKUP_DIR" ]; then
    ui_print "!"
    ui_print "! 你的面板数据仍完整保留在:"
    ui_print "!   $INSTALL_BACKUP_DIR"
    ui_print "! 下次安装本模块时会自动恢复，请勿手动删除该目录。"
  fi
}

# ---- 环境探测 ------------------------------------------------------------
detect_ksu() { [ -d "/data/adb/ksu" ]; }

get_current_version() {
  # 已启用模块的 module.prop —— 按 Magisk / KernelSU / APatch 常见路径依次查找
  for candidate in \
    "/data/adb/modules/$MODID/module.prop" \
    "/data/adb/ksu/modules/$MODID/module.prop" \
    "/data/adb/ap/modules/$MODID/module.prop" \
    "$PERSIST_DIR/module.prop"
  do
    if [ -f "$candidate" ]; then
      grep '^versionCode=' "$candidate" 2>/dev/null | cut -d'=' -f2
      return
    fi
  done
  echo "0"
}

# ---- 架构检查 ------------------------------------------------------------
# 只放行 arm64。容器运行时 system/bin/rurima 是 AArch64 静态二进制
# （ELF64 EXEC, e_machine=0xb7），随包的离线 apk 也都是 arch=aarch64。
# 之前这里同时放行 x64，结果是 x86_64 设备能通过检查、能选中 amd64 后端，
# 然后在 exec rurima 时失败 —— 表现为"装完了但用不了"。宁可明确说不支持。
if [ "$ARCH" = "x64" ] || [ "$ARCH" = "x86_64" ]; then
  ui_print "! 检测到 x86_64 (x64) 设备，本模块暂不支持该架构"
  ui_print "!"
  ui_print "! 原因：模块自带的容器运行时 rurima 目前只有 aarch64 构建，"
  ui_print "! 在 x86_64 上根本无法执行，装上去也起不来。"
  ui_print "!"
  ui_print "! 这不是你下错了包 —— 发布的 ZIP 里就没有 x86_64 的容器运行时，"
  ui_print "! 换哪个版本、哪个下载源都一样。"
  abort "! 安装已中止（未对设备上的任何数据做改动）"
fi
if [ "$ARCH" != "arm64" ]; then
  abort "! 当前仅支持 arm64 (aarch64)，设备架构 $ARCH 暂不支持"
fi

# 硬闸门只挡到 Android 6.0 (API 23)。
# 更高的 API 24 门槛当初是保守猜的，仓库里找不到任何技术上真正需要它的东西：
# 模块走的是 chroot 模式（见下面能力探测处的说明），不依赖 user namespace。
# 真正的准入判据是 rootfs 解压后的「容器能力探测」，不是这里的版本号。
if [ "$API" -lt 23 ]; then
  abort "! 要求 Android 6.0 (API 23) 及以上，当前 API=$API"
fi
if [ "$API" -lt 26 ]; then
  ui_print "! 注意：Android 6.x / 7.x 属于「可以尝试」而不是「保证可用」"
  ui_print "! 部分机型受 SELinux 策略 / 内核挂载限制无法启动容器；"
  ui_print "! 安装过程中会实际探测一次，起不来会当场中止并说明原因。"
fi

# ---- 挑选 daidai-server 二进制 ------------------------------------------
# 架构检查已经保证只剩 arm64。build.sh 仍保留 amd64 / all 构建能力（属于构建
# 基建，将来真有了 x86_64 的 rurima 只需改回 CI 参数和上面的架构检查），
# 所以下面照旧清理可能存在的 amd64 产物。
BIN_SUFFIX="arm64"

if [ ! -f "$MODPATH/system/bin/daidai-server-${BIN_SUFFIX}" ]; then
  abort "! 模块包缺少 system/bin/daidai-server-${BIN_SUFFIX}，无法安装"
fi

# ---- 容器运行时前置检查 --------------------------------------------------
# rurima 是本模块的地基：停旧容器、装依赖、能力探测、收尾卸载全靠它。
# 检查必须放在【第一次使用之前】——下面「停止运行中的容器」那段就已经在调它了。
# 以前这里不做任何检查，直接 chmod +x 再调用，rurima 缺失时所有调用静默失败
# （都带 2>/dev/null || true），一路走到"安装完成"，用户重启后才发现面板起不来。
RURIMA="$MODPATH/system/bin/rurima"
if [ ! -f "$RURIMA" ]; then
  ui_print "! 模块包缺少容器运行时: system/bin/rurima"
  ui_print "!"
  ui_print "! 没有它就无法创建 ${CTR_NAME} 容器，面板不可能运行。"
  ui_print "! 多半是 ZIP 下载不完整，或被解压 / 重新打包工具破坏了。"
  ui_print "! 请从 GitHub Release 重新完整下载 daidai-panel-magisk ZIP 后再装。"
  abort "! 安装已中止（未对设备上的任何数据做改动）"
fi
chmod +x "$RURIMA" 2>/dev/null
if [ ! -x "$RURIMA" ]; then
  ui_print "! system/bin/rurima 存在，但无法赋予可执行权限"
  ui_print "! 请确认 /data 分区未以 noexec 挂载，或换用其他管理器重试。"
  abort "! 安装已中止（未对设备上的任何数据做改动）"
fi

mv -f "$MODPATH/system/bin/daidai-server-${BIN_SUFFIX}" "$MODPATH/system/bin/daidai-server"
[ -f "$MODPATH/system/bin/daidai-server-arm64" ] && rm -f "$MODPATH/system/bin/daidai-server-arm64"
[ -f "$MODPATH/system/bin/daidai-server-amd64" ] && rm -f "$MODPATH/system/bin/daidai-server-amd64"

# ddp CLI（如果有）
if [ -f "$MODPATH/system/bin/ddp-${BIN_SUFFIX}" ]; then
  mv -f "$MODPATH/system/bin/ddp-${BIN_SUFFIX}" "$MODPATH/system/bin/ddp"
fi
[ -f "$MODPATH/system/bin/ddp-arm64" ] && rm -f "$MODPATH/system/bin/ddp-arm64"
[ -f "$MODPATH/system/bin/ddp-amd64" ] && rm -f "$MODPATH/system/bin/ddp-amd64"

set_perm_recursive $MODPATH/system/bin 0 2000 0755 0755

# ---- 打印安装信息 -------------------------------------------------------
if detect_ksu; then
  ui_print "- 检测到 KernelSU 环境"
else
  ui_print "- 检测到 Magisk 环境"
fi

ui_print ""
ui_print "------------呆呆面板安装环境----------"
ui_print "容器基础系统：$CTR_NAME ($FLAVOR)"
ui_print "设备：$(getprop ro.product.model)"
ui_print "系统版本：$(getprop ro.build.version.release)"
ui_print "安卓版本：$(getprop ro.build.version.sdk)"
if [ -f "/data/adb/ksu/kernel/version" ]; then
  ui_print "KernelSU版本：$(cat /data/adb/ksu/kernel/version)"
else
  ui_print "Magisk版本：$(cat /data/adb/magisk/version 2>/dev/null || echo 'N/A')"
fi
ui_print "-------------------------------------"
ui_print ""

# ---- 保留用户数据（升级 / 重装 / 降级均保护） ----------------------------
current_ver=$(get_current_version)
new_ver=$(grep '^versionCode=' $MODPATH/module.prop 2>/dev/null | cut -d'=' -f2)

# 如果上次更新在清理 rootfs 之后中断，完整数据只会留在持久化备份目录里。
# 这种情况下不能重新从半成品 rootfs 备份，必须优先沿用上次留下的完整备份。
if [ -d "$INSTALL_BACKUP_DIR" ]; then
  backup_count=$(ls -1 "$INSTALL_BACKUP_DIR/" 2>/dev/null | wc -l)
  if [ "$backup_count" -gt 0 ]; then
    if [ -f "$INSTALL_IN_PROGRESS_FLAG" ] || [ ! -d "$rootfs/app/Dumb-Panel" ]; then
      INSTALL_BACKUP_READY=1
      ui_print "- 检测到上次安装中断留下的数据备份"
      ui_print "- 本次安装完成后会自动恢复 $INSTALL_BACKUP_DIR ($backup_count 项)"
    fi
  else
    rm -rf "$INSTALL_BACKUP_DIR" 2>/dev/null
  fi
fi

if [ "$INSTALL_BACKUP_READY" != "1" ] && [ -d "$rootfs/app/Dumb-Panel" ]; then
  if [ "$current_ver" != "0" ] && [ "$current_ver" != "$new_ver" ] 2>/dev/null; then
    ui_print "- 检测到版本变更: $current_ver -> $new_ver"
  else
    ui_print "- 检测到已有面板数据"
  fi
  ui_print "- 正在保留用户数据..."
  mkdir -p "$PERSIST_DIR" || abort "! 无法创建持久化目录 $PERSIST_DIR"
  if [ -e "$INSTALL_BACKUP_DIR" ]; then
    rm -rf "$INSTALL_BACKUP_DIR" 2>/dev/null || abort "! 无法清理旧的数据备份目录 $INSTALL_BACKUP_DIR"
  fi
  mkdir -p "$INSTALL_BACKUP_DIR" || abort "! 无法创建数据备份目录 $INSTALL_BACKUP_DIR"
  # Magisk 安装器的 TMPDIR 经常落在 /dev/tmp，老设备空间很小；完整用户数据改放 /data/adb 持久化目录，避免更新时因 /dev/tmp 不足失败。
  if ! cp -rf "$rootfs/app/Dumb-Panel/." "$INSTALL_BACKUP_DIR/" 2>/dev/null; then
    abort "! 用户数据备份失败（$INSTALL_BACKUP_DIR 所在 /data 空间可能不足），已中止安装以保护数据"
  fi
  backup_count=$(ls -1 "$INSTALL_BACKUP_DIR/" 2>/dev/null | wc -l)
  if [ "$backup_count" -eq 0 ]; then
    abort "! 数据备份目录为空，可能复制失败，已中止安装以保护数据"
  fi
  INSTALL_BACKUP_READY=1
  ui_print "- 数据已备份到 $INSTALL_BACKUP_DIR ($backup_count 项)"
  echo "$current_ver" > "$UPDATE_FLAG"

  # ---- 持久化"上次更新前快照"：模块每次更新都会重写 $MODPATH，但 $PERSIST_DIR
  # 不会被 Magisk 触碰。把关键数据镜像一份到这里，下次升级前清空重写——
  # 即使安装中途出错 / 数据被回填覆盖 / 用户手滑误删 rootfs，仍能从这里翻回最近一次的状态。
  # 体积大的 logs/ deps/ 不备份（可重建，且会让备份动辄上 GB）。
  PERSIST_BACKUP_DIR="$PERSIST_DIR/last-update-backup"
  PERSIST_BACKUP_PREV="$PERSIST_DIR/last-update-backup.prev"
  ui_print "- 同步持久化快照到 $PERSIST_BACKUP_DIR ..."
  # 原子切换：先把现有快照重命名为 .prev，新快照完整建好后再删 prev。
  # 避免新快照建到一半失败导致"两份都丢"。
  rm -rf "$PERSIST_BACKUP_PREV" 2>/dev/null
  if [ -d "$PERSIST_BACKUP_DIR" ]; then
    mv "$PERSIST_BACKUP_DIR" "$PERSIST_BACKUP_PREV" 2>/dev/null
  fi
  mkdir -p "$PERSIST_BACKUP_DIR"
  snapshot_items=0
  for item in daidai.db daidai.db-shm daidai.db-wal scripts backups .jwt_secret config.yaml panel.log; do
    src="$INSTALL_BACKUP_DIR/$item"
    if [ -e "$src" ]; then
      if cp -rf "$src" "$PERSIST_BACKUP_DIR/" 2>/dev/null; then
        snapshot_items=$((snapshot_items + 1))
      fi
    fi
  done
  snapshot_size=$(du -sh "$PERSIST_BACKUP_DIR" 2>/dev/null | awk '{print $1}')
  cat > "$PERSIST_BACKUP_DIR/BACKUP_INFO.txt" <<META
呆呆面板 - 上次更新前数据快照
================================================================
备份时间: $(date '+%Y-%m-%d %H:%M:%S')
源版本:   $current_ver
目标版本: $new_ver
源路径:   $rootfs/app/Dumb-Panel
项目数:   $snapshot_items
总大小:   ${snapshot_size:-?}

包含: daidai.db (+wal/-shm)、scripts/、backups/、.jwt_secret、config.yaml、panel.log
跳过: logs/、deps/（体积大且可重建，省存储空间）

恢复方法（任选其一）：
  方式 A —— 一键脚本：
    su -c "sh $PERSIST_DIR/restore-last-update.sh"

  方式 B —— 手动：
    # 先停面板（点模块卡片的「运行 / Action」按钮，或用下面这条等价命令）。
    # 别用 pkill：存活守护 60 秒内就会把面板拉回来。
    su -c "sh /data/adb/modules/$MODID/action.sh"
    su -c "cp -rf $PERSIST_BACKUP_DIR/. $rootfs/app/Dumb-Panel/"
    # 再点一次动作按钮启动，或重启设备：
    su -c "sh /data/adb/modules/$MODID/action.sh"

⚠️ 注意：
  - 此快照在每次模块更新时会被清空重写，只保留"最近一次更新前"的版本
  - 卸载模块默认会一并删除此目录；如想保留，卸载前执行：
      su -c "touch $PERSIST_DIR/.keep_on_uninstall"
META
  # 新快照建好，可以安全删除上一份的 prev 副本
  rm -rf "$PERSIST_BACKUP_PREV" 2>/dev/null
  ui_print "- 持久化快照完成（$snapshot_items 项，约 ${snapshot_size:-?}）"
  ui_print "- 万一数据丢了：su -c \"sh $PERSIST_DIR/restore-last-update.sh\""
fi

# 极少数情况下 /data 挂载异常，提示用户重启后重试
if [ -e "$rootfs/sys/kernel" ] && [ "$current_ver" = "0" ]; then
  abort "- 请重启后再尝试安装！"
fi

# ---- 先让存活守护自退 ----------------------------------------------------
# 下面这段停容器的老写法只 pkill 了 daidai-server 和 ruri，**打不到守护子 shell**
# —— 它的 argv 继承自 service.sh。紧接着几行就要把整个 rootfs 删掉重装，
# 也就是说刷 zip 的这几分钟里，守护仍在每轮进容器抢同一个 rootfs。
#
# 收口方式和 action.sh 一致：写停止开关 + 删守护代次标记，任一条命中守护都会自退
# （最坏 10 秒，见 service.sh 的分片睡眠）。这里【不能】用 pkill -f service.sh：
# 那会误杀正在跑的 service.sh 自己。
#
# 无条件写（不放进下面的 `if [ -d "$rootfs" ]`）：rootfs 被手工删过、但守护还活着
# 的情况同样要收口。文件末尾的收尾段会无条件把停止开关删掉。
#
# 【已知限制，非漏洞】只有「清掉旧 rootfs」**之前**那条 abort（备份未完成）会把停止
# 开关撤掉，因为那条路径上 rootfs 还完好、面板本来能跑，留着开关等于永久停机。
# 删完 rootfs **之后**的 abort（rootfs 下载 / 解压失败、用户数据恢复失败、依赖验证
# 不通过）一律不撤：那时 rootfs 已经没了，service.sh 本来就会因为「找不到 rootfs」
# 直接退出，有没有停止开关都一样起不来；而下一次安装成功时，文件末尾的收尾段会
# 无条件把它清掉。所以这里刻意**不**加 `trap ... EXIT` 做跨 abort 的兜底清理：
# trap 与 abort/exit 的交互、以及误在成功路径上触发的风险，都大于这点收益。
#
# 注意：本段注释与下方代码都不要再出现「删 rootfs 那条命令」的字面写法 ——
# magisk_assets_test.go 用 strings.Index 找它的**首次出现**来断言
# 「先写停止开关、再删 rootfs」的先后顺序，注释里复述一遍会让那条断言假阳性。
mkdir -p "$PERSIST_DIR" 2>/dev/null
printf '%s\n' "stopped by customize.sh at $(date '+%Y-%m-%d %H:%M:%S')" > "$PERSIST_DIR/stopped" 2>/dev/null
rm -f "$PERSIST_DIR/watchdog.gen" 2>/dev/null
ui_print "- 正在等待存活守护退出..."
sleep 12

# ---- 停止运行中的容器，防止 rm -rf 因活跃挂载点导致安装器闪退 ------------
if [ -d "$rootfs" ]; then
  # $RURIMA 已在上面做过存在性 + 可执行性检查
  "$RURIMA" ruri -w -U "$rootfs" 2>/dev/null || true
  pkill -f daidai-server 2>/dev/null || true
  pkill -f "ruri.*$rootfs" 2>/dev/null || true
  sleep 1
  cat /proc/mounts 2>/dev/null | awk -v r="$rootfs" '$2 ~ r {print $2}' | sort -r | \
    while read -r mp; do
      umount -l "$mp" 2>/dev/null || true
    done
fi

# ---- 清掉旧 rootfs 重装 -------------------------------------------------
# 安全检查：如果面板数据存在但备份未完成，禁止继续
if [ -d "$rootfs/app/Dumb-Panel" ] && [ "$INSTALL_BACKUP_READY" != "1" ]; then
  # 这条路径上 rootfs 还完好，用户原来的面板本来是能跑的。
  # 上面为了让守护自退而写下的停止开关必须在这里撤掉，
  # 否则「中止一次安装」= 永久停机（重启也不会自动起来），而且完全没有线索。
  rm -f "$PERSIST_DIR/stopped" 2>/dev/null
  abort "! 面板数据存在但未成功备份，已中止安装以保护数据。请重试或手动备份 $rootfs/app/Dumb-Panel"
fi
if [ "$INSTALL_BACKUP_READY" = "1" ]; then
  echo "$new_ver" > "$INSTALL_IN_PROGRESS_FLAG" 2>/dev/null || true
fi
rm -rf "$rootfs"

ui_print "- 请勿切换到后台，避免下载失败！"
ui_print "- 正在联网下载 ${CTR_NAME} rootfs..."

# 架构检查已保证只剩 arm64，两个 flavor 的 rootfs 也都只有 aarch64 版本，
# 具体 URL 在文件顶部的 flavor 分支里定好了。
#
# 注意这里旧 rootfs 已经被 rm -rf 了，此后任何失败都必须提醒用户备份还在，
# 否则用户看到的就是"面板没了、也不知道数据还在不在"。
if ! busybox wget --no-check-certificate -O $TMPDIR/rootfs.tar.gz "$ROOTFS_URL"; then
  ui_print "! ${CTR_NAME} rootfs 下载失败"
  ui_print "! 下载地址: $ROOTFS_URL"
  if [ "$FLAVOR" = "debian" ]; then
    ui_print "!"
    ui_print "! Debian 版的 rootfs（约 27MB）放在 GitHub Release 上，且要跟一次 302"
    ui_print "! 跳转，国内直连经常不稳定。可以挂代理 / 换网络后重试，"
    ui_print "! 或者改装 Alpine 版（rootfs 走国内镜像站，只有 3MB）。"
  fi
  warn_backup_preserved
  abort "! 安装已中止：rootfs 下载失败，请检查网络后重试"
fi

mkdir -p $rootfs
if ! tar -xf $TMPDIR/rootfs.tar.gz -C $rootfs; then
  ui_print "! ${CTR_NAME} rootfs 解压失败"
  ui_print "! 多半是下载被截断（存储空间不足 / 中途断网），也可能是 tar 格式不兼容。"
  warn_backup_preserved
  abort "! 安装已中止：rootfs 解压失败"
fi

# 离线 apk（linux-pam / shadow）塞进容器 /tmp —— 只有 Alpine 需要：
# 随包的这两个 apk 是 aarch64 Alpine 专用，Debian 侧的同等能力（passwd / libpam）
# 由 apt 直接提供，build.sh 打 debian 包时也根本不会拷 apk/ 目录进来。
if [ "$FLAVOR" = "alpine" ]; then
  mv $MODPATH/apk $rootfs/tmp 2>/dev/null
fi
rm -rf $MODPATH/apk 2>/dev/null
rm -f $MODPATH/rootfs.tar.gz 2>/dev/null

# ---- 容器能力探测 --------------------------------------------------------
# 这里才是真正的准入判据。Android 版本号只是代理指标，这台设备到底能不能起容器
# 只有试过才知道 —— 所以实际进一次容器，比对哨兵字符串。
#
# 位置很关键：必须在【装依赖之前】。装依赖要好几分钟且强依赖网络，先探测能省掉
# 用户的漫长等待，也能把「容器起不来」和「网络不通」这两类完全不同的失败区分开。
#
# 为什么不需要 user namespace（别因为"看起来多余"就删掉这段）：
# 模块调的是 ruri -p -N -S -A，从不传 -u (unshare) 也不传 -s (seccomp)，走的是
# chroot 模式。所以 Android 6 常见的内核 3.10 通常没开 CONFIG_USER_NS 并不构成
# 阻塞 —— 这正是硬闸门敢从 API 24 降到 23 的技术依据。
# 反过来，SELinux 策略和挂载限制无法靠静态分析排除（ruri 的挂载落在宿主全局
# mount namespace，上面那段 umount 循环就是证据），只能靠这次实际探测发现。
CONTAINER_PROBE_TOKEN="DAIDAI_CONTAINER_PROBE_OK"
CONTAINER_PROBE_ERR="$TMPDIR/container-probe.err"

ui_print "- 正在探测容器运行能力..."
# 容器内把哨兵拼接出来，令牌不会以完整形态出现在命令行里；
# 这样即使 ruri 把失败的命令行回显出来，也不会被误判成探测成功。
# stderr 单独收走，保证 probe_out 里只可能有容器真正 echo 出来的东西。
#
# 这里的 shell 【必须】走 $CTR_SHELL，不能写死 /bin/ash：Debian 里没有 ash，
# 写死的话 Debian 版会在这一步 100% 失败，而且报错完全指不到真正的原因。
probe_out=$("$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" -c 'echo "DAIDAI_CONTAINER""_PROBE_OK"' 2>"$CONTAINER_PROBE_ERR")

case "$probe_out" in
  *"$CONTAINER_PROBE_TOKEN"*)
    ui_print "- 容器可以正常启动"
    ;;
  *)
    ui_print "! 无法在本机启动 ${CTR_NAME} 容器，安装已中止"
    ui_print "!"
    ui_print "! 继续装下去也只会得到一个起不来的面板，所以在这里就停。"
    ui_print "! 常见原因（按可能性排序）："
    ui_print "!   1. SELinux 策略限制：试试 setenforce 0 后重装，"
    ui_print "!      能装上说明是策略问题（注意宽容模式会降低系统安全性）"
    ui_print "!   2. 内核不支持所需的挂载 / chroot 操作，常见于魔改内核或"
    ui_print "!      过老的设备，这种情况本模块无解"
    ui_print "!   3. root 方案版本过旧：升级 Magisk / KernelSU / APatch 后重试"
    # 报错可能落在 stderr，也可能被 ruri 打到 stdout，两边都捞一下，
    # 免得用户拿到一句"起不来"却没有任何可以搜索的原文。
    if [ ! -s "$CONTAINER_PROBE_ERR" ] && [ -n "$probe_out" ]; then
      printf '%s\n' "$probe_out" > "$CONTAINER_PROBE_ERR" 2>/dev/null
    fi
    if [ -s "$CONTAINER_PROBE_ERR" ]; then
      ui_print "!"
      ui_print "! rurima 报错原文（最多 5 行）："
      head -n 5 "$CONTAINER_PROBE_ERR" 2>/dev/null | while IFS= read -r probe_line; do
        ui_print "!   $probe_line"
      done
    fi
    warn_backup_preserved
    abort "! 安装已中止：容器能力探测未通过"
    ;;
esac

ui_print "- 正在联网安装面板运行依赖..."

# ---- DNS / hosts 准备 ----------------------------------------------------
# hosts 两个 flavor 都照抄宿主的一份，与改动前逐字一致，不随 flavor 分叉。
cp /system/etc/hosts $rootfs/etc/ 2>/dev/null

# resolv.conf 【必须】按 flavor 分叉写，这不是风格问题，是 glibc 与 musl 的
# 解析语义根本不同，同一份 resolv.conf 在两边的效果是相反的：
#
#   glibc（Debian）：按 resolv.conf 里的顺序【串行】查，第一个不通就等超时再换下一个。
#     所以多写几条纯粹是兜底，写得越全越稳，最坏情况只是慢一点。
#
#   musl（Alpine）：对【所有】 nameserver 并行发查询，谁先回就采信谁，而且把
#     NXDOMAIN 也当成「确定性应答」直接接受、不再等其他解析器。这意味着一旦把宿主
#     net.dns（校园网 / 企业网 / Captive Portal 的强制解析器）塞进来，只要它抢先回
#     一个 NXDOMAIN，musl 就直接判定域名不存在 —— 本来装得上的网络会开始装不上。
#
# Alpine 是目前唯一被真机证实可用的 flavor，回归面太大，所以它保持改动前那条写死的
# 单一 DNS，一个字都不动。⚠️ 后人别看到两边不一样就「顺手统一」把 Debian 这套合回
# Alpine —— 那是回归，不是清理。
if [ "$FLAVOR" = "debian" ]; then
  # 原来这里两个 flavor 都只写死一条 `nameserver 223.5.5.5`，是 Debian 版
  # 「装依赖时容器内 DNS 全挂」（apt 报 Temporary failure resolving）最可疑的一环，
  # 有两条独立的失败路径：
  #   1. 223.5.5.5 本身不可达 —— 校园网 / 企业网强制 DNS、Captive Portal、部分 APN
  #      屏蔽对外 53 端口。宿主自己从不走这条路（走的是 netd 的真实 DNS），所以
  #      「手机能上网」完全不代表容器里这条 DNS 能用。
  #   2. 没有 options single-request-reopen —— glibc 默认把 A / AAAA 两条查询用
  #      同一个源端口并发发出，不少家用路由 / 运营商 DNS 只回一条就丢另一条，
  #      glibc 只能等到超时重试，最终报的就是 EAI_AGAIN（正是那句 Temporary failure）。
  #      musl 不这么干，所以 Alpine 版一直没事 —— 这条能解释「同样的网络只有 Debian 挂」。
  # 两条对 glibc 都是「加上没坏处、不加可能致命」，所以不等真机结论就一起上。
  #
  # glibc 的 MAXNS 是 3，写再多也只有前 3 条会被用到。
  # 所以先取宿主 net.dns*（真实网络的 DNS，校园网 / 企业网强制 DNS 场景只有它能用），
  # 但最多取 2 条，保证至少还能留一个公共 DNS 兜底；不够 3 条再用公共 DNS 补齐。
  # net.dns* 在 Android 8+ 上通常是空的（netd 不再回写这几个 prop），那样就等价于
  # 只用公共 DNS —— 与改动前的行为一致，不会让本来能装的设备变得装不上。
  : > $rootfs/etc/resolv.conf
  dns_written=0
  dns_seen=""
  add_nameserver() {
    # $1 = 待写入的 DNS 地址；空值 / 重复 / 已满 3 条一律跳过
    [ -n "$1" ] || return 0
    [ "$dns_written" -lt 3 ] || return 0
    case " $dns_seen " in
      *" $1 "*) return 0 ;;
    esac
    echo "nameserver $1" >> $rootfs/etc/resolv.conf
    dns_seen="$dns_seen $1"
    dns_written=$((dns_written + 1))
  }
  host_dns_used=0
  for p in net.dns1 net.dns2 net.dns3 net.dns4; do
    [ "$host_dns_used" -lt 2 ] || break
    v=$(getprop "$p" 2>/dev/null)
    # 只收 IPv4/IPv6 字面量，prop 里偶尔会是空串或占位符
    case "$v" in
      ""|"0.0.0.0"|"::") continue ;;
    esac
    before="$dns_written"
    add_nameserver "$v"
    [ "$dns_written" -gt "$before" ] && host_dns_used=$((host_dns_used + 1))
  done
  for d in 223.5.5.5 119.29.29.29 8.8.8.8; do
    add_nameserver "$d"
  done
  # single-request-reopen 是这次的重点；timeout/attempts 只是让失败来得快一点，
  # 免得 apt 在一个不通的 DNS 上卡满默认的 5 秒 × 2 轮。
  echo 'options single-request-reopen timeout:2 attempts:3' >> $rootfs/etc/resolv.conf
  ui_print "- 容器 DNS: $(echo $dns_seen)"

  # nsswitch.conf 只在「文件存在但没有 hosts: 行」时才追加一行。
  # ⚠️ 绝不能 `> $rootfs/etc/nsswitch.conf` 整体覆盖：Debian 的这个文件由 base-files
  # 提供、本来就在，截断写会连 passwd: / group: / shadow: 一起删掉，
  # 直接搞坏紧随其后的 usermod / chpasswd 以及 service.sh 里的 adduser / sshd。
  #
  # 放进 Debian 分支而不是当公共代码：nsswitch.conf 是 glibc 的 NSS 机制，musl 压根
  # 不读这个文件（musl 没有 NSS），对 Alpine 是纯死代码；留在公共段只会平白多一处
  # 「可能动到 Alpine」的写操作。
  if [ -f "$rootfs/etc/nsswitch.conf" ] && ! grep -q '^hosts:' "$rootfs/etc/nsswitch.conf" 2>/dev/null; then
    echo 'hosts: files dns' >> "$rootfs/etc/nsswitch.conf"
  fi
else
  # Alpine：与改动前【逐字节一致】的单条写死，只有这一行。
  # 不读宿主 net.dns、不补公共 DNS、不写 options —— 理由见上面那段 musl 并行查询 +
  # 采信 NXDOMAIN 的说明。这是刻意保留的差异，不是漏改。
  echo "nameserver 223.5.5.5" > $rootfs/etc/resolv.conf
fi

# 装依赖脚本先落到容器 /tmp 再执行，不再直接 heredoc 喂给 shell 的 stdin。
# 这样「装包」这一段（唯一真正随 flavor 分叉的部分）可以单独写两份，
# 后面账号 / SSH / 镜像源 / 目录初始化那一大段两个 flavor 完全一致，
# 用追加(>>)的方式只保留一份 —— 不给两个 flavor 分叉留任何空间。
mkdir -p "$rootfs/tmp"
DEPS_SCRIPT="$rootfs/tmp/daidai-install-deps.sh"

if [ "$FLAVOR" = "debian" ]; then
  cat > "$DEPS_SCRIPT" << 'DEPS_PKG_DEBIAN_EOF'
#!/bin/bash
export HOME=/root
export LANG=C.UTF-8
export DAIDAI_DIR=/app/Dumb-Panel
export DEBIAN_FRONTEND=noninteractive

# apt 加固配置。写在任何 apt-get 调用之前，且【装完不删】——
# 运行期面板装 Linux 依赖走的是同一个容器里的裸 apt-get
# （server/service/linux_packages.go、server/handler/deps_package_manager.go、
#  server/service/backup_runtime.go 三处），apt 会自动读 /etc/apt/apt.conf.d/，
# 留着这份配置就等于顺带把那三条运行期路径一起加固了。
#
# APT::Sandbox::User "root"：apt 默认会把下载动作降权到 _apt 用户跑。
#   在带 CONFIG_ANDROID_PARANOID_NETWORK 的老内核上（Android ≤7），非 AID_INET
#   组的 uid 连 socket() 都调不通，表现就是「root 手动 wget 得到，apt 却解析不了」。
#   Android 8+ 换成 netd 的 eBPF 过滤后 _apt 这类系统 uid 默认放行，所以这一条
#   在主力设备上多半是空转 —— 定位是「消除一个不确定变量」，不是已确认的根因修复。
#   注意不要改成给 _apt 加 aid_3003 组：apt 自己的 DropPrivs() 会 setgroups() 清掉
#   附加组，加了也不生效，只会把问题掩盖掉。
# ForceIPv4：手机上常见「有 IPv6 地址但出不去」，apt 会先试 AAAA 再慢慢回落。
mkdir -p /etc/apt/apt.conf.d
cat > /etc/apt/apt.conf.d/99-daidai-android << 'APTCONF_EOF'
APT::Sandbox::User "root";
Acquire::Retries "3";
Acquire::http::Timeout "30";
Acquire::https::Timeout "30";
Acquire::ForceIPv4 "true";
APTCONF_EOF

# 镜像源改写 + 逐个回退。
# 注意 bookworm 用的是 deb822 格式的 /etc/apt/sources.list.d/debian.sources，
# 不是老的 /etc/apt/sources.list —— 只改后者会静默继续走 deb.debian.org，
# 表现是"装依赖特别慢/偶发超时"而不是报错。两个都试一遍，哪个在就改哪个。
# NJU / TUNA / 阿里云的 security 源路径同样是 /debian-security，所以只换域名 URI 依然正确。
#
# 先留一份原始副本：换第二个源时如果还在已改过的文件上 sed，
# 就得知道「上一个源叫什么」，每多一个候选就多一层状态；从原始副本重改最省事。
for _src in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do
  [ -f "$_src" ] || continue
  [ -f "$_src.daidai-orig" ] || cp -f "$_src" "$_src.daidai-orig"
done

# 顺便把 https 钉成 http：此刻 ca-certificates 还没装（debian-slim 不自带根证书），
# 镜像源一旦 301 到 https，apt-get update 会直接死在证书校验上，
# 报错还长得跟网络不通一模一样。等下面装完 ca-certificates 就没这个约束了。
use_mirror() {
  _host="$1"
  for _src in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do
    [ -f "$_src.daidai-orig" ] || continue
    cp -f "$_src.daidai-orig" "$_src"
    if [ "$_host" != "deb.debian.org" ]; then
      sed -i -e "s|deb.debian.org|$_host|g" \
             -e "s|security.debian.org|$_host|g" "$_src"
    fi
    sed -i -e 's|https://|http://|g' "$_src"
  done
}

# 顺序：NJU（原来唯一的源）-> TUNA -> 阿里云 -> Debian 官方。
# 前三个是国内镜像，最后一个是官方源兜底：国内镜像被单位网络整体屏蔽时还有得救。
_mirror_ok=0
_mirror_used=""
for _mirror in mirrors.nju.edu.cn mirrors.tuna.tsinghua.edu.cn mirrors.aliyun.com deb.debian.org; do
  echo "[daidai] 正在尝试镜像源: $_mirror"
  use_mirror "$_mirror"
  if apt-get update; then
    _mirror_ok=1
    _mirror_used="$_mirror"
    echo "[daidai] 镜像源可用: $_mirror"
    break
  fi
  echo "[daidai] 镜像源不可用，换下一个: $_mirror"
done
if [ "$_mirror_ok" = "1" ]; then
  echo "MIRROR=$_mirror_used" > /tmp/daidai-deps-status
else
  echo "MIRROR=FAIL" > /tmp/daidai-deps-status
  echo "[daidai] 所有候选镜像源的 apt-get update 都失败了"
fi

# 装包期间禁止 dpkg 的 postinst 去拉起服务。
# openssh-server 的 postinst 会调 invoke-rc.d ssh start —— 这里是 ruri 的 chroot，
# 没有 init/systemd，起服务失败会让 dpkg 返回非 0，进而让整批安装被标成失败。
# policy-rc.d 返回 101 是 Docker/chroot 场景的标准做法：告诉 invoke-rc.d「别动」。
# 装完就删掉，避免影响用户日后自己在容器里装东西。
# sshd 由 service.sh 每次开机直接 exec /usr/sbin/sshd 拉起，不依赖 init 脚本。
printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d
chmod +x /usr/sbin/policy-rc.d

# 根证书必须【第一个】装。
# 原来它混在下面那一大批里，等于「装完这批才有根证书」，可是这批里的 curl / git / pip
# 一旦在 postinst 或后续步骤里走 https 就已经没证书可用了；更要命的是镜像源只要对
# http 做一次 301 -> https，整批安装会直接死在证书校验上，报错长得跟网络不通一模一样。
# 这一步失败不致命（下面那批照装，验证段会兜底），所以不做任何中止。
#
# 装完不把 sources 切回 https：apt 的包本来就有 GPG 签名，走 http 是 Debian 官方
# 推荐做法，安全性不打折；少两轮 apt-get update，也少一处可能失败的分支。
apt-get install -y --no-install-recommends ca-certificates || \
  echo "[daidai] ca-certificates 安装失败，pip / npm / git 的 https 可能不可用"

# 与 Alpine 版逐条对齐（左 Alpine / 右 Debian）：
#   build-base -> build-essential      py3-pip -> python3-pip
#   openssh    -> openssh-client + openssh-server
#   shadow     -> passwd（bookworm 预装，显式列出只为把对应关系写死在这里）
#   其余同名：bash / bash-completion / coreutils / curl / wget / git / jq /
#             openssl / libtool / python3 / python3-dev / nodejs / npm /
#             tzdata / procps / netcat-openbsd
# 两个 Debian 独有的追加项：
#   python3-venv       bookworm 把 ensurepip 拆出去了，没有它 python3 -m venv 直接失败，
#                      而 service.sh 每次开机都要建 deps/python/<小版本> 这个 venv
#   ca-certificates    Alpine 的 apk 自带根证书，debian-slim 不带，pip/npm/git 走 https 会炸
#                      （上面已经先单独装过一次，这里留着只是把清单写全，重复装是空操作）
apt-get install -y --no-install-recommends \
  bash bash-completion coreutils build-essential \
  curl wget git jq openssh-client openssh-server openssl libtool \
  python3 python3-dev python3-pip python3-venv \
  nodejs npm \
  passwd tzdata procps netcat-openbsd \
  ca-certificates

rm -f /usr/sbin/policy-rc.d

# 缓存留着只会白占几百 MB，手机内部存储很贵。
# 注意【不要】顺手删 /etc/apt/apt.conf.d/99-daidai-android：
# 面板运行期装 Linux 依赖用的是同一个容器里的 apt，那份配置对它同样有效，
# 删了等于每次运行期装包又回到没有 Retries / 没有 Sandbox 覆写的裸状态。
apt-get clean
rm -rf /var/lib/apt/lists/*
DEPS_PKG_DEBIAN_EOF
else
  cat > "$DEPS_SCRIPT" << 'DEPS_PKG_ALPINE_EOF'
#!/bin/ash
export HOME=/root
export LANG=C.UTF-8
export DAIDAI_DIR=/app/Dumb-Panel

# 切到 NJU Alpine 镜像源
sed -i 's|dl-cdn.alpinelinux.org|mirrors.nju.edu.cn|g' /etc/apk/repositories

# 先装离线包（linux-pam / shadow），再联网装剩下的。
# 这一句本来就允许失败（后面有联网兜底），所以整个脚本不能加 set -e。
apk add --allow-untrusted --no-network /tmp/apk/*.apk 2>/dev/null && rm -rf /tmp/apk

apk add --no-cache \
  bash bash-completion coreutils build-base \
  curl wget git jq openssh openssl libtool \
  python3 python3-dev py3-pip \
  nodejs npm \
  shadow tzdata procps netcat-openbsd
DEPS_PKG_ALPINE_EOF
fi

# ---- 以下与包管理器无关，两个 flavor 共用同一份 ----
# 同样不能加 set -e：上面 Alpine 的离线包安装允许失败，Debian 的 apt-get 也可能
# 局部失败但仍需要走完后面的配置；真正的判据是下面那段"依赖装完验证"。
cat >> "$DEPS_SCRIPT" << 'DEPS_COMMON_EOF'

# Android AID 组兼容
for id in 3001 3002 3003 3004 3005; do
  groupadd -g $id aid_$id 2>/dev/null || true
done
usermod -a -G aid_3001,aid_3002,aid_3003,aid_3004,aid_3005 root 2>/dev/null || true

# SSH 凭据（ports.conf 可自定义，这里用默认值）
SSH_USER="${SSH_USER:-root}"
SSH_PASSWORD="${SSH_PASSWORD:-123456}"
echo "${SSH_USER}:${SSH_PASSWORD}" | chpasswd 2>/dev/null
# Alpine 是 busybox chsh（密码走 stdin），Debian 是 shadow 的 chsh（root 经 pam_rootok 免密），
# 两边都吃得下这个参数顺序；再用 usermod 兜一次底，保证 root 的登录 shell 一定是 bash。
echo '123456' | chsh root -s /bin/bash 2>/dev/null
usermod -s /bin/bash root 2>/dev/null || true
cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime 2>/dev/null

# SSH 基础配置
sed -i -e 's/^#PermitRootLogin.*/PermitRootLogin yes/' \
       -e 's/^#PasswordAuthentication/PasswordAuthentication/' \
       /etc/ssh/sshd_config 2>/dev/null
ssh-keygen -A 2>/dev/null

# 常用镜像源
npm config set registry https://registry.npmmirror.com 2>/dev/null
git config --global user.email "daidai@users.noreply.github.com"
git config --global user.name "daidai"
git config --global http.postBuffer 524288000

mkdir -p /app /app/web /app/Dumb-Panel
DEPS_COMMON_EOF

chmod +x "$DEPS_SCRIPT" 2>/dev/null

# ---- 容器内 DNS 判别探测（仅 Debian）--------------------------------------
# 目的不是修复，是**区分**。Debian 版报的那句 `Temporary failure resolving`
# 至少对应三种互斥的失败机制，脚本里原本没有任何东西能把它们分开：
#   A. apt 降权到 _apt 后被内核按 uid 拒网（只在老内核的 PARANOID_NETWORK 上成立）
#   B. glibc 解析器行为（A+AAAA 并发查询丢应答，musl 不受影响 -> Alpine 照通）
#   C. 容器里配的 DNS 本身不可达（强制 DNS / Captive Portal / 屏蔽外部 53）
# 用 root 和 _apt 两个身份各跑一次 getent hosts，就能把 A 与 B/C 分开：
#   两条都失败 -> B 或 C（uid 说被证伪）
#   仅 _apt 失败 -> A 成立
#
# 只对 Debian 做：`_apt` 这个用户是 Debian 特有的，Alpine 的 apk 也不降权，
# 而且要求「Alpine flavor 行为完全不变」，多跑一次 ruri 都不加。
#
# 探测失败绝不中止安装 —— 它只负责往日志里留证据，真正的判据仍是后面的运行时验证。
DNS_PROBE_VERDICT="skipped"
if [ "$FLAVOR" = "debian" ]; then
  ui_print "- 正在探测容器内 DNS 解析能力..."
  cat > "$rootfs/tmp/daidai-dns-probe.sh" << 'DNS_PROBE_EOF'
#!/bin/bash
# 输出固定的 KEY=VALUE 行，宿主侧按前缀解析，不依赖任何语言/顺序
probe_host="mirrors.nju.edu.cn"
echo "PROBE_DNS=$(awk '/^nameserver/{printf "%s ", $2}' /etc/resolv.conf 2>/dev/null)"
if getent hosts "$probe_host" >/dev/null 2>&1; then
  echo "PROBE_ROOT=OK"
else
  echo "PROBE_ROOT=FAIL"
fi
# _apt 在 bookworm 是 uid 42 / gid 65534(nogroup)，bullseye 才是 100 ——
# 这里必须动态取，写死过的版本一换基础镜像就静默探测错对象。
apt_uid=$(id -u _apt 2>/dev/null)
apt_gid=$(id -g _apt 2>/dev/null)
if [ -z "$apt_uid" ] || [ -z "$apt_gid" ]; then
  echo "PROBE_APT=NOUSER"
elif ! command -v setpriv >/dev/null 2>&1; then
  echo "PROBE_APT=NOSETPRIV"
elif setpriv --reuid="$apt_uid" --regid="$apt_gid" --clear-groups \
     getent hosts "$probe_host" >/dev/null 2>&1; then
  echo "PROBE_APT=OK(uid=$apt_uid)"
else
  echo "PROBE_APT=FAIL(uid=$apt_uid)"
fi
DNS_PROBE_EOF
  chmod +x "$rootfs/tmp/daidai-dns-probe.sh" 2>/dev/null
  DNS_PROBE_OUT="$TMPDIR/dns-probe.txt"
  : > "$DNS_PROBE_OUT"
  "$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" /tmp/daidai-dns-probe.sh \
    > "$DNS_PROBE_OUT" 2>/dev/null || true

  probe_dns=$(grep '^PROBE_DNS=' "$DNS_PROBE_OUT" 2>/dev/null | cut -d= -f2-)
  probe_root=$(grep '^PROBE_ROOT=' "$DNS_PROBE_OUT" 2>/dev/null | cut -d= -f2-)
  probe_apt=$(grep '^PROBE_APT=' "$DNS_PROBE_OUT" 2>/dev/null | cut -d= -f2-)
  ui_print "-   容器 nameserver: ${probe_dns:-?}"
  ui_print "-   root 身份解析 mirrors.nju.edu.cn: ${probe_root:-无输出}"
  ui_print "-   _apt 身份解析 mirrors.nju.edu.cn: ${probe_apt:-无输出}"

  case "$probe_root:$probe_apt" in
    OK:OK*)
      DNS_PROBE_VERDICT="ok"
      ui_print "-   判定：容器内 DNS 正常（两种身份都能解析）"
      ;;
    OK:FAIL*)
      DNS_PROBE_VERDICT="apt_uid_blocked"
      ui_print "-   判定：root 能解析、apt 的降权用户 _apt 不能 —— 内核按 uid 拦了网络"
      ui_print "-   （已写入 APT::Sandbox::User \"root\" 让 apt 不再降权，本次安装应能绕过）"
      ;;
    OK:*)
      # NOUSER / NOSETPRIV：降权侧测不了，但 root 侧解析是通的
      DNS_PROBE_VERDICT="root_ok"
      ui_print "-   判定：root 能解析；降权身份无法测试（$probe_apt），只能确认 DNS 本身可用"
      ;;
    FAIL:*)
      # root 都解析不了，apt 以 root 跑同样解析不了 —— 无论降权侧结果如何，
      # 都能确定问题不在 uid 门控上，所以这里不再细分 _apt 的取值。
      DNS_PROBE_VERDICT="dns_down"
      ui_print "-   判定：root 身份就解析不了 —— 容器配的 DNS 本身不通，与 apt 降权无关"
      ;;
    *)
      DNS_PROBE_VERDICT="unknown"
      ui_print "-   判定：探测没有拿到有效输出，无法判别（不影响继续安装）"
      ;;
  esac
fi

"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" /tmp/daidai-install-deps.sh

# ---- 验证关键运行时真的装上了 --------------------------------------------
# 不要只信包管理器的退出码：apk add / apt-get install 都可能部分成功（个别包 404、
# 网络中途断开、镜像源同步不完整），退出码却未必反映出来。真正可靠的判据是装完之后
# 关键运行时到底能不能执行、能不能报出版本。
#
# 也不要在上面那个装依赖脚本里加 set -e：Alpine 那句离线包 `apk add --no-network`
# 本来就允许失败（后面有联网兜底），加了会直接中断整个安装。验证统一放在这里做。
#
# 验证清单两个 flavor 完全一致（python3 / node / npm / git / bash），
# 但进容器的 shell 必须跟着 flavor 走 —— 和上面的能力探测是同一个坑。
ui_print "- 正在验证容器运行时..."

DEPS_REPORT="$TMPDIR/deps-verify.txt"
: > "$DEPS_REPORT"
"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" -c '
  for c in python3 node npm git bash; do
    if command -v "$c" >/dev/null 2>&1 && "$c" --version >/dev/null 2>&1; then
      echo "OK $c"
    else
      echo "MISSING $c"
    fi
  done
' > "$DEPS_REPORT" 2>/dev/null

missing_runtimes=""
for c in python3 node npm git bash; do
  if ! grep -q "^OK $c$" "$DEPS_REPORT" 2>/dev/null; then
    missing_runtimes="$missing_runtimes $c"
  fi
done

if [ -n "$missing_runtimes" ]; then
  ui_print "! 以下运行时未能安装成功:$missing_runtimes"
  ui_print "!"
  ui_print "! 这一步强依赖网络：${CTR_PKG_TOOL} 需要从 ${CTR_PKG_SOURCE} 下载${CTR_DEPS_SIZE}。"

  # Debian 版分流：DNS 不通 / 镜像源全挂 / 单纯下载中断，用户看到的现象完全不同，
  # 原来三种都只给同一句「检查网络」，等于把最关键的线索抹掉了。
  # Alpine 分支的文案与改动前逐字一致，不受这段影响。
  if [ "$FLAVOR" = "debian" ]; then
    deps_status=""
    if [ -f "$rootfs/tmp/daidai-deps-status" ]; then
      deps_status=$(grep '^MIRROR=' "$rootfs/tmp/daidai-deps-status" 2>/dev/null | cut -d= -f2-)
    fi
    case "$DNS_PROBE_VERDICT" in
      dns_down)
        ui_print "!"
        ui_print "! 判定：容器内 DNS 解析失败（连 root 身份都解析不出域名）。"
        ui_print "! 注意这【不等于】你手机没网 —— 宿主走的是系统 DNS，容器走的是"
        ui_print "! $rootfs/etc/resolv.conf 里那几个（内容见上面的「容器 nameserver」）。"
        ui_print "! 常见于校园网 / 企业网强制 DNS、公共 Wi-Fi 的登录门户，"
        ui_print "! 以及部分运营商屏蔽对外 53 端口。"
        ui_print "! 解决办法：换个网络（手机热点 / 家里 Wi-Fi）后重装；"
        ui_print "! 或先把当前网络实际可用的 DNS 写进 $rootfs/etc/resolv.conf 再重装。"
        ;;
      apt_uid_blocked)
        ui_print "!"
        ui_print "! 判定：root 能解析域名，但 apt 的降权用户 _apt 不能 —— 是内核按 uid"
        ui_print "! 拦掉了网络（老内核的 PARANOID_NETWORK 策略）。本次安装已写入"
        ui_print "! /etc/apt/apt.conf.d/99-daidai-android 让 apt 不再降权，若仍失败，"
        ui_print "! 请把上面这段判别输出反馈给开发者。"
        ;;
      *)
        if [ "$deps_status" = "FAIL" ]; then
          ui_print "!"
          ui_print "! 判定：四个候选镜像源（NJU / TUNA / 阿里云 / Debian 官方）"
          ui_print "! 的 apt-get update 全部失败 —— 多半是镜像源被网络策略拦截，或需要代理。"
          ui_print "! 请换网络或配置代理后重装。"
        elif [ -n "$deps_status" ]; then
          ui_print "!"
          ui_print "! 判定：镜像源 $deps_status 是通的，软件包索引也拉下来了，"
          ui_print "! 失败发生在下载 / 解包阶段 —— 常见于中途断网、存储空间不足。"
          ui_print "! 请确认剩余空间充足、网络稳定后重装。"
        else
          ui_print "!"
          ui_print "! 判定：没能拿到装依赖阶段的状态记录，无法进一步区分失败环节。"
          ui_print "! 请把上面完整的安装日志反馈给开发者。"
        fi
        ;;
    esac
  else
    ui_print "! 请检查网络（公司 / 校园网被墙时可挂 VPN），然后重新安装本模块。"
  fi

  ui_print "!"
  ui_print "! 缺少这些运行时的话，面板的定时任务和依赖管理都无法工作，"
  ui_print "! 所以这里直接中止，不会给你一个装了却用不了的面板。"
  warn_backup_preserved
  abort "! 安装已中止：容器运行时验证未通过"
fi

INSTALL_DEPS_OK=1
ui_print "- 容器运行时验证通过 (python3 / node / npm / git / bash)"

# 容器里补一份默认 bashrc。
# 路径两个 flavor 不一样：Alpine 的 bash 包读 /etc/bash/bashrc，
# Debian 读 /etc/bash.bashrc —— 写错位置不会报错，只是环境变量静默不生效。
mkdir -p "$(dirname "$rootfs$CTR_BASHRC")"
cat > "$rootfs$CTR_BASHRC" << 'BASHRC_EOF'
export HOME=/root
export LANG=C.UTF-8
export SHELL=/bin/bash
export PS1='\u@\h:\w\$ '
export DAIDAI_DIR=/app/Dumb-Panel
export DAIDAI_MAGISK_MODULE=1
export DAIDAI_ANDROID_RUNTIME_BIN_DIR=/data/adb/daidai-panel/bin
export PATH=/data/adb/daidai-panel/bin/python/bin:/data/adb/daidai-panel/bin/node/bin:/data/adb/daidai-panel/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export NODE_PATH=/usr/local/lib/node_modules
BASHRC_EOF

# ---- 回填用户数据 -------------------------------------------------------
if [ "$INSTALL_BACKUP_READY" = "1" ] && [ -d "$INSTALL_BACKUP_DIR" ]; then
  ui_print "- 正在恢复用户数据..."
  mkdir -p "$rootfs/app/Dumb-Panel"
  for item in "$INSTALL_BACKUP_DIR"/* "$INSTALL_BACKUP_DIR"/.[!.]* "$INSTALL_BACKUP_DIR"/..?*; do
    [ -e "$item" ] || continue
    cp -rf "$item" "$rootfs/app/Dumb-Panel/" 2>/dev/null || \
      abort "! 用户数据恢复失败：$(basename "$item") 无法复制回容器数据目录"
  done
  rm -rf "$INSTALL_BACKUP_DIR" 2>/dev/null
  rm -f "$INSTALL_IN_PROGRESS_FLAG" 2>/dev/null
fi

# module.prop 同步一份给容器内 (supply to updater)
mkdir -p $rootfs/app
cp -f $MODPATH/module.prop $rootfs/app/module.prop 2>/dev/null

# ---- 持久化数据目录 ------------------------------------------------------
mkdir -p "$PERSIST_DIR"

# 把新版本的 module.prop 也落一份到持久化目录，作为 get_current_version() 的兜底，
# 下次升级就算管理器路径差异也能读到正确的旧版本号。
cp -f "$MODPATH/module.prop" "$PERSIST_DIR/module.prop" 2>/dev/null || true

# ---- 默认端口配置（用户可编辑 ports.conf 自定义端口，重启模块后生效） ----
if [ ! -f "$PERSIST_DIR/ports.conf" ]; then
  cat > "$PERSIST_DIR/ports.conf" << 'PCONF'
# 呆呆面板端口配置 —— 修改后重启模块生效
#
# PANEL_PORT: 面板 HTTP 端口（浏览器访问端口），默认 5700
#             后端绑定的是 0.0.0.0:PANEL_PORT，局域网 / 穿透都能直连
# SSH_PORT:   容器内 SSH 端口（adb/termux 登入容器调试），默认 22
# SSH_USER:   SSH 登录用户名，默认 root
# SSH_PASSWORD: SSH 登录密码，默认 123456（建议修改！）
# EXTRA_CORS_ORIGINS:
#             额外的 CORS 白名单；默认 127.0.0.1 / localhost 已放行，
#             且"同源请求"会被中间件自动放行，绝大多数内网穿透不需要改它。
#             以下两种情况再补：
#               1) 穿透侧端口与面板端口不同（例如 frp 公网 6700 → 内网 5700）
#               2) 用跨域模式访问（浏览器 Origin 与后端 Host 不一致）
#             用英文逗号分隔，建议加引号，示例：
#               EXTRA_CORS_ORIGINS="https://panel.example.com,https://xx.trycloudflare.com"
PANEL_PORT=5700
SSH_PORT=22
SSH_USER=root
SSH_PASSWORD=123456
EXTRA_CORS_ORIGINS=""
PCONF
fi

# 读一下当前配置，用于提示
CUR_PANEL_PORT=5700
CUR_SSH_PORT=22
# shellcheck disable=SC1090
. "$PERSIST_DIR/ports.conf" 2>/dev/null || true
CUR_PANEL_PORT="${PANEL_PORT:-5700}"
CUR_SSH_PORT="${SSH_PORT:-22}"

# ---- 一键恢复脚本（指向 PERSIST_DIR/last-update-backup） ------------------
# 每次安装都重写，保证脚本里硬编码的 rootfs / MODID 与本次一致。
cat > "$PERSIST_DIR/restore-last-update.sh" <<RESTORE
#!/system/bin/sh
# 呆呆面板 - 一键恢复"上次更新前"的数据快照。
# 使用：su -c "sh /data/adb/daidai-panel/restore-last-update.sh"
set -e

MODID=$MODID
PERSIST_DIR=$PERSIST_DIR
BACKUP_DIR="\$PERSIST_DIR/last-update-backup"
ROOTFS_CANDIDATES="/data/daidai /data/local/daidai"

log()  { echo "[restore] \$*"; }
fail() { echo "[restore][FATAL] \$*" >&2; exit 1; }

if [ ! -d "\$BACKUP_DIR" ]; then
  fail "找不到备份目录 \$BACKUP_DIR；说明还没经历过任何一次模块更新"
fi
if [ ! -s "\$BACKUP_DIR/BACKUP_INFO.txt" ]; then
  log "警告：\$BACKUP_DIR 存在但没有 BACKUP_INFO.txt，可能是不完整快照"
fi

# 找当前 rootfs
ROOTFS=""
for candidate in \$ROOTFS_CANDIDATES; do
  if [ -d "\$candidate/app/Dumb-Panel" ] || [ -d "\$candidate/app" ]; then
    ROOTFS="\$candidate"
    break
  fi
done
[ -n "\$ROOTFS" ] || fail "找不到 rootfs（试过：\$ROOTFS_CANDIDATES）；请确认模块已安装"

TARGET="\$ROOTFS/app/Dumb-Panel"
log "rootfs: \$ROOTFS"
log "目标: \$TARGET"

cat "\$BACKUP_DIR/BACKUP_INFO.txt" 2>/dev/null | head -n 8
echo

# 安全检查：当前目录已存在且非空 → 二次确认
if [ -d "\$TARGET" ] && [ -n "\$(ls -A "\$TARGET" 2>/dev/null)" ]; then
  log "目标目录已存在数据；恢复会覆盖同名文件（其他文件保留）"
  if [ -z "\$FORCE" ]; then
    printf "确认恢复？(y/N): "
    read -r ans
    case "\$ans" in
      y|Y|yes|YES) ;;
      *) fail "用户取消" ;;
    esac
  fi
fi

# 停面板
# 必须先写停止开关再 kill：service.sh fork 的存活守护每分钟探活一次，
# 光 pkill 的话 60 秒内面板就会被拉回来，恢复到一半的数据目录会被新进程接管。
# 收尾时会把开关删掉。
log "停止 daidai-server ..."
mkdir -p "\$PERSIST_DIR" 2>/dev/null || true
echo "stopped by restore-last-update.sh" > "\$PERSIST_DIR/stopped" 2>/dev/null || true
pkill -f /usr/local/bin/daidai-server 2>/dev/null || true
pkill -f daidai-server 2>/dev/null || true
sleep 12

# 回拷（覆盖式 cp，但用 -a 保留属性；不删 TARGET 里的额外文件）
mkdir -p "\$TARGET"
log "从快照复制 ..."
for item in "\$BACKUP_DIR"/* "\$BACKUP_DIR"/.[!.]* "\$BACKUP_DIR"/..?*; do
  [ -e "\$item" ] || continue
  [ "\$(basename "\$item")" = "BACKUP_INFO.txt" ] && continue
  cp -af "\$item" "\$TARGET/"
done

# 清掉停止开关，否则重启后 service.sh 会在早退点直接退出、面板不会自动启动。
rm -f "\$PERSIST_DIR/stopped" 2>/dev/null || true

log "恢复完成"
log "下一步：重启设备，或点模块卡片的「运行 / Action」按钮启动面板，也可以执行："
log "  su -c \"sh /data/adb/modules/\$MODID/action.sh\""
RESTORE
chmod +x "$PERSIST_DIR/restore-last-update.sh" 2>/dev/null

# ---- 收尾 --------------------------------------------------------------
"$RURIMA" ruri -w -U $rootfs 2>/dev/null || true

# 无条件清掉停止开关（包括上面为了让守护自退而写的那一份）。
# 「刚装完的模块必须能起来」优先级高于「记住我停过」：
# 留着它的话，用户刷完 zip 重启会得到一个静默不启动的面板，且完全没有线索。
rm -f "$PERSIST_DIR/stopped" 2>/dev/null

# 「安装完成！」必须是有条件的。
# 上面每一条失败路径都走 abort（abort 自身 exit 1，走不到这里），所以这道判断
# 是第二重保险：万一将来有人把某个 abort 改回警告，也不会再出现
# "安装器显示成功 → 重启 → 面板起不来 → 用户不知道哪一步错了" 这种情况。
if [ "$INSTALL_DEPS_OK" != "1" ]; then
  ui_print "! 容器运行时未通过验证，安装未完成"
  warn_backup_preserved
  abort "! 安装已中止"
fi

ui_print ""
ui_print "- 安装完成！"
ui_print "- 重启后面板将自动启动，访问 http://127.0.0.1:${CUR_PANEL_PORT}"
ui_print "- 端口配置: $PERSIST_DIR/ports.conf (PANEL_PORT=${CUR_PANEL_PORT}, SSH_PORT=${CUR_SSH_PORT})"
ui_print "- SSH 连接: ssh ${SSH_USER:-root}@<设备IP> -p ${CUR_SSH_PORT} (默认密码: ${SSH_PASSWORD:-123456})"
ui_print "- rootfs 位置: $rootfs"
ui_print "- 数据目录:   $rootfs/app/Dumb-Panel"
ui_print ""
