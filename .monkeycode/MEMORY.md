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
  - jniLibs 中的 liboperit_*/libpython3.14.so 等旧库是 AndroidLinuxRuntime.kt 的回退路径（`resolveNativeTool(context, listOf("libdaidai_*", "liboperit_*"))`）与 python/node runtime 依赖，勿删除。
