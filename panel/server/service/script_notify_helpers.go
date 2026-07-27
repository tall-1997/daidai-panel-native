package service

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daidai-panel/middleware"
)

const (
	managedNotifyHelperToken = "DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1"
	notifyPyFilename         = "notify.py"
	sendNotifyJSFilename     = "sendNotify.js"
)

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
	"QingLong compatibility:",
	"- Keep send(title, content, ignore_default_config=False, **kwargs).",
	"- channel_id / channel_ids select panel notification channels.",
	"- Extra kwargs are merged into context for content_template variables.",
	"- ignore_default_config=True skips DAIDAI_NOTIFY_CHANNEL_ID fallback.",
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
	"    \"\"\"Convert iterable channel IDs into a JSON-safe list.\"\"\"",
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
	"def _build_payload(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True):",
	"    \"\"\"Build the request body expected by /api/v1/notifications/send.\"\"\"",
	"    payload = {\"title\": title, \"content\": content}",
	"    default_channel_id = _resolve_default_channel_id(use_default_channel)",
	"    if channel_id is not None:",
	"        payload[\"channel_id\"] = channel_id",
	"    else:",
	"        normalized_channel_ids = _normalize_channel_ids(channel_ids)",
	"        if normalized_channel_ids:",
	"            payload[\"channel_ids\"] = normalized_channel_ids",
	"        elif default_channel_id is not None:",
	"            payload[\"channel_id\"] = default_channel_id",
	"    if context is not None and context != {}:",
	"        payload[\"context\"] = context",
	"    return payload",
	"",
	"",
	"def request_notify(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True, url=None, token=None, timeout=None):",
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
	"    )",
	"    request = urllib.request.Request(",
	"        notify_url,",
	"        data=json.dumps(payload).encode(\"utf-8\"),",
	"        headers={",
	"            \"Authorization\": f\"Bearer {notify_token}\",",
	"            \"Content-Type\": \"application/json\",",
	"        },",
	"        method=\"POST\",",
	"    )",
	"",
	"    try:",
	"        with urllib.request.urlopen(request, timeout=timeout_seconds) as response:",
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
	"        channel_id / channel_ids: Choose target channels.",
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
	"    )",
	"    print(result.get(\"message\", \"通知发送完成\"))",
	"    return result",
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
	" * - Extra params are merged into context for content_template variables.",
	" * - params.ignore_default_config = true skips DAIDAI_NOTIFY_CHANNEL_ID.",
	" */",
	"const fs = require('node:fs');",
	"const http = require('node:http');",
	"const https = require('node:https');",
	"const path = require('node:path');",
	"const Module = require('node:module');",
	"const { URL } = require('node:url');",
	"",
	"const DEFAULT_TIMEOUT_MS = 15000;",
	"const RESERVED_PARAM_KEYS = new Set(['channel_id', 'channel_ids', 'context', 'ignore_default_config', 'url', 'token', 'timeout']);",
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
	" * Merge params.context with non-reserved params into one context object.",
	" */",
	"function buildContext(params = {}) {",
	"  const extraContext = {};",
	"  for (const [key, value] of Object.entries(params)) {",
	"    if (!RESERVED_PARAM_KEYS.has(key)) {",
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
	" * Build the request body expected by /api/v1/notifications/send.",
	" */",
	"function buildPayload(title, content, params = {}) {",
	"  const payload = { title, content };",
	"  const defaultChannelId = resolveDefaultChannelId(params);",
	"  if (params.channel_id != null) {",
	"    payload.channel_id = params.channel_id;",
	"  } else if (Array.isArray(params.channel_ids) && params.channel_ids.length > 0) {",
	"    payload.channel_ids = params.channel_ids;",
	"  } else if (defaultChannelId != null) {",
	"    payload.channel_id = defaultChannelId;",
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
	"module.exports = {",
	"  sendNotify,",
	"  send,",
	"  requestNotify,",
	"};",
	"",
}, "\n")

var managedNotifyArtifacts = []managedNotifyArtifact{
	{filename: notifyPyFilename, content: managedNotifyPyContent + "\n"},
	{filename: sendNotifyJSFilename, content: managedSendNotifyJSContent + "\n"},
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
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Clean(path), root) {
			return nil
		}
		return cleanupManagedHelperCopies(root, path)
	})
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

	return os.WriteFile(path, []byte(content), 0o644)
}

func BuildNotifyHelperEnv(scriptsDir string, workDir string, serverPort int, defaultChannelID *uint, ttl time.Duration) (map[string]string, error) {
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
		return nil, err
	}
	if err := cleanupManagedHelperCopies(scriptsDir, workDir); err != nil {
		return nil, err
	}

	token, err := middleware.GenerateTemporaryAccessToken("internal-script-notify", "operator", ttl)
	if err != nil {
		return nil, err
	}

	env := map[string]string{
		"DAIDAI_NOTIFY_URL":     fmt.Sprintf("http://127.0.0.1:%d/api/v1/notifications/send", serverPort),
		"DAIDAI_NOTIFY_TOKEN":   token,
		"DAIDAI_NOTIFY_TIMEOUT": "15000",
		"DAIDAI_SCRIPTS_DIR":    scriptsDir,
		"DAIDAI_NOTIFY_PY":      filepath.Join(scriptsDir, notifyPyFilename),
		"DAIDAI_SEND_NOTIFY_JS": filepath.Join(scriptsDir, sendNotifyJSFilename),
	}
	if defaultChannelID != nil && *defaultChannelID > 0 {
		env["DAIDAI_NOTIFY_CHANNEL_ID"] = fmt.Sprintf("%d", *defaultChannelID)
	}
	return env, nil
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
