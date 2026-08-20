# DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1
#!/usr/bin/env python3
"""Daidai Panel managed notification helper.

Usage:
    from notify import send

    notify_lines = []
    notify_lines.append("签到成功")
    notify_lines.append("账号: user01")
    send("示例任务", "\n".join(notify_lines))

QingLong compatibility:
- Keep send(title, content, ignore_default_config=False, **kwargs).
- channel_id / channel_ids select panel notification channels.
- Extra kwargs are merged into context for content_template variables.
- ignore_default_config=True skips DAIDAI_NOTIFY_CHANNEL_ID fallback.

Runtime environment variables:
- DAIDAI_NOTIFY_URL: panel notify API URL
- DAIDAI_NOTIFY_TOKEN: temporary bearer token
- DAIDAI_NOTIFY_TIMEOUT: timeout in ms or seconds, default 15000ms
- DAIDAI_NOTIFY_CHANNEL_ID: default notification channel ID for current task
"""
import json
import os
from typing import Iterable
import urllib.error
import urllib.request

DEFAULT_TIMEOUT_SECONDS = 15.0

def _resolve_timeout_seconds(timeout=None):
    """Normalize timeout values from ms/seconds/env to seconds."""
    raw = timeout if timeout is not None else os.getenv("DAIDAI_NOTIFY_TIMEOUT", "15000")
    text = str(raw).strip().lower()
    if not text:
        return DEFAULT_TIMEOUT_SECONDS
    if text.endswith("ms"):
        try:
            return max(float(text[:-2]) / 1000.0, 0.1)
        except ValueError:
            return DEFAULT_TIMEOUT_SECONDS
    if text.endswith("s"):
        try:
            return max(float(text[:-1]), 0.1)
        except ValueError:
            return DEFAULT_TIMEOUT_SECONDS
    try:
        value = float(text)
    except ValueError:
        return DEFAULT_TIMEOUT_SECONDS
    if value > 300:
        return max(value / 1000.0, 0.1)
    return max(value, 0.1)


def _resolve_default_channel_id(use_default_channel=True):
    """Return the configured default channel ID for the running task."""
    if not use_default_channel:
        return None
    raw = os.getenv("DAIDAI_NOTIFY_CHANNEL_ID", "").strip()
    if not raw:
        return None
    try:
        return int(raw)
    except ValueError:
        return None


def _normalize_channel_ids(channel_ids):
    """Convert iterable channel IDs into a JSON-safe list."""
    if not channel_ids:
        return None
    if isinstance(channel_ids, (str, bytes)):
        return [channel_ids]
    if isinstance(channel_ids, Iterable):
        return list(channel_ids)
    return [channel_ids]


def _merge_context(context, extra_kwargs):
    """Merge custom context with extra keyword arguments."""
    if context is None:
        return extra_kwargs or None
    if isinstance(context, dict):
        merged = dict(context)
        merged.update(extra_kwargs)
        return merged
    if extra_kwargs:
        merged = {"value": context}
        merged.update(extra_kwargs)
        return merged
    return context


def _build_payload(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True):
    """Build the request body expected by /api/v1/notifications/send."""
    payload = {"title": title, "content": content}
    default_channel_id = _resolve_default_channel_id(use_default_channel)
    if channel_id is not None:
        payload["channel_id"] = channel_id
    else:
        normalized_channel_ids = _normalize_channel_ids(channel_ids)
        if normalized_channel_ids:
            payload["channel_ids"] = normalized_channel_ids
        elif default_channel_id is not None:
            payload["channel_id"] = default_channel_id
    if context is not None and context != {}:
        payload["context"] = context
    return payload


def request_notify(title, content, channel_id=None, channel_ids=None, context=None, use_default_channel=True, url=None, token=None, timeout=None):
    """Send a notification request to the panel notify API.

    Args:
        title: Notification title.
        content: Notification body text.
        channel_id: Single target channel ID.
        channel_ids: Multiple target channel IDs.
        context: Extra template variables for content_template.
        use_default_channel: Whether DAIDAI_NOTIFY_CHANNEL_ID should be used.
        url: Override DAIDAI_NOTIFY_URL.
        token: Override DAIDAI_NOTIFY_TOKEN.
        timeout: Override DAIDAI_NOTIFY_TIMEOUT.
    """
    notify_url = (url or os.getenv("DAIDAI_NOTIFY_URL", "")).strip()
    notify_token = (token or os.getenv("DAIDAI_NOTIFY_TOKEN", "")).strip()
    if not notify_url or not notify_token:
        raise RuntimeError("DAIDAI_NOTIFY_URL 或 DAIDAI_NOTIFY_TOKEN 未配置")

    timeout_seconds = _resolve_timeout_seconds(timeout)
    payload = _build_payload(
        title,
        content,
        channel_id=channel_id,
        channel_ids=channel_ids,
        context=context,
        use_default_channel=use_default_channel,
    )
    request = urllib.request.Request(
        notify_url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {notify_token}",
            "Content-Type": "application/json",
            "Origin": os.getenv("DAIDAI_NOTIFY_ORIGIN", ""),
            "X-Daidai-Local-Token": notify_token,
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
            body = response.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as err:
        body = err.read().decode("utf-8", errors="ignore")
        raise RuntimeError(f"通知发送失败: HTTP {err.code}: {body}") from err
    except urllib.error.URLError as err:
        raise RuntimeError(f"通知发送失败: {err}") from err


def send(title, content, ignore_default_config=False, **kwargs):
    """QingLong-style wrapper around request_notify.

    Supported kwargs:
        channel_id / channel_ids: Choose target channels.
        context: Extra template variables.
        url / token / timeout: Override runtime environment values.
        any other kwargs: Automatically merged into context.
    """
    if not content:
        print(f"{title} 推送内容为空！")
        return None

    request_url = kwargs.pop("url", None)
    request_token = kwargs.pop("token", None)
    request_timeout = kwargs.pop("timeout", None)
    channel_id = kwargs.pop("channel_id", None)
    channel_ids = kwargs.pop("channel_ids", None)
    context = kwargs.pop("context", None)
    context = _merge_context(context, kwargs)

    result = request_notify(
        title,
        content,
        channel_id=channel_id,
        channel_ids=channel_ids,
        context=context,
        use_default_channel=not ignore_default_config,
        url=request_url,
        token=request_token,
        timeout=request_timeout,
    )
    print(result.get("message", "通知发送完成"))
    return result


def main():
    send("title", "content")


if __name__ == "__main__":
    main()

