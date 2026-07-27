呆呆面板 (Dumb Panel) Windows 单机版
=====================================

系统要求
  - Windows 10 或更高（amd64 架构）

快速开始
  1. 把整个文件夹解压到任意位置
     - 建议避免路径含空格或中文，比如 D:\daidai-panel 即可
     - 不建议放到 C:\Program Files 之类的系统保护目录
  2. 双击 start.bat 启动服务
  3. 浏览器访问 http://localhost:5700
  4. 第一次打开需要初始化管理员账号
  5. 关闭控制台窗口即停止服务

数据和配置
  - 所有面板数据保存在当前目录下的 Dumb-Panel\
    （daidai.db、脚本、日志、备份、依赖环境等）
  - 迁移 / 备份只需拷贝 Dumb-Panel\ 文件夹
  - 修改监听端口等启动参数见 config.yaml

可选：脚本执行环境
  如需面板调度 Python / Node.js 脚本，请自行安装：
  - Python 3.10 或更高: https://www.python.org/downloads/windows/
  - Node.js 20 LTS:     https://nodejs.org/zh-cn/download
  安装时勾选 "Add to PATH"，然后重启 start.bat 即可。

升级
  1. 推荐进入面板后台「系统设置」->「概览」->「检查系统更新」。
  2. 检测到新版本后点击「立即更新」，面板会自动下载 Windows zip，
     并在后台替换程序和 web\ 前端文件。
  3. 后台更新会保留现有 config.yaml、Dumb-Panel\、data\、logs\、
     backups\ 等本地配置和数据目录。
  4. 只有后台更新失败、网络无法访问 GitHub Release，或当前目录没有
     写入权限时，才需要手动下载 zip 后迁移 Dumb-Panel\。

命令行运维
  同目录的 ddp.exe 提供一组运维子命令，例如：
    ddp.exe status
    ddp.exe list-users
    ddp.exe reset-password admin NewPass123
    ddp.exe backup create --name nightly
  完整子命令列表请执行 ddp.exe help。

注意事项
  - 首次启动会在当前目录创建 Dumb-Panel\ 和数据库文件，
    请确保 start.bat 所在目录有读写权限。
  - Windows Defender 或其他杀毒软件可能提示二进制未经代码签名，
    这是正常现象，请选择"允许运行"。
  - Windows 单机版支持面板后台更新；如安装目录位于系统保护目录，
    请将面板移动到有写入权限的位置后再更新。
