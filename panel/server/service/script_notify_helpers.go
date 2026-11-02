package service

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
)

var scriptNotifyBoundary struct {
	sync.RWMutex
	origin     string
	localToken string
}

func ConfigureScriptNotifyBoundary(origin, localToken string) {
	scriptNotifyBoundary.Lock()
	defer scriptNotifyBoundary.Unlock()
	scriptNotifyBoundary.origin = strings.TrimSpace(origin)
	scriptNotifyBoundary.localToken = strings.TrimSpace(localToken)
}

const (
	managedNotifyHelperToken = "DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1"
	notifyPyFilename         = "notify.py"
	sendNotifyJSFilename     = "sendNotify.js"
)

const managedNotifyPackageJSON = `{"name":"notify","version":"1.0.0","private":true,"_managed":"DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1","type":"module","main":"index.cjs","exports":{"require":"./index.cjs","import":"./index.mjs"}}`
const managedNotifyPackageCJS = "// " + managedNotifyHelperToken + "\n'use strict';\nmodule.exports = require('../../sendNotify.js');\n"
const managedNotifyPackageMJS = "// " + managedNotifyHelperToken + "\nimport helper from '../../sendNotify.js';\nexport const sendNotify = helper.sendNotify;\nexport const send = helper.send;\nexport const requestNotify = helper.requestNotify;\nexport default helper.sendNotify;\n"

type managedNotifyArtifact struct {
	filename string
	content  string
}

var managedNotifyPyContent = strings.Join([]string{
	"# " + managedNotifyHelperToken,
	"#!/usr/bin/env python3",
	"\"\"\"Daidai Panel managed notification helper.",
	"",
	"Usage:",
	"    from notify import send",
	"",
	"    notify_lines = []",
	"    notify_lines.append(\"签到成功\")",
	"    notify_lines.append(\"账号: user01\")",
	"    send(\"示例任务\", \"\\n\".join(notify_lines))",
	"",
	"    # 正文是 HTML：邮件发 text/html，不支持 HTML 的渠道自动去掉标签",
	"    send(\"日报\", \"<table>...</table>\", content_type=\"html\")",
	"",
	"    # 按渠道发不同内容：与青龙同名的分渠道函数，或 send_to 指定面板渠道类型",
	"    import notify",
	"    notify.wxpusher_bot(\"日报\", html_report, content_type=\"html\")",
	"    notify.smtp(\"日报\", text_report)",
	"    notify.send_to(\"discord\", \"日报\", text_report)",
	"",
	"QingLong compatibility:",
	"- Keep send(title, content, ignore_default_config=False, **kwargs).",
	"- channel_id / channel_ids select panel notification channels.",
	"- channel_name / channel_names select channels by name (bound channels included).",
	"- channel_type / channel_types keep only channels of those types; without IDs or names",
	"  only default-push channels are used.",
	"- content_type: text / markdown / html. Omit it to keep each channel's own format.",
	"- Extra kwargs are merged into context for content_template variables.",
	"- ignore_default_config=True skips DAIDAI_NOTIFY_CHANNEL_ID fallback.",
	"- channel_type / channel_name selectors also skip DAIDAI_NOTIFY_CHANNEL_ID.",
	"- QingLong-style per-channel functions: wxpusher_bot, smtp, pushplus_bot, dingding_bot,",
	"  feishu_bot, telegram_bot, wecom_bot, wecom_app, bark, gotify, iGot, serverJ, pushdeer,",
	"  qmsg_bot, pushme, ntfy, custom_notify. Other panel types: send_to(channel_type, ...).",
	"  Without a matching channel they print a line and return None; send / send_to raise.",
	"",
	"Runtime environment variables:",
	"- DAIDAI_NOTIFY_URL: panel notify API URL",
	"- DAIDAI_NOTIFY_TOKEN: temporary bearer token",
	"- DAIDAI_NOTIFY_TIMEOUT: timeout in ms or seconds, default 15000ms",
	"- DAIDAI_NOTIFY_CHANNEL_ID: default notification channel ID for current task",
	"\"\"\"",
	"import json",
	"import os",
	"from typing import Iterable",
	"import urllib.error",
	"import urllib.parse",
	"import urllib.request",
	"",
	"DEFAULT_TIMEOUT_SECONDS = 15.0",
	"",
	"def _resolve_timeout_seconds(timeout=None):",
	"    \"\"\"Normalize timeout values from ms/seconds/env to seconds.\"\"\"",
	"    raw = timeout if timeout is not None else os.getenv(\"DAIDAI_NOTIFY_TIMEOUT\", \"15000\")",
	"    text = str(raw).strip().lower()",
	"    if not text:",
	"        return DEFAULT_TIMEOUT_SECONDS",
	"    if text.endswith(\"ms\"):",
	"        try:",
	"            return max(float(text[:-2]) / 1000.0, 0.1)",
	"        except ValueError:",
	"            return DEFAULT_TIMEOUT_SECONDS",
	"    if text.endswith(\"s\"):",
	"        try:",
	"            return max(float(text[:-1]), 0.1)",
	"        except ValueError:",
	"            return DEFAULT_TIMEOUT_SECONDS",
	"    try:",
	"        value = float(text)",
	"    except ValueError:",
	"        return DEFAULT_TIMEOUT_SECONDS",
	"    if value > 300:",
	"        return max(value / 1000.0, 0.1)",
	"    return max(value, 0.1)",
	"",
	"",
	"def _resolve_default_channel_id(use_default_channel=True):",
	"    \"\"\"Return the configured default channel ID for the running task.\"\"\"",
	"    if not use_default_channel:",
	"        return None",
	"    raw = os.getenv(\"DAIDAI_NOTIFY_CHANNEL_ID\", \"\").strip()",
	"    if not raw:",
	"        return None",
	"    try:",
	"        return int(raw)",
	"    except ValueError:",
	"        return None",
	"",
	"",
	"def _normalize_channel_ids(channel_ids):",
	"    \"\"\"Convert iterable channel IDs / names / types into a JSON-safe list.\"\"\"",
	"    if not channel_ids:",
	"        return None",
	"    if isinstance(channel_ids, (str, bytes)):",
	"        return [channel_ids]",
	"    if isinstance(channel_ids, Iterable):",
	"        return list(channel_ids)",
	"    return [channel_ids]",
	"",
	"",
	"def _merge_context(context, extra_kwargs):",
	"    \"\"\"Merge custom context with extra keyword arguments.\"\"\"",
	"    if context is None:",
	"        return extra_kwargs or None",
	"    if isinstance(context, dict):",
	"        merged = dict(context)",
	"        merged.update(extra_kwargs)",
	"        return merged",
	"    if extra_kwargs:",
	"        merged = {\"value\": context}",
	"        merged.update(extra_kwargs)",
	"        return merged",
	"    return context",
	"",
	"",
	"def _build_payload(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True, content_type=None, channel_type=None, channel_types=None, channel_name=None, channel_names=None):",
	"    \"\"\"Build the request body expected by /api/v1/notifications/send.\"\"\"",
	"    payload = {\"title\": title, \"content\": content}",
	"    normalized_channel_types = _normalize_channel_ids(channel_types)",
	"    normalized_channel_names = _normalize_channel_ids(channel_names)",
	"    # 按类型或名称选渠道时不再回落任务默认渠道：默认渠道和类型求交集多半是空集（直接报错），",
	"    # 按名称点名时也不该把默认渠道混进来。",
	"    has_selector = bool(channel_type or channel_name or normalized_channel_types or normalized_channel_names)",
	"    default_channel_id = _resolve_default_channel_id(use_default_channel and not has_selector)",
	"    if channel_id is not None:",
	"        payload[\"channel_id\"] = channel_id",
	"    else:",
	"        normalized_channel_ids = _normalize_channel_ids(channel_ids)",
	"        if normalized_channel_ids:",
	"            payload[\"channel_ids\"] = normalized_channel_ids",
	"        elif default_channel_id is not None:",
	"            payload[\"channel_id\"] = default_channel_id",
	"    if content_type:",
	"        payload[\"content_type\"] = content_type",
	"    # 显式传了空的类型 / 名称也原样发出去，由面板返回 400；按真假判断会把它丢掉，悄悄变成广播。",
	"    if channel_type is not None:",
	"        payload[\"channel_type\"] = channel_type",
	"    if normalized_channel_types:",
	"        payload[\"channel_types\"] = normalized_channel_types",
	"    if channel_name is not None:",
	"        payload[\"channel_name\"] = channel_name",
	"    if normalized_channel_names:",
	"        payload[\"channel_names\"] = normalized_channel_names",
	"    if context is not None and context != {}:",
	"        payload[\"context\"] = context",
	"    return payload",
	"",
	"",
	"def request_notify(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True, url=None, token=None, timeout=None, content_type=None, channel_type=None, channel_types=None, channel_name=None, channel_names=None):",
	"    \"\"\"Send a notification request to the panel notify API.",
	"",
	"    Args:",
	"        title: Notification title.",
	"        content: Notification body text.",
	"        channel_id: Single target channel ID.",
	"        channel_ids: Multiple target channel IDs.",
	"        context: Extra template variables for content_template.",
	"        use_default_channel: Whether DAIDAI_NOTIFY_CHANNEL_ID should be used.",
	"        url: Override DAIDAI_NOTIFY_URL.",
	"        token: Override DAIDAI_NOTIFY_TOKEN.",
	"        timeout: Override DAIDAI_NOTIFY_TIMEOUT.",
	"        content_type: text / markdown / html. None keeps each channel's own format.",
	"        channel_type: Only send to channels of this panel type (e.g. email, wxpusher).",
	"        channel_types: Multiple panel channel types.",
	"        channel_name: Single target channel name.",
	"        channel_names: Multiple target channel names.",
	"    \"\"\"",
	"    notify_url = (url or os.getenv(\"DAIDAI_NOTIFY_URL\", \"\")).strip()",
	"    notify_token = (token or os.getenv(\"DAIDAI_NOTIFY_TOKEN\", \"\")).strip()",
	"    if not notify_url or not notify_token:",
	"        raise RuntimeError(\"DAIDAI_NOTIFY_URL 或 DAIDAI_NOTIFY_TOKEN 未配置\")",
	"",
	"    timeout_seconds = _resolve_timeout_seconds(timeout)",
	"    payload = _build_payload(",
	"        title,",
	"        content,",
	"        channel_id=channel_id,",
	"        channel_ids=channel_ids,",
	"        context=context,",
	"        use_default_channel=use_default_channel,",
	"        content_type=content_type,",
	"        channel_type=channel_type,",
	"        channel_types=channel_types,",
	"        channel_name=channel_name,",
	"        channel_names=channel_names,",
	"    )",
	"    request = urllib.request.Request(",
	"        notify_url,",
	"        data=json.dumps(payload).encode(\"utf-8\"),",
	"        headers={key: value for key, value in {",
	"            \"Authorization\": f\"Bearer {notify_token}\",",
	"            \"Content-Type\": \"application/json\",",
	"            \"Origin\": os.getenv(\"DAIDAI_NOTIFY_ORIGIN\", \"\"),",
	"            \"X-Daidai-Local-Token\": os.getenv(\"DAIDAI_NOTIFY_LOCAL_TOKEN\", \"\"),",
	"        }.items() if value},",
	"        method=\"POST\",",
	"    )",
	"",
	"    # 面板开启代理后，urllib 默认会读 http_proxy / https_proxy 等环境变量，",
	"    # 连发往面板自身 127.0.0.1:<端口> 的通知请求也一并丢给代理，代理直接回 502（issue #111）。",
	"    # 这里只对回环地址显式禁用代理：request_notify 允许用 url= 覆盖成外网地址，",
	"    # 无条件关代理会让那种用法在「只能走代理出网」的环境里彻底断网。",
	"    # 面板另外还会注入 NO_PROXY/no_proxy=localhost,127.0.0.1,::1 作为第一层豁免，",
	"    # 这里是第二层兜底（用户可能在环境变量页把 NO_PROXY 改掉）。两层都只能写纯主机名：",
	"    # Python 的 proxy_bypass_environment 只做主机名全等与 . 后缀匹配，CIDR 网段一律匹配不上。",
	"    host = (urllib.parse.urlsplit(notify_url).hostname or \"\").lower()",
	"    if host in (\"localhost\", \"::1\") or host.startswith(\"127.\"):",
	"        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))",
	"    else:",
	"        opener = urllib.request.build_opener()",
	"",
	"    try:",
	"        with opener.open(request, timeout=timeout_seconds) as response:",
	"            body = response.read().decode(\"utf-8\")",
	"            return json.loads(body) if body else {}",
	"    except urllib.error.HTTPError as err:",
	"        body = err.read().decode(\"utf-8\", errors=\"ignore\")",
	"        raise RuntimeError(f\"通知发送失败: HTTP {err.code}: {body}\") from err",
	"    except urllib.error.URLError as err:",
	"        raise RuntimeError(f\"通知发送失败: {err}\") from err",
	"",
	"",
	"def send(title, content, ignore_default_config=False, **kwargs):",
	"    \"\"\"QingLong-style wrapper around request_notify.",
	"",
	"    Supported kwargs:",
	"        channel_id / channel_ids: Choose target channels by ID.",
	"        channel_name / channel_names: Choose target channels by name.",
	"        channel_type / channel_types: Only send to channels of these panel types.",
	"        content_type: text / markdown / html. Omit it to keep each channel's own format.",
	"        context: Extra template variables.",
	"        url / token / timeout: Override runtime environment values.",
	"        any other kwargs: Automatically merged into context.",
	"    \"\"\"",
	"    if not content:",
	"        print(f\"{title} 推送内容为空！\")",
	"        return None",
	"",
	"    request_url = kwargs.pop(\"url\", None)",
	"    request_token = kwargs.pop(\"token\", None)",
	"    request_timeout = kwargs.pop(\"timeout\", None)",
	"    channel_id = kwargs.pop(\"channel_id\", None)",
	"    channel_ids = kwargs.pop(\"channel_ids\", None)",
	"    # 这几个是请求字段，不进模板变量 context。",
	"    # content_type 不是字符串时是老脚本自己的模板变量（如数字 2），照旧进 context，不发给面板被 400。",
	"    content_type = kwargs.pop(\"content_type\", None)",
	"    if content_type is not None and not isinstance(content_type, str):",
	"        kwargs[\"content_type\"] = content_type",
	"        content_type = None",
	"    channel_type = kwargs.pop(\"channel_type\", None)",
	"    channel_types = kwargs.pop(\"channel_types\", None)",
	"    channel_name = kwargs.pop(\"channel_name\", None)",
	"    channel_names = kwargs.pop(\"channel_names\", None)",
	"    context = kwargs.pop(\"context\", None)",
	"    context = _merge_context(context, kwargs)",
	"",
	"    result = request_notify(",
	"        title,",
	"        content,",
	"        channel_id=channel_id,",
	"        channel_ids=channel_ids,",
	"        context=context,",
	"        use_default_channel=not ignore_default_config,",
	"        url=request_url,",
	"        token=request_token,",
	"        timeout=request_timeout,",
	"        content_type=content_type,",
	"        channel_type=channel_type,",
	"        channel_types=channel_types,",
	"        channel_name=channel_name,",
	"        channel_names=channel_names,",
	"    )",
	"    print(result.get(\"message\", \"通知发送完成\"))",
	"    return result",
	"",
	"",
	"def send_to(channel_type, title, content, content_type=None, **kwargs):",
	"    \"\"\"Send only to enabled channels of one panel channel type.",
	"",
	"    channel_type uses panel type names (email, wxpusher, telegram, discord, ...).",
	"    Without channel_id / channel_name only default-push channels are used;",
	"    bound channels have to be named.",
	"    \"\"\"",
	"    # 类型为空时不能退化成「发给全部默认推送渠道」，直接报错。",
	"    if channel_type is None or not str(channel_type).strip():",
	"        raise ValueError(\"send_to 需要渠道类型 channel_type，例如 email、wxpusher\")",
	"    kwargs[\"channel_type\"] = channel_type",
	"    if content_type is not None:",
	"        kwargs[\"content_type\"] = content_type",
	"    return send(title, content, **kwargs)",
	"",
	"",
	"# 面板没有匹配的渠道时回 400，报错去掉「发送失败: 」后以这两句之一开头。",
	"# 渠道自己发送失败时报错是「渠道名: 错误」，下游正文里碰巧有这两句也不能当成没有渠道，所以只比开头。",
	"_NO_CHANNEL_MARKERS = (\"暂无参与广播的默认推送渠道\", \"未找到已启用的通知渠道\")",
	"",
	"",
	"def _no_channel_reason(err):",
	"    \"\"\"Return the panel's message if the request failed only because no channel matched.\"\"\"",
	"    text = str(err)",
	"    marker = \"HTTP 400:\"",
	"    if marker not in text:",
	"        return None",
	"    detail = text.split(marker, 1)[1].strip()",
	"    try:",
	"        detail = json.loads(detail).get(\"error\") or detail",
	"    except (ValueError, AttributeError):",
	"        pass",
	"    detail = str(detail)",
	"    if detail.startswith(\"发送失败: \"):",
	"        detail = detail[len(\"发送失败: \"):]",
	"    for no_channel in _NO_CHANNEL_MARKERS:",
	"        if detail.startswith(no_channel):",
	"            return detail",
	"    return None",
	"",
	"",
	"def _channel_sender(name, channel_type):",
	"    \"\"\"Build a QingLong-style per-channel function backed by send_to.",
	"",
	"    Like QingLong, it prints a line and returns None when the panel has no matching channel,",
	"    so the next call in the script still runs. send / send_to keep raising.",
	"    \"\"\"",
	"    def sender(title, content, content_type=None, **kwargs):",
	"        try:",
	"            return send_to(channel_type, title, content, content_type=content_type, **kwargs)",
	"        except RuntimeError as err:",
	"            reason = _no_channel_reason(err)",
	"            if reason is None:",
	"                raise",
	"            print(\"%s 跳过推送：%s\" % (name, reason))",
	"            return None",
	"",
	"    sender.__name__ = name",
	"    sender.__doc__ = \"QingLong-compatible alias of send_to(%r, title, content).\" % channel_type",
	"    return sender",
	"",
	"",
	"# 与青龙 notify.py 同名的分渠道函数。发到哪个渠道由面板里配好的通知渠道决定，脚本只给标题和正文。",
	"# 刻意不提供 email / telegram / discord / slack 这类裸名字：和常见包重名，from notify import * 会互相覆盖；",
	"# 面板独有的 webhook / chanify / pushover / discord / slack 用 send_to。",
	"wxpusher_bot = _channel_sender(\"wxpusher_bot\", \"wxpusher\")",
	"smtp = _channel_sender(\"smtp\", \"email\")",
	"pushplus_bot = _channel_sender(\"pushplus_bot\", \"pushplus\")",
	"dingding_bot = _channel_sender(\"dingding_bot\", \"dingtalk\")",
	"feishu_bot = _channel_sender(\"feishu_bot\", \"feishu\")",
	"telegram_bot = _channel_sender(\"telegram_bot\", \"telegram\")",
	"wecom_bot = _channel_sender(\"wecom_bot\", \"wecom\")",
	"wecom_app = _channel_sender(\"wecom_app\", \"wecom_app\")",
	"bark = _channel_sender(\"bark\", \"bark\")",
	"gotify = _channel_sender(\"gotify\", \"gotify\")",
	"iGot = _channel_sender(\"iGot\", \"igot\")",
	"serverJ = _channel_sender(\"serverJ\", \"serverchan\")",
	"pushdeer = _channel_sender(\"pushdeer\", \"pushdeer\")",
	"qmsg_bot = _channel_sender(\"qmsg_bot\", \"qmsg\")",
	"pushme = _channel_sender(\"pushme\", \"pushme\")",
	"ntfy = _channel_sender(\"ntfy\", \"ntfy\")",
	"custom_notify = _channel_sender(\"custom_notify\", \"custom\")",
	"",
	"",
	"def main():",
	"    send(\"title\", \"content\")",
	"",
	"",
	"if __name__ == \"__main__\":",
	"    main()",
	"",
}, "\n")

var managedSendNotifyJSContent = strings.Join([]string{
	"'use strict';",
	"/**",
	" * " + managedNotifyHelperToken,
	" * Daidai Panel managed notification helper.",
	" *",
	" * Usage:",
	" *   const { sendNotify } = require('./sendNotify');",
	" *   const notifyStr = [];",
	" *   notifyStr.push('签到成功');",
	" *   notifyStr.push('账号: user01');",
	" *   await sendNotify('示例任务', notifyStr.join('\\n'));",
	" *",
	" * QingLong compatibility:",
	" * - Keep sendNotify(text, desp, params) and send(text, desp, params).",
	" * - params.channel_id / params.channel_ids select panel channels.",
	" * - params.channel_name / params.channel_names select channels by name (bound channels included).",
	" * - params.channel_type / params.channel_types keep only channels of those types; without IDs or",
	" *   names only default-push channels are used.",
	" * - params.content_type: text / markdown / html. Omit it to keep each channel's own format.",
	" * - Extra params are merged into context for content_template variables.",
	" * - params.ignore_default_config = true skips DAIDAI_NOTIFY_CHANNEL_ID.",
	" * - channel_type / channel_name selectors also skip DAIDAI_NOTIFY_CHANNEL_ID.",
	" *",
	" * Per-channel sending:",
	" *   const { sendTo } = require('./sendNotify');",
	" *   await sendTo('wxpusher', '日报', htmlReport, { content_type: 'html' });",
	" *   await sendTo('email', '日报', textReport);",
	" */",
	"const fs = require('node:fs');",
	"const http = require('node:http');",
	"const https = require('node:https');",
	"const path = require('node:path');",
	"const Module = require('node:module');",
	"const { URL } = require('node:url');",
	"",
	"const DEFAULT_TIMEOUT_MS = 15000;",
	"const RESERVED_PARAM_KEYS = new Set([",
	"  'channel_id', 'channel_ids', 'context', 'ignore_default_config', 'url', 'token', 'timeout',",
	"  'content_type', 'channel_type', 'channel_types', 'channel_name', 'channel_names',",
	"]);",
	"const SCRIPTS_DIR = String(process.env.DAIDAI_SCRIPTS_DIR || __dirname).trim() || __dirname;",
	"const MANAGED_HELPER_PATH = path.join(SCRIPTS_DIR, 'sendNotify.js');",
	"",
	"function isPlainObject(value) {",
	"  return value != null && typeof value === 'object' && !Array.isArray(value);",
	"}",
	"",
	"function installManagedSendNotifyAlias() {",
	"  if (global.__DAIDAI_SEND_NOTIFY_ALIAS_PATCHED__) {",
	"    return;",
	"  }",
	"  const originalResolveFilename = Module._resolveFilename;",
	"  Module._resolveFilename = function patchedResolveFilename(request, parent, isMain, options) {",
	"    if (request === 'sendNotify' || request === 'sendNotify.js' || request === './sendNotify' || request === './sendNotify.js') {",
	"      if (typeof request === 'string' && request.startsWith('.') && parent && parent.filename) {",
	"        const localCandidate = path.resolve(path.dirname(parent.filename), request);",
	"        const localJS = localCandidate.endsWith('.js') ? localCandidate : `${localCandidate}.js`;",
	"        if (fs.existsSync(localCandidate) || fs.existsSync(localJS)) {",
	"          return originalResolveFilename.call(this, request, parent, isMain, options);",
	"        }",
	"      }",
	"      return MANAGED_HELPER_PATH;",
	"    }",
	"    return originalResolveFilename.call(this, request, parent, isMain, options);",
	"  };",
	"  global.__DAIDAI_SEND_NOTIFY_ALIAS_PATCHED__ = true;",
	"}",
	"",
	"installManagedSendNotifyAlias();",
	"",
	"/**",
	" * Normalize timeout values from env or params into milliseconds.",
	" */",
	"function resolveTimeoutMs(timeout) {",
	"  const raw = timeout ?? process.env.DAIDAI_NOTIFY_TIMEOUT ?? DEFAULT_TIMEOUT_MS;",
	"  const text = String(raw).trim().toLowerCase();",
	"  if (!text) return DEFAULT_TIMEOUT_MS;",
	"  if (text.endsWith('ms')) {",
	"    const parsed = Number(text.slice(0, -2));",
	"    return Number.isFinite(parsed) ? Math.max(parsed, 100) : DEFAULT_TIMEOUT_MS;",
	"  }",
	"  if (text.endsWith('s')) {",
	"    const parsed = Number(text.slice(0, -1));",
	"    return Number.isFinite(parsed) ? Math.max(parsed * 1000, 100) : DEFAULT_TIMEOUT_MS;",
	"  }",
	"  const parsed = Number(text);",
	"  if (!Number.isFinite(parsed)) return DEFAULT_TIMEOUT_MS;",
	"  return parsed > 300 ? Math.max(parsed, 100) : Math.max(parsed * 1000, 100);",
	"}",
	"",
	"/**",
	" * Read the default task-level channel from the injected environment.",
	" */",
	"function resolveDefaultChannelId(params = {}) {",
	"  if (params.ignore_default_config === true) {",
	"    return null;",
	"  }",
	"  const raw = String(process.env.DAIDAI_NOTIFY_CHANNEL_ID || '').trim();",
	"  if (!raw) {",
	"    return null;",
	"  }",
	"  const parsed = Number(raw);",
	"  return Number.isNaN(parsed) ? null : parsed;",
	"}",
	"",
	"/**",
	" * content_type that is not a string is an old script's own template variable (e.g. 2):",
	" * keep it in context instead of sending it to the panel, which would reject it.",
	" */",
	"function isTemplateContentType(key, value) {",
	"  return key === 'content_type' && value != null && typeof value !== 'string';",
	"}",
	"",
	"/**",
	" * Merge params.context with non-reserved params into one context object.",
	" */",
	"function buildContext(params = {}) {",
	"  const extraContext = {};",
	"  for (const [key, value] of Object.entries(params)) {",
	"    if (!RESERVED_PARAM_KEYS.has(key) || isTemplateContentType(key, value)) {",
	"      extraContext[key] = value;",
	"    }",
	"  }",
	"",
	"  const baseContext = params.context;",
	"  if (isPlainObject(baseContext)) {",
	"    return { ...baseContext, ...extraContext };",
	"  }",
	"  if (baseContext != null && Object.keys(extraContext).length > 0) {",
	"    return { value: baseContext, ...extraContext };",
	"  }",
	"  if (baseContext != null) {",
	"    return baseContext;",
	"  }",
	"  return Object.keys(extraContext).length > 0 ? extraContext : null;",
	"}",
	"",
	"/**",
	" * Normalize a channel selector (string or array) into a non-empty array, or null.",
	" */",
	"function toSelectorList(value) {",
	"  if (value == null || value === '') {",
	"    return null;",
	"  }",
	"  const list = Array.isArray(value) ? value : [value];",
	"  return list.length > 0 ? list : null;",
	"}",
	"",
	"/**",
	" * Build the request body expected by /api/v1/notifications/send.",
	" */",
	"function buildPayload(title, content, params = {}) {",
	"  const payload = { title, content };",
	"  const channelTypes = toSelectorList(params.channel_types);",
	"  const channelNames = toSelectorList(params.channel_names);",
	"  // 按类型或名称选渠道时不再回落任务默认渠道：默认渠道和类型求交集多半是空集（直接报错），",
	"  // 按名称点名时也不该把默认渠道混进来。",
	"  const hasSelector = Boolean(params.channel_type || params.channel_name || channelTypes || channelNames);",
	"  const defaultChannelId = hasSelector ? null : resolveDefaultChannelId(params);",
	"  if (params.channel_id != null) {",
	"    payload.channel_id = params.channel_id;",
	"  } else if (Array.isArray(params.channel_ids) && params.channel_ids.length > 0) {",
	"    payload.channel_ids = params.channel_ids;",
	"  } else if (defaultChannelId != null) {",
	"    payload.channel_id = defaultChannelId;",
	"  }",
	"  if (typeof params.content_type === 'string' && params.content_type) {",
	"    payload.content_type = params.content_type;",
	"  }",
	"  // 显式传了空的类型 / 名称也原样发出去，由面板返回 400；按真假判断会把它丢掉，悄悄变成广播。",
	"  if (params.channel_type != null) {",
	"    payload.channel_type = params.channel_type;",
	"  }",
	"  if (channelTypes) {",
	"    payload.channel_types = channelTypes;",
	"  }",
	"  if (params.channel_name != null) {",
	"    payload.channel_name = params.channel_name;",
	"  }",
	"  if (channelNames) {",
	"    payload.channel_names = channelNames;",
	"  }",
	"",
	"  const context = buildContext(params);",
	"  if (context != null && (!isPlainObject(context) || Object.keys(context).length > 0)) {",
	"    payload.context = context;",
	"  }",
	"  return payload;",
	"}",
	"",
	"/**",
	" * Send a request to the panel notification API.",
	" *",
	" * @param {string} title Notification title.",
	" * @param {string} content Notification body text.",
	" * @param {object} params Optional request overrides and template variables.",
	" * @returns {Promise<object>} Parsed JSON response from the panel API.",
	" */",
	"function requestNotify(title, content, params = {}) {",
	"  const notifyUrl = String(params.url || process.env.DAIDAI_NOTIFY_URL || '').trim();",
	"  const notifyToken = String(params.token || process.env.DAIDAI_NOTIFY_TOKEN || '').trim();",
	"  const timeoutMs = resolveTimeoutMs(params.timeout);",
	"  if (!notifyUrl || !notifyToken) {",
	"    return Promise.reject(new Error('DAIDAI_NOTIFY_URL 或 DAIDAI_NOTIFY_TOKEN 未配置'));",
	"  }",
	"",
	"  const payload = JSON.stringify(buildPayload(title, content, params));",
	"  const target = new URL(notifyUrl);",
	"  const client = target.protocol === 'https:' ? https : http;",
	"",
	"  return new Promise((resolve, reject) => {",
	"    const req = client.request({",
	"      protocol: target.protocol,",
	"      hostname: target.hostname,",
	"      port: target.port || undefined,",
	"      path: `${target.pathname}${target.search}`,",
	"      method: 'POST',",
	"      headers: {",
	"        'Authorization': `Bearer ${notifyToken}`,",
	"        'Content-Type': 'application/json',",
	"        'Content-Length': Buffer.byteLength(payload),",
	"        ...(process.env.DAIDAI_NOTIFY_ORIGIN ? { 'Origin': process.env.DAIDAI_NOTIFY_ORIGIN } : {}),",
	"        ...(process.env.DAIDAI_NOTIFY_LOCAL_TOKEN ? { 'X-Daidai-Local-Token': process.env.DAIDAI_NOTIFY_LOCAL_TOKEN } : {}),",
	"      },",
	"      timeout: timeoutMs,",
	"    }, (res) => {",
	"      let body = '';",
	"      res.setEncoding('utf8');",
	"      res.on('data', (chunk) => { body += chunk; });",
	"      res.on('end', () => {",
	"        let parsed = {};",
	"        if (body) {",
	"          try {",
	"            parsed = JSON.parse(body);",
	"          } catch (err) {",
	"            parsed = { raw: body };",
	"          }",
	"        }",
	"        if (res.statusCode >= 200 && res.statusCode < 300) {",
	"          resolve(parsed);",
	"          return;",
	"        }",
	"        const message = parsed.error || parsed.message || body || `HTTP ${res.statusCode}`;",
	"        reject(new Error(`通知发送失败: ${message}`));",
	"      });",
	"    });",
	"",
	"    req.on('timeout', () => {",
	"      req.destroy(new Error('通知发送超时'));",
	"    });",
	"    req.on('error', reject);",
	"    req.write(payload);",
	"    req.end();",
	"  });",
	"}",
	"",
	"/**",
	" * QingLong-style notify entry point.",
	" *",
	" * @param {string} text Notification title.",
	" * @param {string} desp Notification body text.",
	" * @param {object} params Optional request overrides and template variables.",
	" * @returns {Promise<object|null>}",
	" */",
	"async function sendNotify(text, desp, params = {}) {",
	"  if (!desp) {",
	"    console.log(`${text} 推送内容为空！`);",
	"    return null;",
	"  }",
	"  const result = await requestNotify(text, desp, params);",
	"  console.log(result.message || '通知发送完成');",
	"  return result;",
	"}",
	"",
	"/**",
	" * Alias kept for compatibility with some JS scripts that call send().",
	" */",
	"async function send(text, desp, params = {}) {",
	"  return sendNotify(text, desp, params);",
	"}",
	"",
	"/**",
	" * Send only to enabled channels of one panel channel type (email, wxpusher, telegram, ...).",
	" * Without channel_id / channel_name only default-push channels are used; bound channels have to be named.",
	" *",
	" * @param {string} channelType Panel channel type.",
	" * @param {string} text Notification title.",
	" * @param {string} desp Notification body text.",
	" * @param {object} params Optional request overrides and template variables.",
	" * @returns {Promise<object|null>}",
	" */",
	"async function sendTo(channelType, text, desp, params = {}) {",
	"  // 类型为空时不能退化成「发给全部默认推送渠道」，直接报错。",
	"  if (channelType == null || String(channelType).trim() === '') {",
	"    throw new Error('sendTo 需要渠道类型 channelType，例如 email、wxpusher');",
	"  }",
	"  return sendNotify(text, desp, { ...params, channel_type: channelType });",
	"}",
	"",
	"module.exports = {",
	"  sendNotify,",
	"  send,",
	"  sendTo,",
	"  requestNotify,",
	"};",
	"",
}, "\n")

var managedNotifyArtifacts = []managedNotifyArtifact{
	{filename: notifyPyFilename, content: managedNotifyPyContent + "\n"},
	{filename: sendNotifyJSFilename, content: managedSendNotifyJSContent + "\n"},
	{filename: "node_modules/notify/package.json", content: managedNotifyPackageJSON + "\n"},
	{filename: "node_modules/notify/index.cjs", content: managedNotifyPackageCJS},
	{filename: "node_modules/notify/index.mjs", content: managedNotifyPackageMJS},
}

func EnsureBuiltinNotifyHelpers(dirs ...string) error {
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, artifact := range managedNotifyArtifacts {
			if err := ensureManagedHelperFile(filepath.Join(dir, artifact.filename), artifact.content); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanupManagedHelperCopies(scriptsDir, workDir string) error {
	scriptsDir = strings.TrimSpace(scriptsDir)
	workDir = strings.TrimSpace(workDir)
	if scriptsDir == "" || workDir == "" {
		return nil
	}

	scriptsClean := filepath.Clean(scriptsDir)
	workClean := filepath.Clean(workDir)
	if strings.EqualFold(scriptsClean, workClean) {
		return nil
	}

	for _, artifact := range managedNotifyArtifacts {
		path := filepath.Join(workClean, artifact.filename)
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !strings.Contains(string(content), managedNotifyHelperToken) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func CleanupManagedHelperCopiesUnderRoot(scriptsDir string) error {
	scriptsDir = strings.TrimSpace(scriptsDir)
	if scriptsDir == "" {
		return nil
	}

	root := filepath.Clean(scriptsDir)
	// 唯一调用方是面板启动时的后台协程（#158），可能正赶上脚本管理、订阅拉取在增删目录：
	// 读不了 / 刚被删掉的目录跳过；某个目录清理失败也接着清其余目录，遍历完只把第一个错误交给调用方打日志
	// （以前遇到第一个错误整棵停下，排在后面的目录都清不到）。
	var firstErr error
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 读不了的目录（权限不够，或遍历途中刚被删掉：ReadDir 报错后 WalkDir 会带着 err 再回调一次）跳过，不影响其它目录。
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Clean(path), root) {
			return nil
		}
		// node_modules、__pycache__、.git 这类目录不会是任务的工作目录，面板也就不会往里放通知脚本副本，整棵跳过：
		// 订阅仓库的 .git、真实的 node_modules 动辄上千个目录，进去就是白扫一遍（#156）。
		if ShouldHideScriptTreeEntryName(d.Name()) {
			return filepath.SkipDir
		}
		if cleanupErr := cleanupManagedHelperCopies(root, path); cleanupErr != nil && firstErr == nil {
			firstErr = cleanupErr
		}
		return nil
	})
	return firstErr
}

func ensureManagedHelperFile(path, content string) error {
	existing, err := os.ReadFile(path)
	if err == nil {
		existingText := string(existing)
		if existingText == content {
			return nil
		}
		if !strings.Contains(existingText, managedNotifyHelperToken) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ScriptTokenInfo 描述注入到脚本环境里的那枚面板凭据。
// 只带 jti 与到期时间：token 本身已经在 env map 里，这里存的是「事后怎么把它作废」所需的信息。
type ScriptTokenInfo struct {
	JTI       string
	ExpiresAt time.Time
}

// RevokeScriptToken 把注入脚本环境的面板凭据拉黑。
// 任务结束（成功、失败、超时、panic）后调用，避免这枚 operator token 在任务之外继续可用。
// blockToken 内部按 jti 去重，重复调用安全；info 为空时是 no-op。
func RevokeScriptToken(info *ScriptTokenInfo) {
	if info == nil || strings.TrimSpace(info.JTI) == "" {
		return
	}
	if database.DB == nil {
		return
	}
	blockToken(info.JTI, "access", nil, info.ExpiresAt)
}

// notifyOnly 为 true 时（任务限定了可用环境变量，#168）签一枚只能发通知的令牌：
// 内置 notify.py / sendNotify.js 只调发通知接口，不受影响；脚本拿它调其它接口一律 403。
func BuildNotifyHelperEnv(scriptsDir string, workDir string, serverPort int, defaultChannelID *uint, ttl time.Duration, notifyOnly bool) (map[string]string, *ScriptTokenInfo, error) {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if absScriptsDir, err := filepath.Abs(strings.TrimSpace(scriptsDir)); err == nil {
		scriptsDir = absScriptsDir
	}
	if absWorkDir, err := filepath.Abs(strings.TrimSpace(workDir)); err == nil {
		workDir = absWorkDir
	}
	if err := EnsureBuiltinNotifyHelpers(scriptsDir); err != nil {
		return nil, nil, err
	}
	if err := cleanupManagedHelperCopies(scriptsDir, workDir); err != nil {
		return nil, nil, err
	}

	var tokenInfo *middleware.TokenInfo
	var err error
	if notifyOnly {
		tokenInfo, err = middleware.GenerateNotifyOnlyAccessTokenInfo(middleware.ScriptTokenUsername, "operator", ttl)
	} else {
		tokenInfo, err = middleware.GenerateTemporaryAccessTokenInfo(middleware.ScriptTokenUsername, "operator", ttl)
	}
	if err != nil {
		return nil, nil, err
	}

	// 全部网卡 / 回环仍走 127.0.0.1；指定了非回环监听地址（#162）就只能连那个地址
	selfHost := "127.0.0.1"
	if config.C != nil {
		selfHost = config.C.Server.SelfHost()
	}
	apiBase := fmt.Sprintf("http://%s/api/v1", net.JoinHostPort(selfHost, strconv.Itoa(serverPort)))

	// DAIDAI_NOTIFY_URL / DAIDAI_NOTIFY_TOKEN 是历史契约，内置 notify.py、sendNotify.js
	// 以及用户既有脚本都在读，只能新增别名、不能改名。
	// DAIDAI_API_BASE / DAIDAI_TOKEN 是通用入口：脚本不必再对 DAIDAI_NOTIFY_URL
	// 做字符串截断去拼别的接口，两枚 token 是同一枚凭据。
	env := map[string]string{
		"DAIDAI_NOTIFY_URL":     apiBase + "/notifications/send",
		"DAIDAI_NOTIFY_TOKEN":   tokenInfo.Token,
		"DAIDAI_NOTIFY_TIMEOUT": "15000",
		"DAIDAI_SCRIPTS_DIR":    scriptsDir,
		"DAIDAI_NOTIFY_PY":      filepath.Join(scriptsDir, notifyPyFilename),
		"DAIDAI_SEND_NOTIFY_JS": filepath.Join(scriptsDir, sendNotifyJSFilename),
		"DAIDAI_API_BASE":       apiBase,
		"DAIDAI_TOKEN":          tokenInfo.Token,
	}
	scriptNotifyBoundary.RLock()
	if scriptNotifyBoundary.origin != "" && scriptNotifyBoundary.localToken != "" {
		env["DAIDAI_NOTIFY_ORIGIN"] = scriptNotifyBoundary.origin
		env["DAIDAI_NOTIFY_LOCAL_TOKEN"] = scriptNotifyBoundary.localToken
	}
	scriptNotifyBoundary.RUnlock()
	if defaultChannelID != nil && *defaultChannelID > 0 {
		env["DAIDAI_NOTIFY_CHANNEL_ID"] = fmt.Sprintf("%d", *defaultChannelID)
	}
	return env, &ScriptTokenInfo{JTI: tokenInfo.JTI, ExpiresAt: tokenInfo.ExpiresAt}, nil
}

func AppendScriptHelperPaths(envMap map[string]string, scriptsDir string) {
	scriptsDir = strings.TrimSpace(scriptsDir)
	if scriptsDir == "" {
		return
	}

	appendEnvPathValue(envMap, "NODE_PATH", scriptsDir)
	appendEnvPathValue(envMap, "PYTHONPATH", scriptsDir)
	appendNodeRequireOption(envMap, filepath.Join(scriptsDir, sendNotifyJSFilename))
}

func appendEnvPathValue(envMap map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	existing := strings.TrimSpace(envMap[key])
	if existing == "" {
		envMap[key] = value
		return
	}

	for _, item := range strings.Split(existing, string(os.PathListSeparator)) {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return
		}
	}

	envMap[key] = existing + string(os.PathListSeparator) + value
}

func appendNodeRequireOption(envMap map[string]string, helperPath string) {
	helperPath = strings.TrimSpace(helperPath)
	if helperPath == "" {
		return
	}
	helperPath = filepath.ToSlash(helperPath)

	existing := strings.TrimSpace(envMap["NODE_OPTIONS"])
	if strings.Contains(existing, helperPath) {
		return
	}

	option := `--require="` + strings.ReplaceAll(helperPath, `"`, `\"`) + `"`
	if existing == "" {
		envMap["NODE_OPTIONS"] = option
		return
	}
	envMap["NODE_OPTIONS"] = existing + " " + option
}
