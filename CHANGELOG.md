# Changelog

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
