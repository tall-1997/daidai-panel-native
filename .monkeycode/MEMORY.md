# User Instruction Memory

This file records user instructions, preferences, and teachings for reference in future interactions.

## Format

### User Instruction Entry
User instruction entries should follow this format:

[User Instruction Summary]
- Date: [YYYY-MM-DD]
- Context: [Mentioned scenario or time]
- Instructions:
  - [Content of user teaching or instruction, described line by line]

### Project Knowledge Entry
Entries discovered by the Agent during task execution should follow this format:

[Project Knowledge Summary]
- Date: [YYYY-MM-DD]
- Context: Discovered by Agent while performing [specific task description]
- Category: [Operations & Deployment|Build Methods|Testing Methods|Troubleshooting & Debugging|Workflow & Collaboration|Environment Configuration]
- Instructions:
  - [Specific knowledge points, described line by line]

## Deduplication Strategy
- Before adding a new entry, check for similar or identical instructions.
- If a duplicate is found, skip the new entry or merge it with the existing one.
- When merging, update the context or date information.
- This helps avoid redundant entries and keeps the memory file tidy.

## Entries

[Android 测试版与正式版发布策略]
- Date: 2026-08-28
- Context: 用户明确指定 Android 测试包长期版本号与正式版基线
- Instructions:
  - Android 测试包固定使用版本号 `1.0.19`，后续测试发布持续覆盖该版本的测试资产。
  - 只有用户明确要求使用新版本号发布时，才提升测试包版本号。
  - Android 当前正式版为 `1.0.18`，GitHub Release 应将 `v1.0.18` 标记为正式版和 latest。
  - 不保留 `1.0.20` 测试 Release、标签或项目版本声明。

[全量审查与优化执行方式]
- Date: 2026-08-27
- Context: 用户要求后续项目全量审查、Bug 修复和优化任务采用主动执行模式
- Instructions:
  - 全量审查项目最新进度时，默认拆分多个子任务并行检查不同模块，由主任务统一汇总、实施和验证。
  - 发现 Bug、回归风险和明确优化项后直接实现，无需逐项询问用户。
  - 方案选择以项目整体质量、长期可维护性和端到端效果为目标，不以最小改动量作为优先标准。
  - 完成前执行覆盖相关模块的测试、静态检查和构建，使用可复核证据闭环。

[Project Knowledge Summary]
- Date: 2026-08-24
- Context: Discovered by Agent while cross-compiling proot/busybox/talloc for Android arm64-v8a with NDK r27 (API 24)
- Category: Build Methods | Troubleshooting & Debugging
- Instructions:
  - proot 的 loader.elf 链接只读 GNUmakefile 内的 LOADER_LDFLAGS 变量，命令行传 LDFLAGS 对它无效；命令行传 `LOADER_LDFLAGS+=...` 会丢失默认的 `-static -nostdlib -Ttext`（GNUmakefile 用 `LOADER_LDFLAGS$1 +=` 在 eval 内定义）。正确做法是 sed 直接改 src/GNUmakefile 里 loader 的链接参数追加 `-z,max-page-size=16384`，见 build_proot。
  - Android 16KB 页要求所有 jniLibs 原生库 PT_LOAD 对齐 >=16384，verify-android-linux-runtime.py 会逐一断言；proot 主二进制与 loader 都必须带 `-z,max-page-size=16384`。
  - verify-android-linux-runtime.py 断言：e_machine 按 ABI 映射（arm64-v8a=183/AArch64，x86_64=62/X86_64，脚本内 expected_machine 字典）、PT_LOAD 对齐 >=16384、manifest 中 size/sha256 与文件一致、packaged proot 必须含字节 "PROOT_LOADER"。
  - prepare-android-native-source-build.sh 的 build_proot 缓存检查可用环境变量 FORCE_REBUILD_PROOT=1 强制失效（产物已存在时默认跳过）。
  - manifest 输出路径必须用显式绝对路径（`$repo_root/app/android/app/src/main/assets/android-runtime/$abi/...`），勿用 `$native_dir/../assets`（会落到 jniLibs/assets，与 verify 读取的 main/assets 不一致）。
  - 多 ABI 支持：`ANDROID_RUNTIME_ABIS="arm64-v8a x86_64"` 运行 prepare-android-native-source-build.sh；脚本 target_and_toolchain 已含两 ABI 映射，talloc 的 qemu 选择 `qemu-${target%%-*}`（qemu-x86_64 存在）。
  - qemu-user 冒烟 busybox：直接 `qemu-aarch64 <path.so> --list` 会报 "applet not found"（guest argv[0] 是 .so 文件名，busybox 按 argv[0] basename 解析 applet）。必须用 `qemu-aarch64 -0 busybox <path.so> --list` 设置 guest argv[0]；sh 冒烟用 `-0 sh`。
  - jniLibs 已移除所有 Termux 预编译 .so（liboperit_*、libicu*、libandroid-* 等），只保留 NDK 自编译的 libdaidai_proot.so、libproot_loader.so、libdaidai_busybox.so 及少量系统库；AndroidLinuxRuntime.kt 的 resolveNativeTool 只回退到 libdaidai_*，不再引用 liboperit_*。

[Project Knowledge Summary]
- Date: 2026-08-25
- Context: Discovered by Agent while fixing CI failures after the Termux-dependency removal refactor
- Category: Build Methods | Troubleshooting & Debugging
- Instructions:
  - `native-runtime-manifest.json` 的 `artifacts` 列表只放原生 ELF 二进制（.so），不放 shell 脚本；`verifyLinuxRootfsRuntime` 会遍历 artifacts 逐个断言 `isArm64Elf` + PT_LOAD 对齐 >=16384 + sha256/size 与 jniLibs 内文件一致，shell 脚本会必然失败。
  - proot 的 TLS 段 p_align 需 >=64（ARM64 Bionic 要求），构建时必须对 proot 主二进制和 loader 追加 `-z,max-page-size=16384`（见 62c37a8）。
  - 镜像常量统一为 `ALPINE_APK_DEFAULT_MIRROR`（清华 TUNA）、`UBUNTU_APT_DEFAULT_MIRROR`、`PYTHON_PIP_ALIBABA_INDEX`、`NODE_NPM_NPMMIRROR_REGISTRY`；旧名 `ALPINE_APK_HUAWEI_MIRROR` 已废弃，主代码与测试文件都要同步改名，否则 compileDebugUnitTestKotlin 会报 Unresolved reference。
  - commit 前检查 `.git/hooks/prepare-commit-msg` 会自动从 local git config 的 coauthor.* 条目追加 Co-authored-by；本项目已移除 monkeycode-ai co-author，作者统一为 tall-1997。

[Project Knowledge Summary]
- Date: 2026-08-26
- Context: Discovered by Agent while reviewing shiyi-agent (github.com/JIUSIS/shiyi-agent) for proot/Alpine reference
- Category: Troubleshooting & Debugging
- Instructions:
  - apk-tools 3 写数据库用 hardlink 原子发布，无 root 设备 SELinux 禁 app_data_file link，apk add 装完文件但写 db 报 "failed to write database: Permission denied"；解决：放 apk-tools 2.x 静态版（用 rename 写 db）到 /usr/local/bin/apk（PATH 优先于 /sbin）。
  - seccomp 加速会破坏 apk 3 libfetch 的 connect()（报 Permission denied / DNS transient error），shiyi-agent 用 PROOT_NO_SECCOMP=1 换取 apk 兼容；本项目用默认 seccomp 保证脚本运行，若 apk add 突然报 Permission denied/DNS 错误需权衡该取舍。
  - --link2symlink 会把 link() 转成符号链接并生成 .l2s glue 文件，破坏 node-gyp 的 link+rename 原子发布（原生模块变悬空链接 dlopen 失败）；编译 node 原生模块时应去掉 --link2symlink。
  - proot 需 bind 系统目录 /apex /odm /product /system /system_ext /vendor /linkerconfig 等，供 rootfs 内进程访问系统库；并发 apk 用 mkdir 原子互斥锁 + 超时清理防抢数据库锁（EAGAIN/EINTR）。
