# 脚本内调用面板能力

面板在执行定时任务时，会往脚本进程里注入一批 `DAIDAI_*` 环境变量，其中包含**面板自己的接口地址和一枚临时凭据**。
脚本因此可以在运行过程中回头调用面板：发通知、读写环境变量、查任务和日志、触发订阅拉取……全都不需要你去后台申请 Open API 应用，也不用把账号密码写进脚本。

这篇文档写给**写定时任务脚本的人**，不需要懂面板内部实现。

> 一句话版本：`$DAIDAI_API_BASE` 是接口根地址，`$DAIDAI_TOKEN` 是配套的令牌，两个拼起来直接发 HTTP 请求就行。
> 唯一必须记住的红线：**要触发别的任务，请走 HTTP 接口，不要在脚本里跑 `ddp task run`**（原因见[第 4 节](#4-两条路径ddp-还是-http)）。

---

## 目录

- [1. 注入了哪些环境变量](#1-注入了哪些环境变量)
- [2. 最小示例：Python / Node / Shell](#2-最小示例python--node--shell)
- [3. 常用接口速查](#3-常用接口速查)
- [4. 两条路径：`ddp` 还是 HTTP](#4-两条路径ddp-还是-http)
- [5. 更新环境变量的正确姿势](#5-更新环境变量的正确姿势)
- [6. 安全说明](#6-安全说明)
- [7. `ddp` 在哪些安装方式里有](#7-ddp-在哪些安装方式里有)
- [8. 任务前置 / 后置脚本](#8-任务前置--后置脚本)
- [9. 青龙脚本兼容层](#9-青龙脚本兼容层)

---

## 1. 注入了哪些环境变量

任务开始执行时，下面这些变量会被写进脚本进程的环境。Python、Node、TypeScript、Shell、Go 任务都能拿到，**取用方式就是普通的读环境变量**（`os.environ` / `process.env` / `$VAR`）。

| 变量 | 含义 |
|------|------|
| `DAIDAI_API_BASE` | 面板 API 根地址，形如 `http://127.0.0.1:5701/api/v1`。端口跟着 `config.yaml` 的 `server.port` 走，**不要在脚本里写死** |
| `DAIDAI_TOKEN` | 调用上面这些接口用的 Bearer 令牌。权限与有效期见[第 6 节](#6-安全说明) |
| `DAIDAI_NOTIFY_URL` | 发通知接口的完整地址，等价于 `$DAIDAI_API_BASE/notifications/send`。为兼容既有脚本保留 |
| `DAIDAI_NOTIFY_TOKEN` | 与 `DAIDAI_TOKEN` **完全同值**，同样为兼容既有脚本保留。新脚本用哪个都行 |
| `DAIDAI_NOTIFY_TIMEOUT` | 内置通知 helper 的请求超时，固定 `15000`（毫秒） |
| `DAIDAI_NOTIFY_CHANNEL_ID` | 当前任务在「通知渠道」里选定的默认渠道 ID。**任务没有选渠道时这个变量不存在**，内置 helper 会退回广播 —— 广播只发到「默认推送」渠道，设为「绑定推送」的渠道收不到 |
| `DAIDAI_SCRIPTS_DIR` | 脚本根目录的绝对路径（面板里「脚本管理」看到的那个根） |
| `DAIDAI_NOTIFY_PY` | 内置 Python 通知 helper `notify.py` 的绝对路径 |
| `DAIDAI_SEND_NOTIFY_JS` | 内置 Node 通知 helper `sendNotify.js` 的绝对路径 |
| `DAIDAI_PYTHON_VERSION` | 当前任务实际使用的 Python 小版本，如 `3.12`。任务表单里选的版本若在本机不可用，这里是回退后的**真实**版本 |
| `DAIDAI_SILENT_EXIT_DETECT` | **由你按需设置**（不是面板注入的）。填 `0` / `off` / `false` 可为该任务单独关掉「半路静默结束」检测，见下方小节 |
| `NO_PROXY` / `no_proxy` | 代理直连白名单，大小写两份**同值注入**。面板保证 `localhost`、`127.0.0.1`、`::1` 三个主机名一定在里面；你没设过这个变量时它就等于 `localhost,127.0.0.1,::1`。它既不是保留名也不是「只补不覆盖」，而是**在你已有的值上合并追加** —— 详见下方第 4 点 |

四点补充：

1. **上表里的 `DAIDAI_*` 是保留名**（`NO_PROXY` / `no_proxy` 不在此列，它走合并追加，见第 4 点；`DAIDAI_SILENT_EXIT_DETECT` 本来就是你自己设的）。注入发生在面板环境变量之后，如果你在「环境变量」页面建了一条同名的 `DAIDAI_NOTIFY_URL`，任务运行时会被上面的值覆盖掉。
2. **脚本调试运行、`ddp python` / `ddp shell` 也有。** 这两条路径同样会注入这批变量（但没有 `DAIDAI_NOTIFY_CHANNEL_ID`，因为它们不挂在任何任务上），令牌也同样在运行 / 会话结束后立即作废，详见[第 6 节](#6-安全说明)。
3. **子进程会继承。** 脚本里 `subprocess` / `child_process` 起的子命令自动继承这些变量，所以在 Python 里调 `ddp`、在 Node 里调 `curl` 都不用手动传。
4. **`NO_PROXY` 的注入策略和上面两类都不一样。** 面板一共有三种注入策略，别搞混：

   | 策略 | 谁赢 | 例子 |
   |------|------|------|
   | 保留名 | **面板赢**，你的同名变量被覆盖 | `DAIDAI_*`、`TZ` |
   | 只补不覆盖 | **你赢**，你设了面板就不再注入 | `QL_*` 等青龙兼容变量（[第 9 节](#9-青龙脚本兼容层)） |
   | **合并追加** | **两边都保留** | `NO_PROXY` / `no_proxy` |

   合并的具体做法：面板把你写的值按 `,` 切开，只把还缺的 `localhost` / `127.0.0.1` / `::1` 追加到末尾，已经有的不重复加（大小写、前后空格都算「已经有」）。所以你自己写的白名单一条都不会少。

   **为什么脚本回调面板自身不会走代理**：面板一旦在「系统设置 → 代理设置」里配了代理地址，`HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` 及其小写版共 6 个变量会一并注入脚本进程 —— 这是为了让脚本里的 `pip` / `npm` / `git` 能走代理。但 Python 的 `urllib` / `requests` 默认也认这些变量，于是连发往 `$DAIDAI_API_BASE`（`http://127.0.0.1:<端口>/api/v1`）的通知请求都会被交给代理，代理去连一个它根本到不了的「本机」，回一个 `502`。把回环地址放进 `NO_PROXY` 就是为了堵住这条路，同时不影响 `pip` / `npm` / `git` 继续走代理。

   ⚠️ **白名单只认主机名，不认 CIDR。** 写成 `127.0.0.1/32`、`127.0.0.0/8` 这类网段形式 Python 一律匹配不上，等于没写 —— 要豁免就老老实实写 `127.0.0.1`。

### Node 任务：半路静默结束的检测与豁免

Node 有一个别的语言没有的失败模式：某个 `Promise` 永远不 resolve 也不 reject，`await` 它的调用永久卡住，事件循环随后排空，**node 以退出码 0 干净退出**。从面板外面看和「跑完了」一模一样，任务会被记成成功、不发失败通知。

典型触发写法是「请求在响应中途断开」——只在请求对象上挂了 `error` 监听，而 Node 在响应已经开始之后是把错误发给响应对象的：

```js
// ❌ 响应中途断流时，这个 Promise 永远不会 settle
new Promise((resolve, reject) => {
  const req = http.request(opts, (res) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => resolve(Buffer.concat(chunks)));
    // 缺了 res 上的 'aborted' / 'error'
  });
  req.on('error', reject);
  req.end();
});
```

面板默认会检测这种情况（系统设置 → 任务运行 → **检测脚本半路静默结束**）。命中时日志里出现 `[任务疑似半路结束]`，退出码被置为 `75`，任务判失败并正常发通知。

**这个检测有无法消除的误报**：「被抛弃的 Promise」和「被卡住的 Promise」在结构上完全一样，探针分不出来，只有脚本自己知道工作做没做完。所以下面这类写法会被误判——它们的共同点是留下了一个**背后没有定时器/socket 等句柄**的未完成 Promise：

```js
// 被抛弃的一方没有句柄时会误报（有 setTimeout 的版本不会，进程会老实等它）
await Promise.race([new Promise(() => {}), timeout(20)]);
report().catch(() => {});      // fire-and-forget 挂防御性 catch
```

**三条豁免通道，按精确度从高到低**：

| 方式 | 作用范围 | 怎么用 |
|------|----------|--------|
| `globalThis.daidaiDone?.()` | 单个脚本 | 主流程末尾加一行，声明「我跑完了」，之后一律判 CLEAN |
| `DAIDAI_SILENT_EXIT_DETECT=0` | 单个任务 | 在该任务的环境变量里加这条，只关它一个 |
| 系统设置里关掉 | 全部任务 | 影响面最大，非必要不用 |

推荐第一种：写 `?.()` 而不是 `()`，这样脚本在面板之外（本地、青龙）跑时不会因为没有这个函数而报错。

> 只有 `.js` / `.mjs` / `.ts` 任务有这套检测。Python 和 Shell **没有这个失败模式**——Python 里 `await` 一个永不完成的 future，asyncio 事件循环不会退出，进程会一直挂着，最终落到面板的任务超时路径。

### 脚本尾部输出为什么会丢

「脚本最后那段汇总只剩第一行」是个高频反馈。**头号元凶是脚本末尾调了 `process.exit()`**，下面按「先定位、再对症、最后确认」的顺序说。

**第一步，先定位：脚本末尾有没有 `process.exit()`？**

面板执行任务时，脚本的 stdout 是一条**管道**（面板要读它才能显示日志）。结论很简单：

- **自然退出（不调 `process.exit`）不会丢**：Node 会一直持有 stdout，等所有待写数据落完才让进程结束。
- **`process.exit()` 会丢**：它不等待，还没真正写出去的部分随进程一起消失。

这是实测数字，不是推论（WSL Linux + node 18.19.1，1.77 MB 输出，每档 40 轮）：

| 退出方式 | 丢尾巴 | 最严重的一次 |
|---|---|---|
| 自然退出 | **0 / 40** | — |
| **末尾 `process.exit(0)`** | **5 / 40（12.5%）** | 只收到 69403 字节，**丢了 96%** |
| 退出前 `await globalThis.daidaiFlush?.()` | **0 / 40** | — |

注意它是**概率性**的：同一个脚本大多数时候正常，偶尔丢一大截，所以很容易被误判成「面板偶发 bug」。丢失量也不限于尾巴——最坏那次连前面 96% 的日志一起没了。

> 关于机理：网上常见的说法是「Node 往管道写 stdout 是异步的」，但 Node 官方 “A note on process I/O”
> 那张表写的是 **Pipes：Windows 与 Linux 同步、只有 macOS 异步**，与上面这组实测对不上。
> 这里不下机理定论，只报可复现的事实：**调了 `process.exit()` 就有概率丢，先 flush 就不丢。**
> 换句话说，别照着「机理」推自己的脚本安不安全，照着下面这条规则做就行。

**第二步，对症：在退出前把 stdout 刷干净。**

```js
await globalThis.daidaiFlush?.();   // 等 stdout 真正写完
process.exit(0);
```

写 `?.()` 而不是 `()`，这样脚本在面板之外（本地、青龙）跑时不会因为没有这个函数而报错 —— 和上面的 `globalThis.daidaiDone?.()` 是同一个道理。

**最省事的办法其实是：别调 `process.exit()`。** 让主流程自然跑完，Node 会自己等写完再退出（上表 0/40）。只有在「还有定时器 / 长连接吊着事件循环、不主动退就一直挂着」时才需要 `process.exit()`，那时才配上 `daidaiFlush`。

除了 `process.exit()`，还有三种写法也会丢：

| 写法 | 为什么会丢 |
|---|---|
| **进程被信号杀掉** | `kill` / 面板超时终止 / 容器 OOM。进程直接没了，没有任何刷盘机会。日志里会出现 `[脚本进程被信号终止：...]`，看到它就往内存超限方向查 |
| **`worker_threads` 里 `console.log` 再转发回主线程** | 跨线程转发是异步的，主线程先退出，队列里还没转发完的就没了 |
| **自己包了一层 console 缓冲** | 那种「攒够 N 行 / N 毫秒再一次性输出」的封装，缓冲区里的残余没人负责最后一次 flush |

**⚠️ 顺带一条反面建议：不要用 `fs.writeSync(1, ...)` 代替 `console.log`。**

网上另一条常见建议是「更彻底的办法：直接 `fs.writeSync(1, msg)` 写文件描述符 1」。**请不要照做，它会打乱输出顺序。**

`fs.writeSync` 绕过了 `console.log` 背后的那条写队列，直接往 fd 1 插队。我们实测到的现象是：脚本末尾用 `fs.writeSync` 打的汇总，被塞进了前面某一段批量日志的**中间**。看起来像「日志错乱」，比丢尾巴更难排查 —— 因为内容一个字都没少，只是位置不对，肉眼很难第一时间意识到问题出在写法上。

**第三步，最后确认：是不是被面板截断的。**

面板确实有两道上限，但**两道超限都会在日志里留下可见标记**：

| 上限 | 默认值 | 计算范围 | 超限时日志里的标记 |
|---|---|---|---|
| `max_log_content_size`（系统设置 → 任务运行 → **日志内容上限**） | `102400000` 字节（≈ 97.66 MiB） | **单次子进程**的输出，不是整个任务累计 | `[日志已截断，超过最大大小限制]` |
| 落盘日志文件大小 | 10 MB（写死，不可配） | 单个日志文件 | `[日志文件已达到大小限制，停止写入]` |

日志里这两行**一行都没有**，就说明不是被面板截掉的，得回到脚本自身找原因（对照上面第二步那张表）。

### 发通知优先用内置 helper

通知这件事已经有现成的封装，不必自己拼 HTTP：

```python
# Python
from notify import send
send("任务标题", "正文第一行\n正文第二行")
```

```js
// Node
const { sendNotify } = require('sendNotify');
await sendNotify('任务标题', '正文第一行\n正文第二行');
```

两个 helper 由面板自动放进脚本根目录并加进 `PYTHONPATH` / `NODE_PATH`，`import` / `require` 直接能找到，签名与青龙保持兼容。它们内部读的就是 `DAIDAI_NOTIFY_URL` 和 `DAIDAI_NOTIFY_TOKEN`。

**正文格式与按渠道发送**（v3.2.9 起）：

```python
import notify

# 正文是 HTML：邮件发 text/html、WxPusher 按 HTML 渲染，不支持 HTML 的渠道自动去掉标签转成纯文本
notify.send("日报", html_report, content_type="html")

# 不同渠道发不同内容：与青龙同名的分渠道函数
notify.wxpusher_bot("日报", html_report, content_type="html")
notify.smtp("日报", text_report)

# 其它渠道类型用 send_to；按渠道名称点名用 channel_name
notify.send_to("discord", "日报", text_report)
notify.send("日报", text_report, channel_name="我的邮箱")
```

```js
const { sendNotify, sendTo } = require('sendNotify');
await sendTo('wxpusher', '日报', htmlReport, { content_type: 'html' });
await sendNotify('日报', textReport, { channel_types: ['email', 'telegram'] });
```

- `content_type` 取 `text` / `markdown` / `html`（也认 `text/html` 这类写法，大小写不限），**不传就按各渠道自己的配置发，和以前完全一样**。它和渠道配置里 WxPusher / 自定义渠道的同名配置项不是一回事。
- `markdown` 不会把企业微信应用的文本消息改成 markdown 消息（微信插件里看不到 markdown，也带不上保密消息设置）；企业微信机器人配了 @ 成员时同样保持文本。
- 青龙同名函数：`wxpusher_bot` `smtp` `pushplus_bot` `dingding_bot` `feishu_bot` `telegram_bot` `wecom_bot` `wecom_app` `bark` `gotify` `iGot` `serverJ` `pushdeer` `qmsg_bot` `pushme` `ntfy` `custom_notify`；其余渠道用 `send_to("渠道类型", ...)`。
- 渠道类型名（`send_to`、`channel_type` 用，大小写不限）：`webhook` `email` `telegram` `dingtalk` `wecom`（企业微信机器人） `wecom_app`（企业微信应用） `bark` `pushplus` `serverchan`（Server酱） `feishu` `gotify` `pushdeer` `pushme` `chanify` `igot` `qmsg` `pushover` `discord` `slack` `ntfy` `wxpusher` `custom`（自定义）。写错时接口返回 400，报错里会列出全部可选类型。
- 按类型发（分渠道函数、`send_to`、`channel_type`）**只发「默认推送」渠道**；设成「绑定推送」的渠道要用 `channel_name` 或 `channel_id` 点名。带了类型或名称时不再使用任务默认渠道 `DAIDAI_NOTIFY_CHANNEL_ID`。
- 找不到匹配的渠道时接口返回 400：`send` / `send_to` 会抛异常；青龙同名函数和青龙一样打印一行跳过、返回 `None`，不影响后面的调用。
- 脚本同目录下有订阅仓库自带的 `notify.py` 时，Python 会优先导入那一份，上面这些能力就用不上。

---

## 2. 最小示例：Python / Node / Shell

三段都是完整可跑的脚本，直接复制进「脚本管理」建个文件就能测。示例操作的变量名叫 `DEMO_TOKEN`，换成你自己的即可。

### Python（标准库 `urllib.request`，无需装依赖）

> 用的是标准库，Alpine 和 Debian 镜像都自带，复制过去就能跑。
> 习惯 `requests` 的话也可以，但它不是内置的，得先去「依赖管理 → Python」装一下。

```python
#!/usr/bin/env python3
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

API_BASE = os.environ["DAIDAI_API_BASE"]
TOKEN = os.environ["DAIDAI_TOKEN"]


def api(method, path, payload=None):
    """最小 HTTP 客户端。payload 为 None 时不带请求体。"""
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(API_BASE + path, data=data, method=method)
    req.add_header("Authorization", "Bearer " + TOKEN)
    if data is not None:
        req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            body = resp.read().decode("utf-8")
    except urllib.error.HTTPError as err:
        # 面板的错误响应统一是 {"error": "原因"}
        detail = err.read().decode("utf-8", "replace")
        raise RuntimeError("%s %s 失败: %s %s" % (method, path, err.code, detail))

    return json.loads(body) if body else None


def list_env(name):
    """按名字查环境变量记录。多账号场景下会返回多条。"""
    query = urllib.parse.urlencode({"keyword": name, "all": "1"})
    result = api("GET", "/envs?" + query) or {}
    # keyword 是模糊匹配（名称/备注/值/分组都会命中），所以这里再按名字精确过滤一次
    return [item for item in (result.get("data") or []) if item["name"] == name]


def upsert_env(name, value, remarks=None):
    """按名字写回。同名存在多条时接口会直接报错，不会静默改错一条。"""
    payload = {"name": name, "value": value}
    if remarks is not None:
        payload["remarks"] = remarks
    return api("PUT", "/envs/by-name", payload)


def notify(title, content):
    api("POST", "/notifications/send", {"title": title, "content": content})


def main():
    records = list_env("DEMO_TOKEN")
    print("DEMO_TOKEN 当前有 %d 条记录" % len(records))

    if len(records) > 1:
        # 多账号变量不能整段写回，详见第 5 节
        print("DEMO_TOKEN 是多账号变量，请改用 PUT /envs/<id> 单条更新")
        return 1

    # 假设脚本在这里刷新出了新的凭据
    upsert_env("DEMO_TOKEN", "new-token-value", remarks="脚本自动更新")
    notify("示例任务", "DEMO_TOKEN 已更新")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

### Node（fetch，Node 18+ 自带，无需装依赖）

```js
const API_BASE = process.env.DAIDAI_API_BASE;
const HEADERS = {
  Authorization: `Bearer ${process.env.DAIDAI_TOKEN}`,
  'Content-Type': 'application/json',
};

async function api(method, path, body) {
  const resp = await fetch(`${API_BASE}${path}`, {
    method,
    headers: HEADERS,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await resp.text();
  if (!resp.ok) {
    // 面板的错误响应统一是 {"error": "原因"}
    throw new Error(`${method} ${path} 失败: ${resp.status} ${text}`);
  }
  return text ? JSON.parse(text) : null;
}

async function listEnv(name) {
  const result = await api('GET', `/envs?all=1&keyword=${encodeURIComponent(name)}`);
  // keyword 是模糊匹配，这里再按名字精确过滤一次
  return (result.data || []).filter((item) => item.name === name);
}

async function upsertEnv(name, value, remarks) {
  const payload = { name, value };
  if (remarks !== undefined) payload.remarks = remarks;
  return api('PUT', '/envs/by-name', payload);
}

async function main() {
  const records = await listEnv('DEMO_TOKEN');
  console.log(`DEMO_TOKEN 当前有 ${records.length} 条记录`);

  if (records.length > 1) {
    // 多账号变量不能整段写回，详见第 5 节
    console.log('DEMO_TOKEN 是多账号变量，请改用 PUT /envs/<id> 单条更新');
    return;
  }

  await upsertEnv('DEMO_TOKEN', 'new-token-value', '脚本自动更新');
  await api('POST', '/notifications/send', {
    title: '示例任务',
    content: 'DEMO_TOKEN 已更新',
  });
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
```

### Shell（curl）

```bash
#!/usr/bin/env bash
set -euo pipefail

AUTH="Authorization: Bearer ${DAIDAI_TOKEN}"
JSON='Content-Type: application/json'

# 1. 读：查名字里含 DEMO_TOKEN 的环境变量（keyword 是模糊匹配）
echo "--- 当前记录 ---"
curl -sS -H "$AUTH" "${DAIDAI_API_BASE}/envs?all=1&keyword=DEMO_TOKEN"
echo

# 2. 写：按名字 upsert。同名多条时接口返回 400，set -e 配合下面的判断会让脚本停下来
echo "--- 写回 ---"
http_code=$(curl -sS -o /tmp/ddp_upsert.json -w '%{http_code}' \
  -X PUT -H "$AUTH" -H "$JSON" \
  -d '{"name":"DEMO_TOKEN","value":"new-token-value","remarks":"脚本自动更新"}' \
  "${DAIDAI_API_BASE}/envs/by-name")
cat /tmp/ddp_upsert.json
echo
if [ "$http_code" -ge 400 ]; then
  echo "写入失败，HTTP $http_code" >&2
  exit 1
fi

# 3. 发通知
echo "--- 通知 ---"
curl -sS -X POST -H "$AUTH" -H "$JSON" \
  -d '{"title":"示例任务","content":"DEMO_TOKEN 已更新"}' \
  "${DAIDAI_API_BASE}/notifications/send"
echo
```

---

## 3. 常用接口速查

**统一约定**

- 根地址：`$DAIDAI_API_BASE`（即 `http://127.0.0.1:<后端端口>/api/v1`）
- 鉴权：请求头 `Authorization: Bearer $DAIDAI_TOKEN`
- 请求体：JSON，需要带 `Content-Type: application/json`
- 出错：HTTP 4xx / 5xx，响应体是 `{"error": "原因"}`
- 列表类接口返回 `{"data": [...], "total": N, "page": 1, "page_size": 20}`

**环境变量**

| 接口 | 说明 |
|------|------|
| `GET /envs?keyword=<关键字>&all=1` | 查询。`keyword` 对名称 / 备注 / 值 / 分组做模糊匹配，拿到结果后请自己按 `name` 精确过滤。`all=1` 表示不分页（上限 5000 条） |
| `PUT /envs/by-name` | **按名字 upsert**，body `{"name": "...", "value": "...", "remarks": "...", "enabled": true}`。`remarks` / `enabled` 可省略，省略即不改。不存在则创建，存在一条则更新，**存在多条直接报错** |
| `PUT /envs/<id>` | 按 id 更新单条，body 可只带 `{"value": "..."}` |
| `POST /envs` | **纯新增**，同名不去重（青龙兼容行为）。脚本想「更新」请勿用这个，会越跑越多条 |
| `DELETE /envs/<id>` | 删除单条 |

变量名必须匹配 `^[A-Za-z_][A-Za-z0-9_]*$`，否则报「变量名格式无效」。

**任务**

| 接口 | 说明 |
|------|------|
| `GET /tasks` | 任务列表 |
| `PUT /tasks/<id>/run` | **触发执行**。走面板自己的调度器，尊重最大并发数与「不允许多实例」。任务已在运行时返回 400 |
| `PUT /tasks/<id>/stop` | 终止运行中的任务 |
| `PUT /tasks/<id>/enable`、`PUT /tasks/<id>/disable` | 启用 / 禁用 |
| `GET /tasks/<id>/latest-log` | 最近一次执行日志 |

**日志 / 脚本 / 订阅 / 通知**

| 接口 | 说明 |
|------|------|
| `GET /logs`、`GET /logs/<id>` | 执行日志列表与详情 |
| `GET /scripts/content?path=<相对路径>` | 读脚本文件 |
| `PUT /scripts/content` | 写脚本文件，body `{"path": "...", "content": "..."}` |
| `PUT /subscriptions/<id>/pull` | 立即拉取一次订阅 |
| `POST /notifications/send` | 发通知，body `{"title": "...", "content": "...", "channel_id": 1}`。`channel_id` / `channel_ids` 都可省略，省略即广播到全部「默认推送」渠道（`push_scope=default`）；显式指定 ID 时按 ID 精确投递，「绑定推送」渠道同样能收到。传了 `channel_id` / 非空 `channel_ids` 但里面没有大于 0 的 ID 会直接返回 400，不会退化成广播。以下字段均可选：`content_type`（`text` / `markdown` / `html`，也认 `text/html` 这类写法，不传按渠道配置发）；`channel_name` / `channel_names` 按名称点名，与 ID 同级，名称不存在返回 400；`channel_type` / `channel_types` 按渠道类型过滤（类型名见上文「正文格式与按渠道发送」，大小写不限），点名时取交集，未点名时只在「默认推送」渠道里过滤，类型未知返回 400 并列出可选类型 |

> 注：面板同时保留了不带版本号的 `/api/...` 路径（等价于 `/api/v1/...`），脚本里用 `$DAIDAI_API_BASE` 即可，不必关心。
>
> 内置 helper 的 `ignore_default_config=True`（Node 侧是 `{ ignore_default_config: true }`）**不是「发给所有人」**：
> 它跳过的只是 `DAIDAI_NOTIFY_CHANNEL_ID`，也就是任务绑定的那个渠道，跳过之后退回广播 ——
> 而广播只发到「默认推送」渠道，等于「只发默认推送渠道」。要连「绑定推送」渠道一起发，必须显式列出 `channel_ids`。

---

## 4. 两条路径：`ddp` 还是 HTTP

面板自带的 `ddp` 命令行也能做很多事，可以在脚本里用 `subprocess` 调。两者的根本差别是：

|  | `ddp` CLI | HTTP API |
|--|-----------|----------|
| 怎么工作 | 自己打开数据库直接读写 | 请求正在运行的面板进程 |
| 要不要令牌 | 不要 | 要 `$DAIDAI_TOKEN` |
| 要不要面板在跑 | 不要 | 要 |
| 输出 | 给人看的文本，不好解析 | JSON |
| **触发任务** | ⚠️ **会绕开并发闸门** | ✅ 走面板调度器 |

### ⚠️ 不要用 `ddp task run` 触发任务

`ddp task run` 会在 `ddp` 这个**独立进程**里现搭一套执行器和调度器，然后自己把任务跑起来。

而面板的两道闸门 —— 「定时任务最大并发数」和任务的「不允许多实例」—— 都是**面板进程内部的状态，跨进程不共享**。也就是说：

- `ddp task run` 跑起来的任务**不占用**面板的并发名额，等于凭空多开一条执行线。并发数设成 1 也拦不住
- 「不允许多实例」也只能靠数据库里的任务状态做一次前置检查，挡不住并发触发的竞态

所以脚本里要触发另一个任务，**请走 HTTP**（`api()` 是上面第 2 节那个小封装）：

```python
api("PUT", "/tasks/12/run")
```

`ddp task run` 保留给**人在终端里手动跑**用（`docker exec -it daidai-panel ddp task run 12`），那种场景下你自己知道正在开第二条线。

### 按场景怎么选

| 我要做的事 | 推荐 |
|------------|------|
| 发通知 | 内置 `notify.py` / `sendNotify.js` |
| 读环境变量 | 直接读进程环境变量（面板已经注入好了），不用调接口 |
| 按名字写回环境变量 | HTTP `PUT /envs/by-name`，或脚本里 `ddp env set` |
| **触发另一个任务** | **HTTP `PUT /tasks/<id>/run`** |
| 查任务 / 日志 | 两者皆可，要解析结果就用 HTTP |
| 拉订阅 | 两者皆可 |

在脚本里调 `ddp` 长这样：

```python
import subprocess
subprocess.run(["ddp", "env", "set", "DEMO_TOKEN", "new-token-value"], check=True)
```

---

## 5. 更新环境变量的正确姿势

「跑完把新 Cookie 写回去」是脚本最常见的需求，也是最容易把数据搞坏的地方。先理解面板是怎么把环境变量交到你手上的。

### 面板注入时做了什么

1. 按**变量名分组**。同名的多条记录（也就是多账号）会被合并成**一个**环境变量
2. 组内顺序：置顶的在前 → 分组内的排序位置 → 创建时间 → id
3. **只有一条**记录时：值原样注入，不做任何处理
4. **两条及以上**时：用 `&` 连接；只要任意一条的值里本身含 `&`，整体分隔符就升级为 `&&`。同时每条值会被转义 —— 先把 `\` 换成 `\\`，再给分隔符字符前加 `\`

所以你在脚本里读到的 `$JD_COOKIE`，在多账号情况下是一个**合并且转义过**的串，不等于任何一条记录的原始值。

### 陷阱一：多账号变量不能整段写回

```python
# ❌ 千万别这么写
cookie = os.environ["JD_COOKIE"]           # 可能是 "账号1\&x&&账号2" 这种合并串
upsert_env("JD_COOKIE", cookie + ";new")   # 整段塞回单条 → 多账号结构被压成一条，转义符还留在值里
```

`PUT /envs/by-name` 在检测到同名多条时会**直接返回错误**，就是为了拦住这种写法 —— 让脚本当场失败，好过把用户的多账号配置静默毁掉。

正确做法分两种情况：

```python
records = list_env("JD_COOKIE")

if len(records) <= 1:
    # 单账号：按名字 upsert 就行
    upsert_env("JD_COOKIE", new_value)
else:
    # 多账号：先找到你要改的那一条（按备注 / 按值里的账号标识匹配），再按 id 单独更新
    target = next(r for r in records if "pt_pin=myaccount" in r["value"])
    api("PUT", "/envs/%d" % target["id"], {"value": new_value})
```

多账号任务通常本来就是一个账号跑一次（命令里的 `conc` / `desi` 模式），这时你手上的是**单个账号的值**，配合它对应的记录 id 更新即可。

### 陷阱二：值写成 `["a","b"]` 会被当成多账号

面板在按账号拆分变量时（任务命令用了 `conc` / `desi` 多账号模式），会**先尝试把整串当 JSON 字符串数组解析**：只要它长得像 `["a","b"]` 并且能解析成功，就直接按数组元素拆成多个账号，`&` 分隔规则完全不参与。这是为兼容青龙保留的行为。

后果是：如果你的脚本把一段恰好是 JSON 数组字面量的内容写进某个变量，它在多账号模式下会被拆开。

```python
# ⚠️ 这条值会在 conc / desi 模式下被拆成两个"账号"
upsert_env("MY_LIST", '["a","b"]')

# ✅ 想存 JSON 又不想被拆，就别让它以 [ 开头 —— 包一层对象即可
upsert_env("MY_LIST", '{"items":["a","b"]}')
```

写入接口是**逐字节原样存**的，不会替你改写，也不会替你"修正"。要不要规避取决于你自己。

### `ddp env set` 的两个副作用

`ddp env set` 与 `PUT /envs/by-name` 语义一致（0 条创建 / 1 条更新 / 多条报错），但命令行版更新已有记录时会**把没传的字段一并重置**：

- 不传 `--group` → 分组被清空
- 不传 `--remarks` → 备注被清空
- 不传 `--disabled` → 强制设为启用

想保留原有的分组和备注，就把它们一起带上，或者改用 HTTP 的 `by-name`（省略的字段不会动）。

---

## 6. 安全说明

**这枚令牌能干什么**

`$DAIDAI_TOKEN` 是 operator 角色，能读写：

- ✅ 环境变量（增删改查、导入导出）
- ✅ 任务（增删改查、运行、停止、批量操作、导入导出）
- ✅ 脚本文件（读、写、上传、删除）
- ✅ 订阅（增删改、立即拉取）
- ✅ 执行日志（查看、删除）
- ✅ 发送通知

被挡在外面的（需要 admin）：系统配置、依赖管理、备份与恢复、通知渠道的增删改、用户管理、安全设置（登录日志 / 会话 / IP 白名单 / 审计日志）、SSH Key、Open API 应用管理。

它**不是**你的登录态：你在网页端登出或改密码都不影响它，反过来它也改不了任何账号密码、看不到用户数据。

**任务限定了「可用环境变量」时（v3.4.0 起）**

任务编辑页的「环境变量」页签设成「指定」后，这个任务运行时只注入命中所选分组或变量名的已启用变量（config.sh 和面板内置变量照常注入），注入的 `$DAIDAI_TOKEN` 也随之收紧成**只能发通知**：

- ✅ `POST /api/v1/notifications/send`（以及旧路径 `/api/notifications/send`）照常可用，内置 `notify.py` / `sendNotify.js` 不受影响
- 🚫 调其它任何接口（读写环境变量、建任务、运行任务……）都返回 `403 {"error":"该任务限定了可用环境变量，脚本令牌只能用来发通知"}`；MCP 也同样拒绝这枚令牌

> ⚠️ 这只是减少不必要的暴露，**不是沙箱**：脚本与面板以同一个系统用户运行（Docker 里是 root），仍然可以用 `ddp env list`、`ddp shell` 或直接读取面板数据文件拿到全部变量。来路不明的脚本不要运行。

**有效期与作废时机**

面板有三条路径会签发这枚令牌 —— 定时任务执行、`ddp python` / `ddp shell`、脚本编辑器的调试运行。
**三条都是「用完立刻作废，有效期只做兜底」**：

| 签发路径 | 兜底有效期 | 什么时候作废 |
|----------|-----------|--------------|
| 定时任务执行 · 设了「超时时间」 | 超时时长 + 1 小时 | 任务收尾时 |
| 定时任务执行 · 没设超时（默认） | 7 天 | 任务收尾时 |
| `ddp python` / `ddp shell` | 7 天（与上一行同一个常量） | 命令退出时 |
| 脚本编辑器的「调试运行 / 运行代码」 | 2 小时 | 运行结束时（含点「停止」） |

「任务收尾」覆盖正常结束、失败、超时被杀、脚本异常崩溃四种情况 —— 收尾逻辑挂在 `defer` 上，走哪条路都会经过。作废之后再拿它调任何接口都会得到 `401 {"error":"令牌已被撤销"}`。

上面那列有效期只在**吊销压根没机会执行**时才起作用：面板进程被 `kill -9`、宿主断电。正常运行下这枚令牌的实际寿命就是一次运行的时长。

> 这三条是当前版本签发脚本令牌的**全部**入口 —— 它们最终都汇聚到同一段注入逻辑。
> 如果你在别处看到 `DAIDAI_TOKEN`，那也是从上面某一条继承下来的（比如脚本起的子进程）。

**请遵守**

- 🚫 **不要打印到日志**。`print(os.environ["DAIDAI_TOKEN"])` 会让令牌进入执行日志，而日志是可以被导出的
- 🚫 **不要发给第三方**。它只对本机 `127.0.0.1` 上的面板有意义，发到外网服务器上没有任何用处，只有风险
- 🚫 **不要写进通知内容**、不要提交进 Git、不要缓存到文件
- ⚠️ **不要把它当长期凭据**。一次运行结束就失效了，缓存下来下次用一定会 401。每次运行都从环境变量重新读
- ✅ 需要给外部系统长期访问，请到「系统设置 → Open API」建应用，那才是为第三方对接设计的

---

## 7. `ddp` 在哪些安装方式里有

| 安装方式 | `ddp` 位置 | 脚本里能否直接调 |
|----------|-----------|------------------|
| Docker（Alpine / Debian 镜像） | `/usr/local/bin/ddp` | ✅ 直接 `ddp` |
| Android Magisk 模块 | 容器内 `/usr/local/bin/ddp` | ✅ 直接 `ddp`（必须在容器内，宿主侧那份找不到数据库） |
| Windows 单机版 zip | 与 `daidai-server.exe` 同目录的 `ddp.exe` | ✅ 需保证它在 PATH 里，或用绝对路径 |
| Linux tar.gz | 解压后与主程序同目录的 `ddp`（**v3.0.0 起随包提供**） | ⚠️ 见下方「找不到配置」 |

### `ddp` 找不到配置怎么办

`ddp` 需要读 `config.yaml` 才知道数据库在哪，查找顺序是：

1. 环境变量 `DAIDAI_CONFIG` 指定的路径
2. `/app/config.yaml`（Docker 镜像就是靠这条）
3. `ddp` 可执行文件**同目录**下的 `config.yaml`
4. 当前工作目录下的 `config.yaml`

Docker / Magisk 走第 2 条，天然可用。**二进制部署（Linux tar.gz、Windows）要注意**：如果你把 `ddp` 单独复制到了 `/usr/local/bin`，而 `config.yaml` 留在别处，上面四条就全落空了 —— 任务执行时的工作目录是脚本所在目录，第 4 条也帮不上忙。

两种解法，任选其一：

- 把 `ddp` 留在解压出来的目录里（和 `config.yaml` 做邻居），脚本里用绝对路径调用
- 在面板的「环境变量」页面加一条 `DAIDAI_CONFIG`，值填 `config.yaml` 的绝对路径。它会被注入任务环境，`ddp` 子进程就能读到

配好之后在容器 / 终端里跑一下 `ddp status`，能打出版本和数据目录就说明找对了。

### ⚠️ 任务命令栏不能直接填 `ddp`

面板的任务命令栏只支持两种写法：**脚本文件**（`python xxx.py`、`node xxx.js`……）和**托管依赖命令**。后者只会在面板自己的 Python venv 目录和 Node `node_modules/.bin` 目录里找可执行文件，**完全不查系统 PATH**。

所以任务命令直接写 `ddp task list` 会报「找不到托管依赖命令 ddp」。

要用 `ddp`，请在脚本内部以子进程方式调用：

```python
import subprocess
result = subprocess.run(["ddp", "env", "list"], capture_output=True, text=True)
print(result.stdout)
```

```js
const { execFileSync } = require('child_process');
console.log(execFileSync('ddp', ['env', 'list'], { encoding: 'utf8' }));
```

```bash
ddp env list
```

---

## 8. 任务前置 / 后置脚本

任务表单的「前后置脚本」标签页里有两段 shell 脚本：**前置脚本**在目标脚本之前跑，**后置脚本**在目标脚本结束之后跑。
除此之外，脚本根目录下的 `task_before.sh` / `task_after.sh` / `extra.sh` 是**全局**钩子，对每个任务都生效。

完整执行顺序：

```text
任务前置脚本  →  task_before.sh  →  目标脚本  →  任务后置脚本  →  task_after.sh  →  extra.sh
```

任务命令里传的参数（`task demo.py foo bar` 里的 `foo bar`）会原样传给这些钩子，用 `$1`、`$2` 读取。

### 前置脚本里 `export` 的变量，对目标脚本生效

这是青龙 `task_before` 的语义。**任务前置脚本**和**全局 `task_before.sh`** 都参与，按执行顺序依次合并；
合并结果同时交给目标脚本、`task_after.sh`、`extra.sh` 和任务后置脚本。

```bash
# 前置脚本
export API_BASE_URL="https://api.example.com"
export RUN_ID="$(date +%s)"
exit 0            # ← 这么写也没问题，变量照样传得过去
```

```python
# 目标脚本
import os
print(os.environ["API_BASE_URL"])   # https://api.example.com
```

四条必须知道的限制：

1. **只支持新增和覆盖，`unset` 不会传导。** 想让某个变量变成空值，写 `export VAR=`，不要写 `unset VAR`。
   （原因：面板只能看到「新增」和「值变了」，看不到「被删了」；而超大的账号变量本来就不会出现在前置脚本的环境里，
   按「缺席即删除」处理会把它们误删。）
2. **后置脚本自身的 `export` 不回传** —— 它跑完任务就结束了，没有下游消费方。
3. **`TZ` 和所有 `DAIDAI_` 开头的变量改了不生效。** 它们是面板的运行时契约：`TZ` 决定面板时区，
   `DAIDAI_TOKEN` / `DAIDAI_API_BASE` 是脚本调面板接口的凭据，`DAIDAI_NOTIFY_CHANNEL_ID` 决定任务通知发给哪个渠道。
   前置脚本改动它们会被忽略，任务日志里会写明「已忽略受保护变量: …」。
   `PWD`、`SHLVL`、`BASH_VERSION` 这类 shell 内部变量同样不会回传（静默忽略）。
4. **`PATH` 不在保护名单里**，前置脚本改 `PATH` 是生效的 —— 它决定你脚本里 `subprocess` 调 `pip` / `npm` / `git` 时用哪个。
   面板自己找 python / node / bash 用的是面板进程的 PATH，不受影响。
   `PYTHONPATH` / `NODE_PATH` / `NODE_OPTIONS` 同理，改了都生效；但它们和 `PATH` 一样是**面板注入过内容**的路径类变量
   （venv 的 site-packages、托管 `node_modules`、`sendNotify.js` 的预加载）。**请用追加写法**：
   `export PYTHONPATH=/my/lib:$PYTHONPATH`。写成整体覆盖（`export PYTHONPATH=/my/lib`）也照样生效，
   只是脚本可能突然「找不到已装的依赖」，这时任务日志里会有一行以「注意：… 已被前置脚本整体覆盖」开头的提示。

合并结果会写进任务日志（`[前置脚本环境变量] 已生效: …`），不用猜有没有生效。
另外从本版起，前置 / 后置脚本执行失败（bash 找不到、超时、脚本里 `exit 1`）也会在任务日志里出现，
但**仍然不会中断任务**——这是既有行为，没有改。

### ⚠️ 用了 `desi` / `conc` 就别在前置脚本里改同名变量

多账号收窄（`desi` / `conc`）发生在**前置脚本之后**。两者撞上同一个变量时 **`desi` / `conc` 赢**：
你在前置脚本里 `export JD_COOKIE='单个账号'`，随后 `desi` 会把这个单值当成新的账号列表重新按序号切分，
结果就是「明明 export 了一个值，脚本还是把所有账号都跑了一遍」。

**只想跑第 N 个账号，用命令自带的语法就够了**，不需要写前置脚本：

| 写法 | 含义 |
|------|------|
| `task 脚本.py desi 变量名 3` | 单进程，把该变量收窄成第 3 个值 |
| `desi 脚本.py 变量名 3` | 顶层关键字写法，与上面完全等价 |
| `task 脚本.py conc 变量名 3` | 每个选中的账号各起一个进程；只选一个时与 `desi` 等价 |

序号语法（`desi` / `conc` 通用）：

- **从 1 开始**，不是 0
- **留空默认 `1-max`**，即全部账号：`task 脚本.py desi 变量名`
- 多个值用**空格或逗号**分隔：`2 3`、`1,3`
- 区间分隔符 `-`、`~`、`_` 都认，且**支持倒序**：`2-5`、`5~2`、`1_3`
- 区间端点写 `max` 或直接留空表示总数：`3-max`、`3-`

两个容易踩的点：

- **「第 N 个」按合并后的顺序算，不是数据库 id。** 同名的多条环境变量按
  `置顶 → 分组内位置 → 创建时间 → id` 排序后用 `&` 合并（详见[第 5 节](#5-更新环境变量的正确姿势)），
  第 N 个指的是合并串里的第 N 段。
- **`conc` 会抑制实时日志。** 它同时起多个进程，输出交错没法看，所以只在任务结束后落盘。
  `conc` 还会额外给每个进程注入 `TASK_ACCOUNT_NUMBER`（当前账号序号），脚本里可以直接读。

---

## 9. 青龙脚本兼容层

> v3.2.0 起（issue #110）。**只在容器化部署里生效**：Docker / Podman 容器，以及 Magisk 模块
> （它本身就是 chroot 进一个完整 rootfs）。
> 裸机 / systemd 二进制部署**不会**建 `/ql` —— 那是用户的真实根目录，面板不该往里写东西；
> Windows 单机版更没有对位概念。
> 需要在其它形态下强行启用，设环境变量 `DAIDAI_QINGLONG_COMPAT=1`（设 `0` 则强制关闭）。

青龙生态的脚本基本都写成这个形状：

```bash
QL_DIR=${QL_DIR:-"/ql"}
dir_shell=$QL_DIR/shell
dir_repo=${dir_repo:-"$QL_DIR/data/repo"}

touch "$dir_shell/env.sh"                                   # ← 第一步就要写 /ql
repo_dir=$(find "$dir_repo" -type d -name "owner_repo" | head -1)   # ← 第二步要在 /ql 下找仓库
```

呆呆面板里没有 `/ql`，所以这类脚本以前会在第一行就 `No such file or directory` 退出。
现在面板会在**每次启动时**把青龙布局映射到自己的真实目录上。

### 目录映射

| 青龙路径 | 指向面板的 |
|---|---|
| `/ql/data/repo`、`/ql/data/scripts`、`/ql/scripts` | 脚本根目录（`data.scripts_dir`） |
| `/ql/data/log`、`/ql/log` | 日志目录（`data.log_dir`） |
| `/ql/data/config`、`/ql/config` | 数据目录（`data.dir`，「配置文件」页的 `config.sh` 就在这里） |
| `/ql/data/deps` | 数据目录下的 `deps`（依赖管理的落点） |
| `/ql/shell/env.sh` | 一个**空占位文件**（脚本会 source 它；已存在就一个字节都不碰） |

这套映射能对上，是因为订阅的检出目录名（`save_dir` 默认取 `<owner>_<repo>`）与青龙
`/ql/data/repo/<owner>_<repo>` 的目录名逐字相同。

### 注入的环境变量

| 变量 | 值 |
|---|---|
| `QL_DIR` | `/ql`（兼容层没建成时回落数据目录） |
| `QL_BRANCH` | `master` |
| `QL_REPO_PATH` / `QL_SCRIPT_PATH` | 脚本根目录 |
| `QL_LOG_PATH` | 日志目录 |
| `QL_CONFIG_PATH` | 数据目录 |
| `dir_repo` / `dir_scripts` / `dir_raw` | 脚本根目录（**真实路径，不是 `/ql` 下那条软链**） |

**为什么小写那三个也要注入**：`/ql/data/repo` 是一条软链，而 GNU `find` 默认是 `-P`，
**命令行参数本身是软链时不会下钻** —— `find "$dir_repo" -type d -name X` 一条都找不到。
（`cd` / `ls` / `test -d` / `source` / 重定向都能穿透软链，只有 `find` 不行。）
青龙脚本写的是 `dir_repo=${dir_repo:-...}`，所以把它预先设成真实路径，脚本就会直接采用。

> 这批变量与 `DAIDAI_*` / `TZ` 那些**保留名语义相反**：它们**只在你没有定义同名变量时才注入**。
> 你在「环境变量」页面建的 `QL_DIR` 会赢过面板的默认值。

### 已知边界（别把它当成「装好就能跑」）

1. `/ql/data/repo` 指向整个脚本根目录，所以青龙脚本 `find $dir_repo` 会看到**面板里所有订阅仓库**，
   不只是它自己那个。对「按 `<owner>_<repo>` 精确定位自己」的脚本无害，
   对「遍历 repo 目录批量做事」的脚本可能会误伤。
2. 兼容层只补齐**目录与环境变量**。脚本自身的运行时依赖不在面板职责内 ——
   例如 BiliBiliToolPro 需要 dotnet 才能编译运行，那一步得你自己在容器里备好。
3. 很多青龙脚本要读仓库里 `.sh` 之外的文件（源码、配置模板）。订阅默认是 **sparse 检出**，
   只落盘命中白名单的文件。这类脚本要在编辑订阅的「高级设置」里打开 **「完整检出」**（v3.2.0 新增）。
   注意完整检出会拉取整个仓库，体积可能很大。
   它**只放宽落盘范围，不放宽建任务的范围** —— 仍然只有命中「指定子目录 + 白名单」的脚本
   会被建成定时任务，多落下来的文件只是给脚本自己读。
4. 只读根、异常挂载、权限不足时兼容层会建不出来 —— 这时只在面板日志里留一行提示，
   **不会影响面板本身启动**，其余功能一切照旧。

---

## 相关文档

- [README → 容器命令 `ddp`](../README.md#容器命令-ddp)：`ddp` 全部子命令
- [README → 端口与反向代理](../README.md#端口与反向代理)：`DAIDAI_API_BASE` 里那个端口是怎么来的
