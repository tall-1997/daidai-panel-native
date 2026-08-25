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
