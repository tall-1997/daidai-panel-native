// DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1
'use strict';
/**
 * Daidai Panel managed notification helper.
 *
 * Usage:
 *   const { sendNotify } = require('./sendNotify');
 *   const notifyStr = [];
 *   notifyStr.push('签到成功');
 *   notifyStr.push('账号: user01');
 *   await sendNotify('示例任务', notifyStr.join('\n'));
 *
 * QingLong compatibility:
 * - Keep sendNotify(text, desp, params) and send(text, desp, params).
 * - params.channel_id / params.channel_ids select panel channels.
 * - Extra params are merged into context for content_template variables.
 * - params.ignore_default_config = true skips DAIDAI_NOTIFY_CHANNEL_ID.
 */
const fs = require('node:fs');
const http = require('node:http');
const https = require('node:https');
const path = require('node:path');
const Module = require('node:module');
const { URL } = require('node:url');

const DEFAULT_TIMEOUT_MS = 15000;
const RESERVED_PARAM_KEYS = new Set(['channel_id', 'channel_ids', 'context', 'ignore_default_config', 'url', 'token', 'timeout']);
const SCRIPTS_DIR = String(process.env.DAIDAI_SCRIPTS_DIR || __dirname).trim() || __dirname;
const MANAGED_HELPER_PATH = path.join(SCRIPTS_DIR, 'sendNotify.js');

function isPlainObject(value) {
  return value != null && typeof value === 'object' && !Array.isArray(value);
}

function installManagedSendNotifyAlias() {
  if (global.__DAIDAI_SEND_NOTIFY_ALIAS_PATCHED__) {
    return;
  }
  const originalResolveFilename = Module._resolveFilename;
  Module._resolveFilename = function patchedResolveFilename(request, parent, isMain, options) {
    if (request === 'sendNotify' || request === 'sendNotify.js' || request === './sendNotify' || request === './sendNotify.js') {
      if (typeof request === 'string' && request.startsWith('.') && parent && parent.filename) {
        const localCandidate = path.resolve(path.dirname(parent.filename), request);
        const localJS = localCandidate.endsWith('.js') ? localCandidate : `${localCandidate}.js`;
        if (fs.existsSync(localCandidate) || fs.existsSync(localJS)) {
          return originalResolveFilename.call(this, request, parent, isMain, options);
        }
      }
      return MANAGED_HELPER_PATH;
    }
    return originalResolveFilename.call(this, request, parent, isMain, options);
  };
  global.__DAIDAI_SEND_NOTIFY_ALIAS_PATCHED__ = true;
}

installManagedSendNotifyAlias();

/**
 * Normalize timeout values from env or params into milliseconds.
 */
function resolveTimeoutMs(timeout) {
  const raw = timeout ?? process.env.DAIDAI_NOTIFY_TIMEOUT ?? DEFAULT_TIMEOUT_MS;
  const text = String(raw).trim().toLowerCase();
  if (!text) return DEFAULT_TIMEOUT_MS;
  if (text.endsWith('ms')) {
    const parsed = Number(text.slice(0, -2));
    return Number.isFinite(parsed) ? Math.max(parsed, 100) : DEFAULT_TIMEOUT_MS;
  }
  if (text.endsWith('s')) {
    const parsed = Number(text.slice(0, -1));
    return Number.isFinite(parsed) ? Math.max(parsed * 1000, 100) : DEFAULT_TIMEOUT_MS;
  }
  const parsed = Number(text);
  if (!Number.isFinite(parsed)) return DEFAULT_TIMEOUT_MS;
  return parsed > 300 ? Math.max(parsed, 100) : Math.max(parsed * 1000, 100);
}

/**
 * Read the default task-level channel from the injected environment.
 */
function resolveDefaultChannelId(params = {}) {
  if (params.ignore_default_config === true) {
    return null;
  }
  const raw = String(process.env.DAIDAI_NOTIFY_CHANNEL_ID || '').trim();
  if (!raw) {
    return null;
  }
  const parsed = Number(raw);
  return Number.isNaN(parsed) ? null : parsed;
}

/**
 * Merge params.context with non-reserved params into one context object.
 */
function buildContext(params = {}) {
  const extraContext = {};
  for (const [key, value] of Object.entries(params)) {
    if (!RESERVED_PARAM_KEYS.has(key)) {
      extraContext[key] = value;
    }
  }

  const baseContext = params.context;
  if (isPlainObject(baseContext)) {
    return { ...baseContext, ...extraContext };
  }
  if (baseContext != null && Object.keys(extraContext).length > 0) {
    return { value: baseContext, ...extraContext };
  }
  if (baseContext != null) {
    return baseContext;
  }
  return Object.keys(extraContext).length > 0 ? extraContext : null;
}

/**
 * Build the request body expected by /api/v1/notifications/send.
 */
function buildPayload(title, content, params = {}) {
  const payload = { title, content };
  const defaultChannelId = resolveDefaultChannelId(params);
  if (params.channel_id != null) {
    payload.channel_id = params.channel_id;
  } else if (Array.isArray(params.channel_ids) && params.channel_ids.length > 0) {
    payload.channel_ids = params.channel_ids;
  } else if (defaultChannelId != null) {
    payload.channel_id = defaultChannelId;
  }

  const context = buildContext(params);
  if (context != null && (!isPlainObject(context) || Object.keys(context).length > 0)) {
    payload.context = context;
  }
  return payload;
}

/**
 * Send a request to the panel notification API.
 *
 * @param {string} title Notification title.
 * @param {string} content Notification body text.
 * @param {object} params Optional request overrides and template variables.
 * @returns {Promise<object>} Parsed JSON response from the panel API.
 */
function requestNotify(title, content, params = {}) {
  const notifyUrl = String(params.url || process.env.DAIDAI_NOTIFY_URL || '').trim();
  const notifyToken = String(params.token || process.env.DAIDAI_NOTIFY_TOKEN || '').trim();
  const timeoutMs = resolveTimeoutMs(params.timeout);
  if (!notifyUrl || !notifyToken) {
    return Promise.reject(new Error('DAIDAI_NOTIFY_URL 或 DAIDAI_NOTIFY_TOKEN 未配置'));
  }

  const payload = JSON.stringify(buildPayload(title, content, params));
  const target = new URL(notifyUrl);
  const client = target.protocol === 'https:' ? https : http;

  return new Promise((resolve, reject) => {
    const req = client.request({
      protocol: target.protocol,
      hostname: target.hostname,
      port: target.port || undefined,
      path: `${target.pathname}${target.search}`,
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${notifyToken}`,
        'Content-Type': 'application/json',
        'Origin': String(process.env.DAIDAI_NOTIFY_ORIGIN || ''),
        'X-Daidai-Local-Token': notifyToken,
        'Content-Length': Buffer.byteLength(payload),
      },
      timeout: timeoutMs,
    }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => {
        let parsed = {};
        if (body) {
          try {
            parsed = JSON.parse(body);
          } catch (err) {
            parsed = { raw: body };
          }
        }
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(parsed);
          return;
        }
        const message = parsed.error || parsed.message || body || `HTTP ${res.statusCode}`;
        reject(new Error(`通知发送失败: ${message}`));
      });
    });

    req.on('timeout', () => {
      req.destroy(new Error('通知发送超时'));
    });
    req.on('error', reject);
    req.write(payload);
    req.end();
  });
}

/**
 * QingLong-style notify entry point.
 *
 * @param {string} text Notification title.
 * @param {string} desp Notification body text.
 * @param {object} params Optional request overrides and template variables.
 * @returns {Promise<object|null>}
 */
async function sendNotify(text, desp, params = {}) {
  if (!desp) {
    console.log(`${text} 推送内容为空！`);
    return null;
  }
  const result = await requestNotify(text, desp, params);
  console.log(result.message || '通知发送完成');
  return result;
}

/**
 * Alias kept for compatibility with some JS scripts that call send().
 */
async function send(text, desp, params = {}) {
  return sendNotify(text, desp, params);
}

module.exports = {
  sendNotify,
  send,
  requestNotify,
};

