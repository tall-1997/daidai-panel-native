package main

import "fmt"

// helpText 是 ddp help 的全文。单独成常量，测试可以直接核对里面写的命令与参数是否真实可用。
const helpText = `ddp - 呆呆面板容器内置命令

用法:
  ddp help
  ddp version
  ddp status
  ddp check
  ddp logs [--lines 200] [--grep 关键字] [--level debug|info|warn|error]
  ddp restart
  ddp update
  ddp service <install|uninstall|start|stop|restart|status>
  ddp python <脚本路径> [参数...]
  ddp shell
  ddp script list
  ddp script cat <相对路径>
  ddp script fetch <url> [--path 相对路径] [--force]
  ddp env list [--group 分组] [--keyword 关键字]
  ddp env get <名称或ID>
  ddp env set <名称> <值> [--group 分组] [--remarks 备注] [--disabled]
  ddp env delete <名称或ID> [--all]
  ddp clean-logs [days]
  ddp backup create [--name 名称] [--password 密码] [--only configs,tasks,envs,...]
  ddp backup list
  ddp backup restore <filename> [--password 密码]
  ddp backup delete <filename>
  ddp task list [--status running|enabled|disabled|queued] [--keyword 关键字]
  ddp task logs <任务ID或名称> [--lines N]
  ddp task run <任务ID或名称>
  ddp task stop <任务ID或名称>
  ddp sub list [--type git-repo|single-file] [--keyword 关键字]
  ddp sub logs <订阅ID或名称> [--lines N]
  ddp sub pull <订阅ID或名称>
  ddp mcp --app-key <app_key> --app-secret <app_secret> [--url http://127.0.0.1:5701]
  ddp reset-login [用户名] [--ip IP] [--all]
  ddp reset-password [<用户名>] <新密码>
  ddp reset-username [<旧用户名>] <新用户名>
  ddp list-users
  ddp disable-2fa <用户名>
  ddp disable-2fa --all
  ddp ip-whitelist list
  ddp ip-whitelist add <IP或CIDR> [--remarks 备注]
  ddp ip-whitelist delete <ID或IP/CIDR>
  ddp ip-whitelist clear
  ddp ip-whitelist set <IP或CIDR> [更多IP或CIDR...]

说明:
  1. 没有使用 dd 作为命令名，因为 Linux 已自带 dd 命令，容易冲突。
  2. task run 会在当前终端里同步执行并等待结果。
  2.1 python / shell 使用面板托管 Python 环境（venv + 已装依赖 + 面板环境变量），在当前终端前台
      交互执行，可输入手机号/验证码等；解决 docker exec 终端 python3 找不到面板依赖的问题。
      python 的脚本路径相对脚本目录（也可给绝对路径）；shell 进入后 python3 即面板解释器。
  3. sub pull 会在当前终端里实时输出拉库日志。
  4. update 会自动识别 Watchtower、Magisk 模块版、旧 Docker Socket 或二进制部署；精简镜像默认通过 Watchtower HTTP API 触发更新。
  4.1 Magisk 模块版只在线更新面板程序与前端，容器 rootfs 和已装依赖不动；模块外壳有变更的版本会提示重新刷入模块 zip。
  5. service install 目前会在 Linux 上安装 systemd 守护，并让二进制更新时自动停启该服务。
  6. script / env / list / logs 这类命令不会依赖面板前端，容器里直接可用。
  7. mcp 以 stdio 方式运行 MCP 服务，给 Claude Desktop 等 AI 客户端拉起（stdout 只输出协议消息）。
      先在面板「系统设置 → MCP 服务」开启；凭据用「Open API」里创建的应用，也可用环境变量
      DDP_MCP_URL / DDP_MCP_APP_KEY / DDP_MCP_APP_SECRET 传入。Docker 部署时客户端里的命令写成
      docker exec -i <容器名> ddp mcp ...（-i 不能少）。完整说明见 ddp mcp --help。

示例:
  ddp status
  ddp python tg/首次登录.py
  ddp shell
  ddp script fetch https://example.com/demo.py --path tools/demo.py
  ddp env set JD_COOKIE "pt_key=xxx;pt_pin=yyy;" --group 京东
  ddp task list --status running
  ddp logs --lines 200 --grep failed --level error
  ddp service install
  ddp backup create --name nightly --only configs,tasks,envs,scripts
  ddp task run 12
  ddp sub list --type git-repo
  ddp sub pull 我的订阅
  ddp mcp --app-key <app_key> --app-secret <app_secret> --url http://127.0.0.1:5701
  docker exec -i <容器名> ddp mcp --app-key <app_key> --app-secret <app_secret>
  ddp reset-login --all
  ddp reset-password admin NewPass123
  ddp reset-username admin newadmin
  ddp list-users
  ddp disable-2fa admin
  ddp ip-whitelist clear
  ddp ip-whitelist set 203.0.113.10 203.0.113.0/24`

func printHelp() {
	fmt.Println(helpText)
}
