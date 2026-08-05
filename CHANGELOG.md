# Changelog

## v0.4.4

### 修复

- 脚本调试页面直接显示明文 stdout、stderr 和完整 Traceback。
- 失败状态显示退出码和实际错误摘要，并提供高对比度“错误详情”区域。
- 日志接口新增 `content`、`error` 和 `log_count`，保留原始 `logs` 数组兼容性。
- 面板日志纳入最近脚本运行的真实进程输出。
- 修复 Android Python 子进程因错误 `Py_SetPath` 无法导入 `encodings`。
- 修复 Node wrapper 覆盖 `NODE_PATH`，使自动安装的 npm 包可被脚本加载。
- 新增保守型青龙脚本依赖扫描、常用 pip/npm 自动安装及配套文件缺失提示。

### 兼容性

- Python 常用导入映射支持 `yaml→pyyaml`、`bs4→beautifulsoup4`、`Crypto→pycryptodome`。
- Node 常见纯 JavaScript 包可自动安装；相对模块缺失返回 `MISSING_COMPANION_FILE`。
- 不支持 Android ARM64 的原生 Python 包返回 `ANDROID_WHEEL_UNAVAILABLE`，不再盲目源码编译。

## v0.4.3

### 修复

- 脚本运行改为异步启动，避免长脚本触发 Flutter 请求超时。
- Python、JavaScript、TypeScript 和 Shell 脚本运行期间实时增量显示 stdout/stderr。
- 停止脚本现在会终止实际进程，并返回 `stopped`、`exit_code=130`。
- 未保存代码运行会根据脚本扩展名发送正确语言类型。
- 修复脚本复制请求与 Go 后端的 `source_path/target_dir/new_name` 契约差异。
- 脚本重命名、移动和复制不再静默覆盖已有文件。
- 加强脚本路径校验，拒绝 `..`、系统绝对路径、盘符和反斜杠逃逸。
- 保留 APP 使用的 `/目录/脚本.py` 工作区根路径兼容性。
- 补充任务详情中的 `last_run_status`、`last_run_at` 和 `last_log_id`。

### 验证

- 用户提供的 `code_20260802-2.py` 在真机执行成功。
- pip 安装期间日志持续增量更新，最终 `exit_code=0`。
- JS/Python/Shell 导入、直接运行、任务运行及变量调用通过。
- Cron、Hooks、通知、依赖安装、备份恢复和管理 API 回归通过。
- Flutter tests 36/36 通过，Flutter analyze 零告警。
