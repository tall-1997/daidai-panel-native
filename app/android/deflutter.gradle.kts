/**
 * 去 Flutter 辅助开关（阶段 5 · 评估用，当前默认关闭，不影响现有构建）。
 *
 * 本文件只暴露布尔开关，不做任何实际删除/改动，供后续「launcher 切换后整合执行」
 * 时被 app/build.gradle.kts 低风险引用：
 *   - 默认（未设 `-Pdeflutter` 或值不为 true）时，`definitiveDeflutter` 与
 *     `isDeflutter` 均为 false，现有 Flutter 构建分支原样生效，文件无副作用。
 *   - 集成阶段在顶层级 build 引用本脚本并设 `-Pdeflutter=true`（或将下方
 *     `deflutter` Gradle 属性置为 true）后，切进原生构建分支：
 *     * 不再应用 `id("dev.flutter.flutter-gradle-plugin")`；
 *     * `flutter.ndkVersion` → 静态常量 `28.2.13676358`；
 *     * `flutter.versionCode/name` → 来自 VERSION.json 的 `2000000` / `2.0.0`；
 *     * 删除 `flutter { source = "../.." }`；
 *     * ABI 仍由 `requestedAbis`（ANDROID_RUNTIME_ABIS）驱动，去掉 flutter split 分支。
 *
 * 引用示例（app/build.gradle.kts 顶部）：
 *   apply(from = rootProject.file("deflutter.gradle.kts"))
 *   val deflutter = (ext["definitiveDeflutter"] as? Boolean) == true
 */
ext {
    // 去 Flutter 最终开关，默认 false。集成时设 true 走原生构建分支：
    //   方式 A：命令行 Gradle 属性 -Pdeflutter=true
    //   方式 B：把下方 gradleProperty 判断改为恒 true，或将默认值写死为 true
    definitiveDeflutter = providers.gradleProperty("deflutter").orNull?.trim()
        ?.equals("true", ignoreCase = true) ?: false

    // 供 app/build.gradle.kts 直接读取的语义化布尔（与 definitiveDeflutter 同值）。
    isDeflutter = definitiveDeflutter

    // 提示性常量：去 Flutter 后建议使用的静态版本/工具链来源（供集成核对，不参与构建）。
    deflutterVersionName = "2.0.0"          // 取自仓库根 VERSION.json version 字段
    deflutterVersionCode = 2_000_000        // 取自 VERSION.json androidVersionCode 字段
    deflutterNdkVersion = "28.2.13676358"   // 与当前 flutter.ndkVersion 一致，保持行为不变
}
