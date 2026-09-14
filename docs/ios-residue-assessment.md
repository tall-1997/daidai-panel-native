# app/ios/ 残留评估报告（W4）

> 评估对象：HEAD `454d7b2` 上 `app/ios/` 全部 40 个被跟踪文件。
> 本报告只做评估，**不执行任何删除**，删除与否由用户决策。
> 生成日期：2026-09-15

---

## 1. 结论速览

| 项目 | 结果 |
|---|---|
| 文件总数 | 40 个（全部为 Flutter `flutter create` 生成的 iOS Runner 模板） |
| 自定义 iOS 原生逻辑 | **无**（纯模板，git 历史显示自 82e140b 一次加入后从未修改） |
| 预计可删除量 | 40 个文件 / 1,266,102 字节（约 **1236.4 KB ≈ 1.21 MB**） |
| 是否被构建/CI/脚本引用 | Android 侧与根级 CI：**无引用**；`app/.github/workflows/` 有遗留 iOS 工作流（详见 §4） |
| 默认推荐 | **删除**（项目已完成 Android 去 Flutter，`app/ios/` 为纯死代码） |

---

## 2. 文件清单（40 个）

### 2.1 类型分布与大小

| 类别 | 文件数 | 大小 | 说明 |
|---|---|---|---|
| `Runner/` 源码与配置 | 6 | 7,000 B (6.8 KB) | AppDelegate.swift、SceneDelegate.swift、Info.plist、storyboard ×2、Bridging-Header.h |
| `Flutter/` 构建配置 | 3 | 780 B (0.8 KB) | AppFrameworkInfo.plist、Debug/Release.xcconfig |
| `Runner.xcodeproj/` 工程 | 5 | 31,173 B (30.4 KB) | project.pbxproj、workspace、scheme、设置 |
| `Runner.xcworkspace/` 工作区 | 3 | 616 B (0.6 KB) | contents、共享设置 |
| `Assets.xcassets/` 图标与启动图 | 21 | 1,225,679 B (1197 KB) | AppIcon 17 个 png + LaunchImage 3 个 png + 2 个 Contents.json + README |
| `RunnerTests/` 测试 | 1 | 285 B (0.3 KB) | RunnerTests.swift（空测试模板） |
| 根级 `.gitignore` | 1 | 569 B (0.6 KB) | app/ios/.gitignore |
| **合计** | **40** | **1,266,102 B（1236.4 KB）** | 其中 1.17 MB 为图标 PNG 二进制 |

### 2.2 逐文件判定（模板 / 可删）

| 文件 | 类型 | 判定 |
|---|---|---|
| `app/ios/.gitignore` | Flutter 模板 | 模板，可删 |
| `app/ios/Flutter/AppFrameworkInfo.plist` | Flutter 模板 | 模板，可删 |
| `app/ios/Flutter/Debug.xcconfig` | Flutter 模板（仅 `#include "Generated.xcconfig"`） | 模板，可删 |
| `app/ios/Flutter/Release.xcconfig` | Flutter 模板（同上） | 模板，可删 |
| `app/ios/Runner.xcodeproj/project.pbxproj` | Flutter 模板（Xcode 工程） | 模板，可删 |
| `app/ios/Runner.xcodeproj/project.xcworkspace/contents.xcworkspacedata` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcodeproj/project.xcworkspace/xcshareddata/IDEWorkspaceChecks.plist` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcodeproj/project.xcworkspace/xcshareddata/WorkspaceSettings.xcsettings` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcworkspace/contents.xcworkspacedata` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcworkspace/xcshareddata/IDEWorkspaceChecks.plist` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner.xcworkspace/xcshareddata/WorkspaceSettings.xcsettings` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner/AppDelegate.swift` | Flutter 模板（`FlutterAppDelegate` 子类，仅注册插件） | 模板，可删 |
| `app/ios/Runner/Assets.xcassets/AppIcon.appiconset/Contents.json` | Flutter 模板（图标清单） | 模板，可删 |
| `app/ios/Runner/Assets.xcassets/AppIcon.appiconset/Icon-App-*.png`（16 个） | Flutter 模板（默认占位图标） | 模板，可删 |
| `app/ios/Runner/Assets.xcassets/LaunchImage.imageset/Contents.json` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner/Assets.xcassets/LaunchImage.imageset/LaunchImage*.png`（3 个） | Flutter 模板（68 B 占位） | 模板，可删 |
| `app/ios/Runner/Assets.xcassets/LaunchImage.imageset/README.md` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner/Base.lproj/LaunchScreen.storyboard` | Flutter 模板 | 模板，可删 |
| `app/ios/Runner/Base.lproj/Main.storyboard` | Flutter 模板（`FlutterViewController` 占位） | 模板，可删 |
| `app/ios/Runner/Info.plist` | Flutter 模板（含 `NSAllowsArbitraryLoads`，显示名「呆呆面板」） | 模板，可删（仅 Info.plist 显示名/ATS 为模板内小改动，非业务逻辑） |
| `app/ios/Runner/Runner-Bridging-Header.h` | Flutter 模板（仅 import GeneratedPluginRegistrant.h） | 模板，可删 |
| `app/ios/Runner/SceneDelegate.swift` | Flutter 模板（`FlutterSceneDelegate` 空子类） | 模板，可删 |
| `app/ios/RunnerTests/RunnerTests.swift` | Flutter 模板（空 XCTest 用例） | 模板，可删 |

**自定义源码判定：无。** 唯一的非纯生成内容：
- `Info.plist` 的 `CFBundleDisplayName` 改为「呆呆面板」、ATS 打开 `NSAllowsArbitraryLoads`（L29-35）——属于模板配置微调，不构成 iOS 业务逻辑；重建工程时如需保留此配置可从 git 历史复制该文件。

---

## 3. 模板性验证（git 历史）

- `app/ios/` 全部 40 个文件**在单一提交 `82e140b`（feat: add Android embedded panel 0.0.1）中加入，此后 426 个提交中从未被修改**（`git log --oneline -- app/ios` 仅 1 条）。
- Swift 源码内容为 `flutter create` 标准模板：
  - `AppDelegate.swift`：`FlutterAppDelegate` + `FlutterImplicitEngineDelegate`，仅 `GeneratedPluginRegistrant.register`。
  - `SceneDelegate.swift`：`FlutterSceneDelegate` 空子类。
  - `RunnerTests.swift`：空 `XCTestCase`。
  - 无自定义类名、无 method channel、无原生业务代码。
- 项目已完成去 Flutter：`app/pubspec.yaml`、`app/lib/`、`app/test/`、`.dart_tool` 均已删除（0 个被跟踪文件）；`app/ios/` 是 Dart/Flutter 侧唯一残留的工程目录。

---

## 4. 引用检查（是否被构建/CI/脚本依赖）

### 4.1 检查范围与命令

| 范围 | 命令 | 结果 |
|---|---|---|
| 根级 GitHub Actions | `grep -rn "app/ios\|Runner.xcodeproj\|ios" .github/workflows/` | **无匹配**（3 个工作流全部为 Android/服务端：ci.yml、android-release.yml、android-device-smoke.yml） |
| scripts/ | `grep -rn "app/ios\|Runner.xcodeproj\|ios\|iOS" scripts/` | 无 `app/ios` 引用；仅 `test_android_release_workflow.py` 断言**去 Flutter 后产物不含** `libflutter.so`（方向相反，不受影响） |
| app/android/ | `grep -rn "app/ios\|ios\|iOS" app/android/`（gradle/kt/py） | **无匹配**（唯一命中是 assets 内二进制 rootfs 的字节噪音，非引用） |
| 全部被跟踪文件 | 遍历 `git ls-files` 中非 `app/ios` 的文件搜 `app/ios` / `Runner.xcodeproj` | 命中 3 个文件：`app/.github/workflows/ios-build.yml`、`app/.github/workflows/release.yml`、`docs/deflutter-plan.md`（下详） |

### 4.2 命中项分析

1. **`app/.github/workflows/ios-build.yml`、`app/.github/workflows/release.yml`（被跟踪，但为遗留失效工作流）**
   - 两文件引用 `ios/Runner.xcodeproj/project.pbxproj`（`cd ios` + `pod install` + `xcodebuild -workspace ios/Runner.xcworkspace`），以 `app/` 为假想仓库根编写。
   - **GitHub Actions 只执行仓库根 `.github/workflows/` 下的工作流**；`app/.github/workflows/` 不会被 GitHub 发现/触发。根目录 `.github/workflows/` 仅有 3 个 Android 工作流，且无 `workflow_call` / `repository_dispatch` 指向 `app/.github`。
   - 该工作流还依赖 `flutter pub get`、`pubspec.yaml`、`ios/Podfile`——这些在去 Flutter 后均已不存在，即使手工迁移也无法运行。
   - 结论：`app/.github/workflows/` 是旧 daidai-flutter 仓库的遗留物，与 `app/ios/` 相互依赖但**双双失活**；删除 `app/ios/` 不影响任何可执行 CI。

2. **`docs/deflutter-plan.md`（文档，仅记录性引用）**
   - L106 明确：「`app/ios/` | iOS 工程 | 保留（若本规划仅限 Android；跨端部署才移除）」。
   - L557/L562-563/L582-583 将该评估文档（`docs/ios-residue-assessment.md`，W4）列为删除决策依据。文档引用不构成构建依赖。

3. **`ci-check/`（未跟踪，工作区本地杂项）**
   - `c2_state.py` / `c2_tracked2.py` / `c2_untracked.py` 引用 `app/ios` 作为**状态审计目标**（检查其是否仍存在），属本地排查脚本（`git ls-files ci-check` = 0，未提交、未入 CI），不影响仓库。

### 4.3 风险结论

- **删除 `app/ios/` 后 Android 构建、根级 CI、发布链路均不受影响**：根级 3 个工作流、`scripts/`、`app/android/` 均无任何引用（证据见 §4.1）。
- 唯一"受影响"的是已失活的 `app/.github/workflows/ios-build.yml` 与 `release.yml`——它们本就无法运行，且依赖的 Dart 工程已删除；若未来重启 iOS 计划需重写工作流（可考虑一并清理，但不在本任务范围）。
- **git 历史可完整回溯**：40 个文件存在于 `82e140b` 起的全部提交中，随时可用 `git show 82e140b:app/ios/...` 或 `git checkout 82e140b -- app/ios` 恢复。

---

## 5. 决策建议

### 选项 A：删除（默认推荐）
- 适用前提：项目无 iOS 发布计划（当前现状：无 Dart 代码、无可用 iOS CI、Android 为唯一交付形态）。
- 操作：`git rm -r app/ios`（约释放 1.21 MB 工作区 / 仓库对象可后续 gc 优化）。
- 收益：彻底移除 Flutter 残留，仓库边界清晰；`app/` 下仅剩 Android 原生工程。
- 风险：低。无任何运行依赖；如需 iOS 可用 git 历史恢复模板后 `flutter create --platforms=ios .` 重建。
- 注意：若选择删除，建议一并决策 `app/.github/workflows/`（含 ios-build.yml/release.yml/build.yml/build-native.yml 等旧 Flutter 工作流）是否清理——但这是独立决策，不在本报告强制范围。

### 选项 B：保留（跨端计划时）
- 适用前提：近期有 iOS 发布计划，希望在去 Flutter 后保留 Runner 工程骨架，届时重建原生入口。
- 代价：继续保留 1.21 MB 死代码；`app/ios/` 引用的 `GeneratedPluginRegistrant`、`Flutter.xcconfig` 等生成物已不可再生（Dart 工程已删），保留的骨架**不能直接构建**，仅作模板参考。
- 若选此项，建议至少按 §2.2 说明保留 `Info.plist` 的显示名/ATS 配置备忘（本报告已记录）。

> **默认推荐：选项 A（删除）**——纯模板 + 无依赖 + 可回溯。

---

## 6. 附录：命令证据

```text
# 文件清单与数量
git ls-tree -r HEAD --name-only -- app/ios          # 40 个文件
git ls-tree -r HEAD -l -- app/ios                    # 合计 1,266,102 字节

# 模板性验证
git log --oneline -- app/ios                         # 82e140b（唯一提交）
git log --format=%H -- app/ios                       # 1 个提交哈希

# 引用检查（无匹配项）
grep -rn "app/ios" .github/ scripts/ app/android/    # 均无匹配
grep -rn "Runner.xcodeproj" .github/ scripts/ app/android/   # 均无匹配

# 命中项（非构建依赖）
grep -rln "app/ios\|Runner.xcodeproj" <git ls-files 排除 app/ios>
# → app/.github/workflows/ios-build.yml:113
#   app/.github/workflows/release.yml:334
#   docs/deflutter-plan.md:106（记录性）

# Android 侧无 Flutter 残留佐证
git ls-files app | grep -c "app/pubspec.yaml"        # 0
git ls-files app | grep -c "app/lib/"                # 0
git ls-files app | grep -c "app/test/"               # 0
git ls-files app/android | measure                   # 236（纯原生）
```
