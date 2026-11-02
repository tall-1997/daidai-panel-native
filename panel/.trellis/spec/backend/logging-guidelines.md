# 日志规范

> 当前项目既有 Gin 请求日志，也有面板运行日志，还会把部分日志写入数据目录。

---

## 当前真实做法

- 使用标准库 `log`
- Gin 使用自定义 writer 输出请求日志
- 面板启动时会根据运行模式把日志写到 `panel.log` 或 stdout
- 数据库层和部分兼容迁移也会通过 `log.Printf` 记录信息

---

## 应该记录什么

- 启动/关闭关键流程
- 数据库初始化和兼容迁移结果
- 调度器、资源监控、自动更新等关键后台流程
- 会影响用户使用的失败信息
- 安全相关事件，如登录、锁定、白名单限制等

---

## 不应该记录什么

- 明文密码
- token、refresh token、secret、私钥
- 用户敏感配置的完整原文
- 不必要的大段重复噪声日志

---

## 日志风格建议

- 文案尽量直接，便于排查。
- 如果是兼容逻辑、迁移逻辑、历史遗留兜底逻辑，建议加中文注释解释代码目的。
- 对可恢复的异常优先 `log.Printf`，对必须终止启动的问题再 `log.Fatalf`。

---

## 常见错误

- 把敏感信息直接打印到日志。
- 同一错误在循环中重复刷屏。
- 发生失败时没有打关键上下文，后面难排查。
- 只写“失败了”，不写哪个步骤失败。

## 约定：启动耗时与关停日志（v3.3.5，#156）

**是什么**

- **启动慢步骤**：`server/main.go` 的 `logSlowStartupStep(step, started)`，某一步用时 ≥ `startupStepSlowThreshold`（3 秒）才打一行
  `[启动耗时] <步骤> 用时 <耗时>`（耗时按 10ms 取整，Go `Duration` 写法，如 `14.27s`）。目前计时三步（按 main 里的先后）：
  `初始化配置与数据库`（`appboot.InitWithConfig`）、`启动校验已安装依赖`（`verifyInstalledDeps`）、`初始化任务调度器`（`service.InitSchedulerV2`）。
  - 被计时的函数**保持直接调用**，不要包成传函数值的 helper：`service/startup_wiring_test.go` 按 AST 核对 main 里的调用顺序。
  - v3.3.6（#158）起，两遍整棵脚本目录的遍历——`service.QuarantineUnexpectedScriptEntriesOnStartup`（隔离顶层污染目录、清悬空的面板式 `node_modules` 软链）
    与 `service.CleanupManagedHelperCopiesUnderRoot`（清子目录里旧版面板留下的通知脚本副本）——放进 main 里同一个后台 `go` 协程，**不计时**：
    v3.3.5 的两个步骤名 `整理通知辅助脚本（遍历脚本目录）`、`隔离脚本目录污染项、清理残留软链（遍历脚本目录）` 不会再出现。
    后台只打它们原有的逐项与出错行（`unexpected script entry quarantined: …`、`leftover node_modules link removed: …`、`cleanup duplicated notify helpers failed: …`，
    以及各自的 `… failed: …`）。不要给这个协程补耗时行：它不挡监听，耗时行只会被读成「启动慢」。根目录两份 helper（`service.EnsureBuiltinNotifyHelpers`）照旧同步准备，同样不计时。
    放置与时序的契约见 `quality-guidelines.md`「场景：脚本目录污染隔离与 Windows 资源监控」。
  - 启动校验内部另有分类型的 `[启动校验] 校验 N 条已安装依赖耗时 …`，同样是 3 秒门槛，口径见 `quality-guidelines.md`「契约 S1」。
- **关停**（`shutdownPanel` 及它调用的各步，标准库 `log`，进面板日志）：沿用既有的英文短句。正常路径打入口一行、`scheduler v2 stopped`、`backup scheduler stopped`、
  `subscription scheduler stopped`、`database closed`、`panel shutdown finished in <耗时>`（毫秒取整）；其余的行只在真发生时出现：

| 时机 | 日志行 |
|---|---|
| 进入关停 | `received <signal>, shutting down panel` / `panel exit requested (code=N), shutting down panel` / `server failed: <err>` |
| 杀任务 | `interrupted N running task process(es) during panel shutdown`（N = 这次终止的任务进程数，含正在跑的钩子） |
| 杀任务时有进程组扛过 TERM 宽限、被强制 KILL（v3.3.6，#159） | `N task process group(s) still running 2s after SIGTERM, killed`：只有真 KILL 了才打（N = 被 KILL 的进程组数），紧接着才是上一行；Magisk 部署下永远不出现（见表下） |
| 某一步到点放手 | `scheduler v2: cron callbacks still running after 1s, not waiting for them`、`timed out waiting for scheduler workers to finish`、`timed out waiting for running task cleanup`、`backup scheduler stopped (a scheduled backup is still running, not waiting for it)`、`subscription scheduler stopped (a scheduled pull is still running, not waiting for it)`、`server graceful shutdown failed: <err>`、`close database failed: <err>` |
| 截止时兜底 | `revoked N script token(s) of unsettled task run(s) during shutdown`、`marked N active task(s) as interrupted during shutdown` |
| 总兜底（随后 `os.Exit(1)`） | `面板关停超过 8s 仍未完成，强制退出` |
| 下次启动 | `removed N leftover task temp entries from <dir>`（真删了才打） |

- **关停终止任务的宽限**（v3.3.6，#159 修复 B）：`StopAllRunningTasks` 先对所有登记的进程组（任务进程加正在跑的钩子）发 SIGTERM，
  共用一个 `shutdownTermGrace`（2 秒）截止时间逐组等，到点还在的统一 SIGKILL。`… still running 2s after SIGTERM, killed` 只在 `killed > 0` 时打，
  `2s` 是这次用的宽限（`termGrace`，Go `Duration` 写法）；它是事后判断「关停为什么多花了 2 秒」的唯一依据。
  **Magisk 部署**（`playwrightMagiskRuntime()`：`DAIDAI_MAGISK_SHELL_VERSION` 非空，或 `IsMagiskModuleRuntime()`）关停时宽限为 0，
  所有进程组立即整组 SIGKILL（与 v3.3.5 一致：模块动作按钮「停止」是 `kill -TERM` 面板、`sleep 2`、还在就 `kill -KILL`，装不下 SignalStop 的 1 秒加 2 秒宽限），
  所以这一行在 Magisk 上永远不会打，杀任务时只有 `interrupted …` 那一行。手动 / 批量 / 定时停止、超时、调试运行停止不走这里，在 Magisk 上照旧先 TERM、5 秒后才 KILL。
- **entrypoint**：`log()` / `fail()` 每行以 `YYYY/MM/DD HH:MM:SS`（`date '+%Y/%m/%d %H:%M:%S'`）开头，与 Go `log` 的默认格式一致，后面跟 `[entrypoint]` / `[entrypoint][ERROR]`；
  收到停止信号时打 `收到停止信号，等待面板收尾...`。

**为什么**

- 慢启动（#156）以前只能看到「过了很久才开始监听」，对不上是哪一步；门槛设 3 秒，正常启动一行都不多打。
- 关停的每一步都有上限、到点就放手往下走，日志是事后判断「卡在哪一步、有没有执行被兜底标中断、凭据有没有吊销」的唯一依据。
- entrypoint 与面板日志用同一种时间格式，`docker logs` 里两边的时间线能直接对上。

**例子**（格式示意，不是实测输出；`……` 处省略了其余行）

```text
2026/10/05 09:12:40 [启动耗时] 启动校验已安装依赖 用时 14.27s
……
2026/10/05 21:03:11 [entrypoint] 收到停止信号，等待面板收尾...
2026/10/05 21:03:11 received terminated, shutting down panel
2026/10/05 21:03:11 interrupted 2 running task process(es) during panel shutdown
……
2026/10/05 21:03:11 panel shutdown finished in 64ms
```

（v3.3.6 起终止任务先 TERM，等组退空是每 50ms 查一次，有任务在跑时关停通常要几十毫秒；有进程组扛满 2 秒宽限时，`interrupted …` 前面会多一行 `still running … killed`，总耗时也多出约 2 秒。）

## 约定：面板日志接口只留尾部（v3.3.6，#159 修复 E）

**是什么**

- `GET /api/v1/system/panel-log`（`handler/system.go` 的 `PanelLog`，`RequireUserToken` + `RequireAdmin`）参数不变：`lines`（1~10000，其它值回落 100）、
  `keyword`（子串匹配）、`level`（最低级别，`service.MatchPanelLogLevel`）。
- 照旧用 `bufio.Scanner`（`Buffer(64KiB, 1MiB)`）**把整个 `panel.log` 从头扫一遍**，但只用一个长度为 `lines` 的环形缓冲留命中行：
  第 `total` 条命中行写进 `ring[total%lines]`，扫完按文件顺序取最后 `min(total, lines)` 行。内存只和 `lines`（≤ 10000 行）有关，与文件大小无关；扫描耗时不变。
- 响应与改动前逐字节一致：`{"data":{"logs":[…],"total":N,"level":"<原样回传>"}}`。
  - `total` 是**全文件**命中筛选的行数（网页「共 N 行」），不是这次返回了几行；
  - 一行都没命中时 `logs` 是 `null`，不是 `[]`；
  - 文件不存在时仍是 `{"data":{"logs":[]}}`，没有 `total` / `level`；
  - 某一行超过 1MiB 时 scanner 停在那里、之后的行静默不计（不查 `scanner.Err()`），与改动前相同，要改得单独立项并补用例。
- 网页设置页只在「面板日志」子标签激活且页面可见时每 3 秒轮询（`usePanelLogViewer(isTabActive)`）；APP 只读 `logs`。

**为什么**

- 改动前整份读进 `[]string` 再取尾，panel.log 越大峰值越高（#159 调研：合成 100MB 文件时 RSS 212–282MB），设置页还会在后台每 3 秒拉一次。
- 刻意照旧整文件扫描、只省内存：改成只读文件末尾一段的话，`total` 变成「末尾那段里的命中数」，按关键词 / 级别筛选也找不到前面的命中。

**必须有的测试**：`handler/system_panel_log_test.go` 的 `TestPanelLogKeepsTailAndWholeFileTotal`——用例里的 oracle 就是改动前的算法（整份读、筛选、取尾）。
25003 行加一行 70KiB 长行，覆盖 `lines=100`、`lines=10000`（环形缓冲绕好几圈）、`level=error`、关键词命中不足 `lines`（不绕圈）、关键词无命中（`logs` 为 `null`、`total` 为 0）、
`lines=0` / `20000` 回落 100、关键词加级别；`logs` 按 JSON 原文逐项比，再比 `total` 与 `level`。文件不存在时响应体原样是 `{"data":{"logs":[]}}`。

**错误写法**

```go
// 错误一：为了省时间 Seek 到文件末尾只读一段 —— total 不再是全文件命中数，网页「共 N 行」变小，前面的命中也筛不出来
// 错误二：没命中时返回空切片 —— logs 从 null 变成 []，与改动前不一致
tail := make([]string, 0, lines) // total == 0 时也非 nil，序列化成 []
```

## 约定：任务结束后清理残留进程的那一行任务日志（v3.3.6，#159 修复 A）

**是什么**

- 常量 `leftoverProcessCleanupNotice`（`server/service/script_runner.go`），前后各一个 `\n`：

  ```text
  \n[已结束残留的后台进程：主命令退出后，它的进程组里还有进程在运行（多为 nohup、& 放到后台的），已发送结束信号，5 秒内不退出会被强制结束。需要常驻请用 setsid nohup 命令 >/dev/null 2>&1 & 启动，或在「系统设置 → 任务运行」关闭「任务结束后清理残留进程」]\n
  ```

- **只在组里真有进程时写**：`runSingleCommand` 在主命令返回后判断
  `!timedOut && plan.ShouldCleanupProcessGroup != nil && plan.ShouldCleanupProcessGroup() && processGroupAlive(process.Pid)`，
  四条都成立才先 `TerminateProcessGroup(process)`、再写这一行；组里本来就空时不写。每次 `runSingleCommand` 最多一行。
  不写的情形：超时（超时分支自己整组 TERM→KILL）、开机任务、系统配置 `cleanup_leftover_processes` 关闭、这次执行已被手动停止或面板正在关停
  （`runTask` 挂的判定闭包要求 `runStopKind(run) == runStopNone`）、钩子（不经过 `runSingleCommand`）、Windows（`processGroupAlive` 恒为 false）。
- **只进任务日志**：直接 `onOutput(leftoverProcessCleanupNotice)`，不经 `emitChunk`（与「任务超时」「被信号终止」两行同一写法，日志被截断后这一行照样出现），
  进 TinyLog、日志文件与落库的日志正文；**不写面板日志**（不 `log.Printf`）。
- **conc 模式**：每个账号各走一次 `runSingleCommand`，各自最多一行；`prefixedOutput` 给整段加 `[<变量名>#<N>] ` 前缀，
  开头那个 `\n` 让前缀单独占一行，提示行本身仍从行首开始（日志里是 `[LEAK_ACC#1] ` 换行，下一行才是 `[已结束残留的后台进程：…]`）。
- **面板元信息行**：前缀 `[已结束残留的后台进程：` 已登记进 `task_executor.go` 的 `panelMetaLinePrefixes`，成功摘要靠 `isPanelMetaLine` 滤掉它
  （成功的任务最容易出现这一行，不登记就会挤进成功通知那 30 行摘录）。失败摘要的 `normalizeTaskFailureLines` 也只丢**这一个**前缀：
  失败时它常常是最后一行，不丢的话 Python 摘要认不出最后一行的异常、通用摘要的「上下文」也会取成这行提示；
  别改成整体套 `isPanelMetaLine`——`[脚本进程被信号终止：…]` 这类行对失败诊断有用。

**为什么**

- 文案写「已发送结束信号，5 秒内不退出会被强制结束」而不是「已结束」：补刀在 `TerminateProcessGroup` 起的协程里，面板或 `ddp task run` 在这 5 秒内退出时补不上。
- 开头的 `\n` 让 conc 前缀单独成行、提示行从行首开始，`isPanelMetaLine` 才认得出；结尾的 `\n` 免得后面的「=== 执行结束」粘在这一行末尾。
- 组空不写：没清理任何东西还写一行，只会让人以为脚本出了问题。

**必须有的测试**

- `service/task_process_cleanup_linux_test.go`（`//go:build linux`，WSL / CI 跑才算数）：`TestLeftoverProcessCleanupKillsBackgroundSleep`（恰好一行、`isPanelMetaLine` 认得出、
  结束行仍是「退出码 0」、结算为成功）；`…SilentWhenGroupEmpty`、`…SparesSetsidProcess`、`…SkipsStartupTasks`、`…HonorsSwitch`、`…LeavesHooksAlone`、`…SkippedAfterManualStop` 都是 0 行；
  `…CoversConcAccounts` 两行，各自前面是 `[LEAK_ACC#N] \n`。
- `service/task_notification_test.go`：`TestSummarizeTaskSuccessOutputDropsBannersAndMeta`（用真实常量构造的行被成功摘要滤掉）；
  `TestSummarizeTaskFailureOutputCondensesPythonTraceback` 的「末尾跟着清理残留进程的提示行」与 `TestSummarizeTaskFailureOutputGenericContextSkipsLeftoverNotice`
  （失败摘要只丢这一行，`[脚本进程被信号终止：…]` 照旧能当上下文）。

## Scenario: 任务日志流中的终端覆盖刷新

### 1. Scope / Trigger
- Trigger: 修改 `server/service/script_runner.go`、`server/service/task_executor.go`、`server/service/scheduler.go`、`server/handler/log.go` 这条任务日志实时输出链时必须看本节。
- 原因: 进度条、下载器、CLI 状态条常用裸 `\r` 回到当前行开头覆盖内容；如果后端在读取脚本输出或写 SSE 时把 `\r` 直接洗成 `\n`，前端就只能把每次刷新渲染成新行，日志会被严重刷屏。

### 2. Signatures
- 脚本输出回调签名: `type OnOutputFunc func(chunk string)`
- 任务实时日志 SSE: `GET /api/v1/logs/:id/stream`
- 历史日志读取: `TaskLog.Content` / `TaskLog.LogPath`

### 3. Contracts
- `OnOutputFunc` 传递的是原始输出片段，不保证一定是完整一行。
- 片段内允许出现三种边界:
  - `\n`: 正常换行
  - `\r\n`: Windows 风格换行
  - 裸 `\r`: 终端单行覆盖刷新
- `writeSSEData` 只能把 `\r\n` 归一成 `\n`，不能把裸 `\r` 再改成 `\n`。
- **裸 `\r` 必须落在一条 `data:` 行的末尾，不能埋在行中间。** 这是上一条的补充约束，两条同时成立才算正确：
  - SSE 线格式里 CR、LF、CRLF **都是行终止符**。`data: aaa\rbbb\n` 会被标准 `EventSource` 拆成两行：`data: aaa` 和 `bbb`；后半段没有字段名，按规范整行**丢弃** —— `bbb` 静默消失。
  - 实现方式是**按裸 `\r` 分帧**：每碰到一个后面还有内容的裸 `\r`，就让它成为当前帧最后一条 `data:` 行的结尾，**写完帧终止空行**，剩余内容**另起一帧**。字符一个不增不减。
  - ⚠️ **绝不能改成「同一帧里多条以 `\r` 结尾的 `data:` 行」。** SSE 规定同一个 event 的多条 `data:` 行用 `\n` 拼接，那样裸 `\r` 会变成 `\r\n`，而前端 `web/src/utils/ansi.ts` 把 `\r\n` 当**真实换行**，进度条立刻刷屏 —— 正好是本节要防的那个回归。
  - 分帧方案对前端**零改动**：`web/src/utils/sse.ts` 按 `\n\n` 切帧、且对 `data:` 行末尾的 `\r` 刻意不 trim；`LogViewer.vue` 与 `logs/index.vue` 拿到相邻消息是**首尾相接 append**、不补任何分隔符。所以多帧拼起来与改动前的单帧**逐字节一致**。
  - **不变量**：把所有帧的 data 值按顺序首尾相接拼回去，必须逐字节等于输入（`\r\n`→`\n` 除外）。
- 任务执行器 / 调度器把输出写入 `TinyLog` 和日志文件时，必须原样写入片段，不能统一补 `+ "\n"`。

### 4. Validation & Error Matrix
- 输出只包含普通文本 + `\n` -> 正常按多行日志展示
- 输出包含 `\r\n` -> 视为真实换行
- 输出包含裸 `\r` -> 必须保留，交给前端按“覆盖当前行”处理
- 如果在任一后端环节把裸 `\r` 改成 `\n` -> 进度条日志会刷屏，属于行为回归
- 裸 `\r` 埋在 `data:` 行中间（`data: aaa\rbbb`）-> 标准 `EventSource` 把 `bbb` 当成无字段名的行**静默丢弃**，用户看到「进度条后面的输出没了」
- 裸 `\r` 之后另起一帧（`data: aaa\r` + 帧终止空行 + `data: bbb`）-> 内容完整、覆盖刷新语义保持、前端零改动，这是唯一正确的组合
- 裸 `\r` 之后不另起帧、只在同一帧里多写一条 `data:` 行 -> 同帧多条 data 按规范用 `\n` 拼接，裸 `\r` 变成 `\r\n`，进度条重新刷屏，同样是回归

### 5. Good/Base/Bad Cases
- Good: `xx 10%\rxx 20%\rxx 30%\n完成\n` 最终前端只显示一条持续刷新的进度行，再接一条“完成”
- Base: 普通 `print/console.log` 输出不受影响，仍然是逐行日志
- Bad: SSE 层把 `\r` 直接替换为 `\n`，导致 `10%`、`20%`、`30%` 全部堆成独立多行
- Bad: SSE 层只按 `\n` 切行，`\r` 留在 `data:` 行中间 —— 自研解析器看着正常，换成标准 `EventSource`（或中间有严格代理）时 `\r` 后面的内容整段消失，且**没有任何报错**
- Bad: 把 `\r` 后面的内容放进**同一帧**的下一条 `data:` 行（而不是另起一帧）—— 内容回来了，但同帧多条 data 会被 `\n` 拼接，裸 `\r` 变 `\r\n`，进度条重新刷屏

### 6. Tests Required
- 后端测试: `cd server && go test ./...`
- 回归点:
  - `server/handler/log_stream_regression_test.go` 断言 `writeSSEData` 会保留裸 `\r`
  - 同一个文件还要断言**分帧位置**：裸 `\r` 只能出现在某条 `data:` 行的最后一个字符，绝不能出现在行中间（一条断言两件事，否则「保留了但埋在中间」会全绿通过）
  - 任务执行日志链编译和现有 handler/service 测试全部通过

### 7. Wrong vs Correct
#### Wrong
```go
data = strings.ReplaceAll(data, "\r", "\n")
fmt.Fprintf(tinyLog, "%s\n", line)
logMgr.Write(fullLogPath, line+"\n")
```

```go
// 错误：只按 \n 切，裸 \r 被埋在 data: 行中间。
// 自研解析器看着没问题，标准 EventSource 会把 \r 后面的半截当成无字段名的行丢掉。
for _, line := range strings.Split(data, "\n") {
    fmt.Fprintf(w, "data: %s\n", line)
}
```

#### Correct
```go
data = strings.ReplaceAll(data, "\r\n", "\n")
fmt.Fprint(tinyLog, chunk)
logMgr.Write(fullLogPath, chunk)
```

```go
// 正确：\n 照旧在帧内切行；裸 \r 之后【另起一帧】，
// \r 本身留在上一帧最后一条 data: 行的末尾 —— 字符保住了，也不会被 EventSource 吞掉。
// 前端零改动：帧与帧首尾相接 append，拼回去与改动前逐字节一致。
data = strings.ReplaceAll(data, "\r\n", "\n")
for {
    segment, rest := data, ""
    // idx+1 == len(data) 表示 \r 已经在整段末尾，本来就落在行末，不用再切。
    if idx := strings.IndexByte(data, '\r'); idx >= 0 && idx+1 < len(data) {
        segment, rest = data[:idx+1], data[idx+1:]
    }
    for _, line := range strings.Split(segment, "\n") {
        fmt.Fprintf(w, "data: %s\n", line)
    }
    fmt.Fprint(w, "\n") // 帧终止空行必须写在循环内
    if rest == "" {
        return
    }
    data = rest
}
```

## Scenario: 日志清理的记录与文件一致性

### 1. Scope / Trigger

- Trigger: 修改 `server/service/log_cleanup.go`、`server/service/log_manager.go`、`server/service/task_log_archive.go`、`server/handler/log.go`、
  `server/handler/task_logs.go`、`server/cmd/ddp/commands.go` 的 `clean-logs`，或新增任何一处「清理日志」入口、任何删 `task_logs` 行的代码时必须看本节。
- 原因: 「清理日志」在这个仓库里同时意味着**删 `task_logs` 行**和**删磁盘 `.log` 文件**两件事。
  v3.3.2 / issue #144 之前三个入口各做各的（自动清理删行也删文件、`/logs/clean` 只删行、`/tasks/clean-logs` 只删文件），
  同一句话在三个地方是三种结果，而这个不一致在规范里零记载——谁改都不知道另外两处存在。
- v3.3.6 / issue #158 起多一层：仪表板的今日 / 昨日 / 执行趋势会把删掉的行加回来，前提是删行时先把它们按天并进 `task_log_daily_stats`，
  所以删行本身只能经 `service.DeleteTaskLogs`。计数表、全部删除入口、恢复备份与两道护栏的完整契约在 `database-guidelines.md`
  「删除 task_logs 一律经 `service.DeleteTaskLogs`」；本节只写清理与按条删这条链路上调用方要守的约定。

### 2. Signatures

- 删 `task_logs` 行的唯一入口（v3.3.6）: `service.DeleteTaskLogs(tx *gorm.DB, where string, args ...interface{}) (int64, []string, error)`
  ——同一事务里依次「把命中行按天并进 `task_log_daily_stats` → 取命中行里非空的 `log_path` → 带 `model.TaskLogDeleteGuardKey` 标记删行」，返回（删除行数, 路径, 错误）
- 清理入口共用的删行 + 删文件: `cleanExpiredTaskLogRows(extraCond string, extraArg interface{}, days int, logDir string) (int64, int)`（包内函数，`extraCond` 只有 `task_id = ?` / `task_id NOT IN ?` 两种）
- 行 + 文件一起清（按天数）: `service.CleanLogsOlderThan(days int) (int64, int)`
- 行 + 文件一起清（按任务分组）: `service.CleanLogsByRetentionPolicy(globalDays int) (int64, int)`
- 只删文件（按 `log_path`）: `service.DeleteLogFilesForRecords(logPaths []string, logDir string) int`
- 只删文件（按 ModTime 扫盘）: `service.CleanOldLogs(logDir string, days int) int`
- 正在写入的文件判定: `(*LogStreamManager).IsStreamOpen(filePath string) bool`
- 入口: 自动清理 worker（`cleanupOldLogs`）、`DELETE /api/v1/logs/clean`、`DELETE /api/v1/tasks/clean-logs`、`ddp clean-logs`；
  按条删的 `DELETE /api/v1/logs/:id`、`DELETE /api/v1/logs/batch`、`POST /api/v1/logs/batch-delete`（handler 自己开事务调 `DeleteTaskLogs`，提交后 `DeleteLogFilesForRecords`）

### 3. Contracts

**记录与文件的一致性**

- **删 `task_logs` 行的代码路径必须同时按 `log_path` 删磁盘文件**，统一走 `CleanLogsOlderThan` /
  `CleanLogsByRetentionPolicy` / `DeleteLogFilesForRecords`，**不要再起第四条清理路径**。
- 🔴 删行本身一律经 `service.DeleteTaskLogs`（v3.3.6 / #158；唯一例外是恢复备份勾「日志」时整表 `deleteAll`，计数随之整体换成备份里的），
  **不要自己写 `Delete(&model.TaskLog{})`**：绕过它删掉的行不进执行趋势计数，仪表板的今日 / 昨日 / 趋势跟着变少，而且完全静默。源码语法扫描（`TestTaskLogDeletesOnlyGoThroughDeleteTaskLogs`）与 `testutil.SetupTestEnv`
  挂的测试期 Delete 回调都会拦；测试代码要删日志也只能走 `DeleteTaskLogs` 或原生 `Exec`。
- 顺序固定是「`database.DB.Transaction` 里调 `DeleteTaskLogs`（归档 → 取 `log_path` → 带标记删行）→ 事务提交之后再 `DeleteLogFilesForRecords(paths, logDir)`」：
  行一删，路径就再也查不回来了；文件等提交之后再删，回滚时文件不动。
  - `DeleteTaskLogs` 必须是这个事务里的**第一条语句**：WAL 下事务「先读后写」，中间若有别的进程（`ddp`）提交过写，后面的写直接报
    `database is locked (517)`，`busy_timeout` 救不了。不要在它前面先 `Pluck` / `Count`。
  - `where` 只能是代码里写死的 SQL 片段（`?` 占位）：它被原样拼进归档那条原生 SQL。三个清理入口的条件只在 `cleanExpiredTaskLogRows` 里拼这一份。
  - 删行失败时事务整体回滚（行与计数都没动），`cleanExpiredTaskLogRows` 打 `log cleanup: delete TaskLog records failed: …` 后返回 `(0, 0)`，
    **这一轮不按 `log_path` 删文件**；`CleanLogsOlderThan` / `CleanLogsByRetentionPolicy` 随后的 `CleanOldLogs` 按 ModTime 扫盘照旧无条件执行（与改动前相同）。
- 🔴 **任何按 status 过滤 `task_logs` 的 WHERE 都必须写成 `(status IS NULL OR status <> ?)`**，
  绝不能只写 `status <> ?`。`task_logs.status` 是可空列（`model.TaskLog.Status` 是 `*int`），
  SQL 三值逻辑下 `NULL <> 2` 求值为 **NULL 而不是 TRUE**，只写后者会把所有 status 为 NULL 的历史行**整批静默漏掉**——
  永远清不干净，而且接口照样返回成功，从响应里看不出任何异常。
- 🔴 **删任何日志文件前必须先过 `LogStreamManager.IsStreamOpen`**，入参必须是
  `filepath.Join(logDir, relPath)` 这个**未经 `EvalSymlinks`** 的路径——写入方（`task_executor.go` / `scheduler.go`）
  就是拿它当 map key 的，做了归一化就跟写入方对不上，判定恒为 false，这道保护等于没有。
  Linux 上 `os.Remove` 一个已 open 的文件**不报错**，随后的写入会全进被 unlink 的 inode，
  任务跑完日志凭空消失、一句报错都没有；Windows 上则是 Remove 直接失败。跳过的文件留给下一轮清理收。
- `CleanOldLogs` **刻意只管文件、不碰 DB 行**，不要「顺手」给它加上删行：
  `ddp clean-logs` 这条 CLI 与 `README.md`、`cmd/ddp/help.go` 的语义都是「清理任务日志文件」，改它要连文档一起改。
  它同时是「DB 行早没了、文件还在」这类存量垃圾的**唯一**清理手段（按 `log_path` 删根本找不到这些文件），
  所以不能被按 `log_path` 删取代，两者是互补关系。

**自动清理 vs 手动清理的分野**

- 自动清理 worker 走 `CleanLogsByRetentionPolicy`：任务自己设了 `log_retention_days` 就按它的 cutoff，其余跟随全局。
- 🔴 两个手动入口（`DELETE /logs/clean`、`DELETE /tasks/clean-logs`）**刻意仍走 `CleanLogsOlderThan`**，
  不要「统一一下」改成走分组。手动清理是用户明确指定「保留最近 N 天」的一次性动作，
  掺进任务级天数就会删掉用户刚说要留的日志（某任务设 1 天，用户点清理 30 天，结果它 1 天前的全没了）——
  那是越权删除，不是功能。
- **分组依据只认 `task_logs.task_id`，禁止从日志目录名解析 `task_<ID>`**：
  恢复备份时 `backup_runtime.go` 的 `restoreTasks` 把 `item.ID` 置 0、任务重新分配 ID，
  而 `log_path` 与磁盘目录是原样拷回的，按目录名取天数会把 A 任务的设置套到 B 任务头上。
- **分组清理末尾那次 ModTime 扫盘的天数必须取 `max(全局, 所有任务级天数)`**，不能用全局天数：
  扫盘只看文件时间、认不出文件属于哪个任务，某任务把天数调得比全局长时用全局天数扫，
  会造出「DB 行还在、文件没了」，用户点开日志是一片空白。
  代价是这种情况下孤儿垃圾要等更长天数才被扫走——可以接受；
  把天数调短这个主用例完全不受影响（那时 `max` 就等于全局）。
- 覆盖表只收 `log_retention_days IS NOT NULL AND log_retention_days > 0` 的任务，
  顺带挡掉手工改库塞进来的 0 和负数。

### 4. Validation & Error Matrix

- 只删 DB 行、不删文件 -> 磁盘上堆出永远没人认领的孤儿 `.log`，用户看到「清理了却没释放空间」
- 只删文件、不删 DB 行 -> 列表里还有记录，点开是空白
- WHERE 只写 `status <> ?` -> status 为 NULL 的历史行一条都删不掉，**静默**，接口仍回成功
- 删文件前不过 `IsStreamOpen` -> Linux 上正在跑的那次执行的日志写进被 unlink 的 inode，**静默丢失**
- `IsStreamOpen` 传了 `EvalSymlinks` 之后的路径 -> 判定恒 false，等于没有这道保护
- 手动入口改走 `CleanLogsByRetentionPolicy` -> 删掉用户在这次操作里明确说要留的日志
- 分组按目录名 `task_<ID>` 取天数 -> 恢复备份后 A 任务的保留天数被套到 B 任务上
- 扫盘用全局天数而非 `max` -> 「行还在、文件没了」
- 自己写 `Delete(&model.TaskLog{})`（v3.3.5 及以前本节写的「正确写法」）-> 行与文件一致了，但这批行没并进 `task_log_daily_stats`，
  仪表板的今日 / 昨日 / 趋势**静默变少**（#158 的原症状）；语法扫描与测试期护栏让用例变红
- 在同一事务里先读、再调 `DeleteTaskLogs` -> WAL 下撞 `database is locked (517)`
- 事务提交之前就删文件 -> 回滚之后「行还在、文件没了」
- `DELETE /logs/:id`、`DELETE /logs/batch`、`POST /logs/batch-delete` 的事务出错 -> 500「删除日志失败」（v3.3.6 起；以前单删误报 404「日志不存在」、
  批删回 200「已删除 0 条日志」）。没出错、只是一条都没命中 -> 单删照旧 404「日志不存在」，批删照旧 200「已删除 0 条日志」

### 5. Good/Base/Bad Cases

- Good: 自动清理跑完，设了 1 天的高频任务只剩当天日志，其余任务按全局天数保留，正在跑的那次执行的日志文件完好；
  仪表板的 7 天趋势与今日 / 昨日卡片与清理前逐字段相同（v3.3.6）
- Base: 没有任何任务设过 `log_retention_days` 时，`CleanLogsByRetentionPolicy` 的行为与按全局天数一刀切逐字节一致
- Bad: 新加一处「清理日志」按钮，自己写一段 `Delete(&model.TaskLog{})` 就收工，文件留在盘上，也没经 `DeleteTaskLogs`，执行趋势跟着变少
- Bad: 为了「三个入口统一」把手动清理也改成按任务分组
- Bad: 觉得 `CleanOldLogs` 不删记录是遗漏，给它补上删行——`ddp clean-logs` 的语义随之改变，且文档没跟

### 6. Tests Required

- 后端测试: `cd server && go test ./...`
- 回归点:
  - 存量行 `status` 为 NULL 时仍能被按天数清掉（把 WHERE 改回 `status <> ?` 必须变红）
  - 文件仍被 `LogStreamManager` 持有时跳过删除、DB 行的处理不受影响
  - 任务设了比全局短的天数 -> 只有该任务的日志按短天数清，其余按全局
  - 任务设了比全局长的天数 -> 扫盘按 `max` 走，该任务在全局天数之前的文件没被扫走
  - 手动清理入口在有任务级天数的库上，结论与「按全局天数一刀切」一致
  - 删行被 `BEFORE DELETE` 触发器拦下时整体回滚：计数表 0 行、行还在；`cleanExpiredTaskLogRows` 返回 `(0, 0)` 且按 `log_path` 那份文件还在
    （`service/task_log_archive_test.go` 的 `TestDeleteTaskLogsRollsBackArchiveWhenDeleteFails`）
  - 事务里的语句依次是 `INSERT INTO task_log_daily_stats` → `SELECT log_path FROM task_logs` → `DELETE FROM task_logs`（`TestDeleteTaskLogsArchiveIsFirstStatementInTransaction`）
  - 三个清理入口、单删、两个批删、删任务（单个与两个批量入口）各真删掉 > 0 行之后：计数表各列之和 = 删除数，仪表板 range=1 / 7 / 30 逐字段不变
    （`handler/task_log_trend_history_test.go` 的 `TestDashboardTrendSurvivesEveryLogDeletionEntry`）
  - 单删、批删的事务出错回 500「删除日志失败」，计数表 0 行、行还在（`TestLogDeleteFailureReturns500AndKeepsCounts`）

### 7. Wrong vs Correct

#### Wrong

```go
// 错误一：NULL status 的历史行永远删不掉，且完全静默。
db.Where("started_at < ? AND status <> ?", cutoff, model.LogStatusRunning).Delete(&model.TaskLog{})

// 错误二：先删行再想去拿 log_path —— 路径已经没了。
db.Where("started_at < ?", cutoff).Delete(&model.TaskLog{})
db.Model(&model.TaskLog{}).Pluck("log_path", &paths)

// 错误三：删文件前不问有没有人正在写它。
os.Remove(filepath.Join(logDir, relPath))

// 错误四：v3.3.5 及以前写在本节的「正确写法」——先 Pluck 再自己 Delete。行与文件一致了，
// 可这批行没并进 task_log_daily_stats，仪表板的今日 / 昨日 / 趋势跟着变少（#158）；
// 测试期护栏会让它直接报「task_logs 只能经 service.DeleteTaskLogs 删除」。
pathQuery.Pluck("log_path", &paths)
deleteQuery.Delete(&model.TaskLog{})
```

#### Correct

```go
// 删行一律经 DeleteTaskLogs：同一事务里「并入执行趋势计数 → 取 log_path → 带标记删行」，
// 事务提交之后再删文件；WHERE 照旧带上 (status IS NULL OR status <> ?)，只在这里拼一份。
where := "started_at < ? AND (status IS NULL OR status <> ?)"
args := []interface{}{cutoff, model.LogStatusRunning}

var deleted int64
var paths []string
if err := database.DB.Transaction(func(tx *gorm.DB) error {
    var err error
    deleted, paths, err = DeleteTaskLogs(tx, where, args...) // 必须是这个事务里的第一条语句
    return err
}); err != nil {
    log.Printf("log cleanup: delete TaskLog records failed: %v", err)
    return 0, 0 // 整体回滚：行与计数都没动，这一轮也不按 log_path 删文件
}
// 删文件统一走它：内部逐条过 IsStreamOpen + ResolveWithinBase，并清掉被删空的 task_ 目录。
return deleted, DeleteLogFilesForRecords(paths, logDir)
```
