/**
 * 演示站脚本管理页的文件内容。
 *
 * 这些是【手写 fixture】，与 notification-types.json / configs.json 那两个由
 * server/cmd/gen-demo-fixtures 生成的 schema 文件不是一回事 —— 那两个的真源在服务端，
 * 手写等于制造副本；这里是纯剧本数据，服务端没有对应的注册表。
 *
 * 为什么要写真实可运行的内容：脚本页的代码编辑器是纯客户端渲染，
 * 语法高亮、搜索全都会真的生效，占位符（"// TODO"）会让演示站一眼假。
 *
 * ⚠️ 内容里【不要】出现具体的薅羊毛平台名。公网 Demo 是项目门面，
 *    写具体平台既把项目定位窄化成薅羊毛工具，也有品牌风险。一律用中性运维场景。
 *
 * ⚠️ 写 shell 时统一用 $VAR 而不是 ${VAR}：这些内容是 JS 模板字符串，
 *    `${` 会被当成插值起始符，必须转义才能原样输出，容易漏。
 */

export interface DemoScriptSeed {
  path: string
  content: string
  /** 相对「现在」的天数，用来生成一个不整齐的修改时间 */
  daysAgo: number
}

/** 脚本工作区里的目录（文件的父目录不必重复列出，建树时会自动补） */
export const DEMO_SCRIPT_DIRS = [
  'monitor',
  'ops',
  'report',
  'data',
  'lib',
]

const BACKUP_DATA_SH = `#!/usr/bin/env bash
# 每日数据备份：打包数据目录并按保留天数清理历史备份
set -euo pipefail

SOURCE_DIR="\${DATA_DIR:-/opt/app/data}"
TARGET_DIR="$BACKUP_TARGET_DIR"
RETENTION_DAYS="\${BACKUP_RETENTION_DAYS:-14}"
STAMP="$(date +%Y%m%d-%H%M%S)"
ARCHIVE="$TARGET_DIR/data-$STAMP.tar.gz"

mkdir -p "$TARGET_DIR"

echo "[backup] 源目录：$SOURCE_DIR"
echo "[backup] 目标文件：$ARCHIVE"

tar -czf "$ARCHIVE" -C "$SOURCE_DIR" .

SIZE="$(du -h "$ARCHIVE" | cut -f1)"
echo "[backup] 归档完成，大小 $SIZE"

echo "[backup] 清理 $RETENTION_DAYS 天前的历史备份"
find "$TARGET_DIR" -name "data-*.tar.gz" -type f -mtime "+$RETENTION_DAYS" -print -delete

echo "[backup] 全部完成"
`

const CLEAN_TMP_SH = `#!/usr/bin/env bash
# 清理临时文件：删除超过 3 天没有访问过的缓存与临时产物
set -euo pipefail

TARGETS=(
  "/tmp/app-cache"
  "/var/tmp/render"
  "/opt/app/data/tmp"
)

TOTAL_FREED=0

for DIR in "\${TARGETS[@]}"; do
  if [ ! -d "$DIR" ]; then
    echo "[clean] 跳过（目录不存在）：$DIR"
    continue
  fi

  BEFORE="$(du -sk "$DIR" | cut -f1)"
  find "$DIR" -type f -atime +3 -delete
  find "$DIR" -type d -empty -delete
  AFTER="$(du -sk "$DIR" | cut -f1 || echo 0)"

  FREED="$((BEFORE - AFTER))"
  TOTAL_FREED="$((TOTAL_FREED + FREED))"
  echo "[clean] $DIR 释放 \${FREED}KB"
done

echo "[clean] 合计释放 \${TOTAL_FREED}KB"
`

const ARCHIVE_LOGS_SH = `#!/usr/bin/env bash
# 日志归档压缩：把昨天的日志压成一个包，原文件删掉
set -euo pipefail

LOG_DIR="\${APP_LOG_DIR:-/opt/app/logs}"
ARCHIVE_DIR="$LOG_DIR/archive"
YESTERDAY="$(date -d yesterday +%Y-%m-%d 2>/dev/null || date -v-1d +%Y-%m-%d)"

mkdir -p "$ARCHIVE_DIR"

MATCHED="$(find "$LOG_DIR" -maxdepth 1 -name "*-$YESTERDAY.log" -type f | wc -l)"
if [ "$MATCHED" -eq 0 ]; then
  echo "[archive] $YESTERDAY 没有可归档的日志，跳过"
  exit 0
fi

echo "[archive] 匹配到 $MATCHED 个日志文件"
tar -czf "$ARCHIVE_DIR/logs-$YESTERDAY.tar.gz" -C "$LOG_DIR" $(cd "$LOG_DIR" && ls *-"$YESTERDAY".log)
find "$LOG_DIR" -maxdepth 1 -name "*-$YESTERDAY.log" -type f -delete

echo "[archive] 已生成 $ARCHIVE_DIR/logs-$YESTERDAY.tar.gz"
`

const SYNC_OBJECT_STORAGE_SH = `#!/usr/bin/env bash
# 对象存储同步：把本地归档目录增量同步到对象存储
set -euo pipefail

LOCAL_DIR="$BACKUP_TARGET_DIR"
BUCKET="$S3_BUCKET"
ENDPOINT="$S3_ENDPOINT"

if [ -z "\${S3_ACCESS_KEY_ID:-}" ] || [ -z "\${S3_SECRET_ACCESS_KEY:-}" ]; then
  echo "[sync] 缺少对象存储凭据，请先在环境变量里配置" >&2
  exit 78
fi

echo "[sync] 本地目录：$LOCAL_DIR"
echo "[sync] 目标桶：$BUCKET @ $ENDPOINT"

aws --endpoint-url "$ENDPOINT" s3 sync \\
  "$LOCAL_DIR" "s3://$BUCKET/backups/" \\
  --only-show-errors \\
  --storage-class STANDARD_IA

echo "[sync] 同步完成"
`

const HEALTH_CHECK_SH = `#!/usr/bin/env bash
# 系统健康检查：磁盘 / 内存 / 关键端口，任一超阈值就以非零码退出
set -uo pipefail

THRESHOLD="\${MONITOR_ALERT_THRESHOLD:-85}"
FAILED=0

DISK_USED="$(df -P / | awk 'NR==2 {print $5}' | tr -d '%')"
echo "[health] 根分区使用率 \${DISK_USED}%（阈值 \${THRESHOLD}%）"
if [ "$DISK_USED" -ge "$THRESHOLD" ]; then
  echo "[health] 磁盘使用率超过阈值" >&2
  FAILED=1
fi

MEM_USED="$(free | awk '/Mem:/ {printf "%d", $3 / $2 * 100}')"
echo "[health] 内存使用率 \${MEM_USED}%"
if [ "$MEM_USED" -ge "$THRESHOLD" ]; then
  echo "[health] 内存使用率超过阈值" >&2
  FAILED=1
fi

for TARGET in $(echo "\${MONITOR_TARGETS:-}" | tr ',' ' '); do
  if curl -fsS --max-time 5 -o /dev/null "$TARGET"; then
    echo "[health] $TARGET 可达"
  else
    echo "[health] $TARGET 不可达" >&2
    FAILED=1
  fi
done

exit "$FAILED"
`

const CHECK_SSL_SH = `#!/usr/bin/env bash
# 检查 SSL 证书：剩余有效期少于 15 天就以失败退出，交给面板触发通知
set -uo pipefail

DOMAINS="\${SSL_WATCH_DOMAINS:-example.com,api.example.com}"
MIN_DAYS=15
EXIT_CODE=0

for DOMAIN in $(echo "$DOMAINS" | tr ',' ' '); do
  END_DATE="$(echo | openssl s_client -servername "$DOMAIN" -connect "$DOMAIN:443" 2>/dev/null \\
    | openssl x509 -noout -enddate | cut -d= -f2)"

  if [ -z "$END_DATE" ]; then
    echo "[ssl] $DOMAIN 证书读取失败" >&2
    EXIT_CODE=1
    continue
  fi

  END_TS="$(date -d "$END_DATE" +%s)"
  NOW_TS="$(date +%s)"
  LEFT_DAYS="$(( (END_TS - NOW_TS) / 86400 ))"

  echo "[ssl] $DOMAIN 剩余 $LEFT_DAYS 天（到期 $END_DATE）"
  if [ "$LEFT_DAYS" -lt "$MIN_DAYS" ]; then
    echo "[ssl] $DOMAIN 即将到期" >&2
    EXIT_CODE=1
  fi
done

exit "$EXIT_CODE"
`

const OPTIMIZE_SH = `#!/usr/bin/env bash
# 数据库优化：整理碎片并重建统计信息（默认关闭，需要人工确认窗口期再启用）
set -euo pipefail

DB_FILE="\${SQLITE_DB_PATH:-/opt/app/data/panel.db}"

if [ ! -f "$DB_FILE" ]; then
  echo "[db] 数据库文件不存在：$DB_FILE" >&2
  exit 1
fi

BEFORE="$(du -k "$DB_FILE" | cut -f1)"
echo "[db] 优化前大小 \${BEFORE}KB"

sqlite3 "$DB_FILE" "PRAGMA wal_checkpoint(TRUNCATE);"
sqlite3 "$DB_FILE" "VACUUM;"
sqlite3 "$DB_FILE" "ANALYZE;"

AFTER="$(du -k "$DB_FILE" | cut -f1)"
echo "[db] 优化后大小 \${AFTER}KB，回收 $((BEFORE - AFTER))KB"
`

const CERT_EXPIRY_PY = `# -*- coding: utf-8 -*-
"""证书到期巡检：读取证书目录，把 30 天内到期的条目汇总成一条通知。"""

import os
import ssl
import socket
import datetime

from lib.notify import send

WATCH = [item.strip() for item in os.environ.get("SSL_WATCH_DOMAINS", "").split(",") if item.strip()]
WARN_DAYS = int(os.environ.get("SSL_WARN_DAYS", "30"))
TIMEOUT = float(os.environ.get("REQUEST_TIMEOUT", "10"))


def remaining_days(host: str, port: int = 443) -> int:
    context = ssl.create_default_context()
    with socket.create_connection((host, port), timeout=TIMEOUT) as raw:
        with context.wrap_socket(raw, server_hostname=host) as tls:
            not_after = tls.getpeercert()["notAfter"]

    expire_at = datetime.datetime.strptime(not_after, "%b %d %H:%M:%S %Y %Z")
    return (expire_at - datetime.datetime.utcnow()).days


def main() -> int:
    if not WATCH:
        print("未配置 SSL_WATCH_DOMAINS，跳过巡检")
        return 0

    warnings = []
    for host in WATCH:
        try:
            left = remaining_days(host)
        except Exception as exc:  # noqa: BLE001 - 巡检脚本要把所有失败都归到告警里
            warnings.append("%s 检查失败：%s" % (host, exc))
            continue

        print("%s 剩余 %d 天" % (host, left))
        if left <= WARN_DAYS:
            warnings.append("%s 将在 %d 天后到期" % (host, left))

    if warnings:
        send("证书到期巡检", "\\n".join(warnings))
        return 1

    print("全部证书有效期充足")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`

const COLLECT_METRICS_PY = `# -*- coding: utf-8 -*-
"""监控指标采集：抓取本机资源快照，写入 JSON 便于面板之外的看板消费。"""

import os
import json
import time
import shutil
import pathlib

OUTPUT = pathlib.Path(os.environ.get("METRICS_OUTPUT", "/opt/app/data/metrics.json"))
KEEP = int(os.environ.get("METRICS_KEEP", "2880"))


def read_loadavg():
    one, five, fifteen = os.getloadavg()
    return {"1m": round(one, 2), "5m": round(five, 2), "15m": round(fifteen, 2)}


def read_disk(path="/"):
    usage = shutil.disk_usage(path)
    return {
        "total": usage.total,
        "used": usage.used,
        "percent": round(usage.used / usage.total * 100, 2),
    }


def load_history():
    if not OUTPUT.exists():
        return []
    try:
        return json.loads(OUTPUT.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        print("历史文件损坏，已重置")
        return []


def main() -> int:
    sample = {
        "ts": int(time.time()),
        "load": read_loadavg(),
        "disk": read_disk(),
    }

    history = load_history()
    history.append(sample)
    history = history[-KEEP:]

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(json.dumps(history, ensure_ascii=False), encoding="utf-8")

    print("采样完成：load=%s disk=%s%%" % (sample["load"]["1m"], sample["disk"]["percent"]))
    print("历史样本数：%d" % len(history))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`

const SYNC_CONFIG_PY = `# -*- coding: utf-8 -*-
"""同步配置文件：从配置中心拉取最新配置，只有内容变化时才落盘并重载。"""

import os
import json
import hashlib
import pathlib
import urllib.request

SOURCE = os.environ.get("CONFIG_SOURCE_URL", "https://config.example.com/app/current.json")
TARGET = pathlib.Path(os.environ.get("CONFIG_TARGET", "/opt/app/conf/app.json"))
TIMEOUT = float(os.environ.get("REQUEST_TIMEOUT", "10"))


def fetch() -> bytes:
    request = urllib.request.Request(SOURCE, headers={"Accept": "application/json"})
    with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
        return response.read()


def digest(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()[:12]


def main() -> int:
    payload = fetch()

    try:
        parsed = json.loads(payload)
    except json.JSONDecodeError as exc:
        print("配置内容不是合法 JSON：%s" % exc)
        return 2

    incoming = digest(payload)
    current = digest(TARGET.read_bytes()) if TARGET.exists() else "-"

    print("远端版本 %s / 本地版本 %s" % (incoming, current))
    if incoming == current:
        print("配置无变化，跳过写入")
        return 0

    TARGET.parent.mkdir(parents=True, exist_ok=True)
    TARGET.write_text(json.dumps(parsed, ensure_ascii=False, indent=2), encoding="utf-8")
    print("已更新 %d 个配置键" % len(parsed))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`

const DAILY_REPORT_PY = `# -*- coding: utf-8 -*-
"""发送运营日报：汇总昨天的执行数据，用面板的通知渠道推送出去。"""

import os
import json
import sqlite3
import datetime

from lib.notify import send

DB_PATH = os.environ.get("SQLITE_DB_PATH", "/opt/app/data/panel.db")


def yesterday_range():
    today = datetime.date.today()
    start = datetime.datetime.combine(today - datetime.timedelta(days=1), datetime.time.min)
    return start, start + datetime.timedelta(days=1)


def collect():
    start, end = yesterday_range()
    connection = sqlite3.connect(DB_PATH)
    try:
        cursor = connection.execute(
            "SELECT status, COUNT(*) FROM task_logs WHERE started_at >= ? AND started_at < ? GROUP BY status",
            (start.isoformat(), end.isoformat()),
        )
        counted = dict(cursor.fetchall())
    finally:
        connection.close()

    return {
        "date": start.date().isoformat(),
        "success": counted.get(0, 0),
        "failed": counted.get(1, 0),
        "aborted": counted.get(3, 0),
    }


def main() -> int:
    summary = collect()
    total = summary["success"] + summary["failed"] + summary["aborted"]
    rate = (summary["success"] / total * 100) if total else 0.0

    lines = [
        "日期：%s" % summary["date"],
        "总执行：%d" % total,
        "成功：%d" % summary["success"],
        "失败：%d" % summary["failed"],
        "终止：%d" % summary["aborted"],
        "成功率：%.1f%%" % rate,
    ]

    body = "\\n".join(lines)
    print(body)
    print(json.dumps(summary, ensure_ascii=False))

    send("运营日报", body)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`

const EXPORT_OFFLINE_PY = `# -*- coding: utf-8 -*-
"""离线报表导出：手动触发，按月导出执行明细为 CSV。"""

import os
import csv
import sys
import sqlite3
import pathlib
import datetime

DB_PATH = os.environ.get("SQLITE_DB_PATH", "/opt/app/data/panel.db")
OUTPUT_DIR = pathlib.Path(os.environ.get("REPORT_OUTPUT_DIR", "/opt/app/data/reports"))

COLUMNS = ["id", "task_name", "status", "duration", "started_at", "ended_at"]


def month_range(month: str):
    start = datetime.datetime.strptime(month, "%Y-%m")
    end = (start.replace(day=28) + datetime.timedelta(days=4)).replace(day=1)
    return start, end


def main() -> int:
    month = sys.argv[1] if len(sys.argv) > 1 else datetime.date.today().strftime("%Y-%m")
    start, end = month_range(month)

    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    target = OUTPUT_DIR / ("task-logs-%s.csv" % month)

    connection = sqlite3.connect(DB_PATH)
    connection.row_factory = sqlite3.Row
    try:
        rows = connection.execute(
            "SELECT l.id, t.name AS task_name, l.status, l.duration, l.started_at, l.ended_at "
            "FROM task_logs l LEFT JOIN tasks t ON t.id = l.task_id "
            "WHERE l.started_at >= ? AND l.started_at < ? ORDER BY l.started_at",
            (start.isoformat(), end.isoformat()),
        ).fetchall()
    finally:
        connection.close()

    with target.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(COLUMNS)
        for row in rows:
            writer.writerow([row[column] for column in COLUMNS])

    print("已导出 %d 行到 %s" % (len(rows), target))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`

const UPDATE_IPDB_MJS = `// 更新 IP 数据库：每周拉取一次归属地库，校验哈希后原子替换
import fs from 'node:fs/promises'
import path from 'node:path'
import crypto from 'node:crypto'

const SOURCE = process.env.IPDB_SOURCE_URL || 'https://cdn.example.com/ipdb/latest.mmdb'
const CHECKSUM = SOURCE + '.sha256'
const TARGET = process.env.IPDB_TARGET || '/opt/app/data/ipdb/latest.mmdb'
const TIMEOUT = Number(process.env.REQUEST_TIMEOUT || 30) * 1000

async function fetchBuffer(url) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT)
  try {
    const response = await fetch(url, { signal: controller.signal })
    if (!response.ok) {
      throw new Error('HTTP ' + response.status + ' ' + url)
    }
    return Buffer.from(await response.arrayBuffer())
  } finally {
    clearTimeout(timer)
  }
}

function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex')
}

async function main() {
  console.log('[ipdb] 拉取数据库：' + SOURCE)
  const payload = await fetchBuffer(SOURCE)

  console.log('[ipdb] 拉取校验和：' + CHECKSUM)
  const expected = (await fetchBuffer(CHECKSUM)).toString('utf8').trim().split(/\\s+/)[0]
  const actual = sha256(payload)

  if (expected !== actual) {
    console.error('[ipdb] 校验和不匹配，已放弃本次更新')
    console.error('[ipdb] 期望 ' + expected)
    console.error('[ipdb] 实际 ' + actual)
    process.exit(1)
  }

  await fs.mkdir(path.dirname(TARGET), { recursive: true })
  const staging = TARGET + '.tmp'
  await fs.writeFile(staging, payload)
  await fs.rename(staging, TARGET)

  console.log('[ipdb] 更新完成，大小 ' + (payload.length / 1024 / 1024).toFixed(1) + 'MB')
}

main().catch((error) => {
  console.error('[ipdb] 更新失败：' + error.message)
  process.exit(1)
})
`

const WARM_CACHE_MJS = `// 缓存预热：面板启动后跑一次，把高频接口的缓存打满（默认关闭）
const BASE = process.env.WARM_CACHE_BASE || 'http://127.0.0.1:8080'
const CONCURRENCY = Number(process.env.WARM_CACHE_CONCURRENCY || 4)

const PATHS = [
  '/api/health',
  '/api/catalog/categories',
  '/api/catalog/hot',
  '/api/settings/public',
]

async function warm(pathname) {
  const started = Date.now()
  try {
    const response = await fetch(BASE + pathname, { headers: { 'X-Warm-Cache': '1' } })
    const cost = Date.now() - started
    console.log('[warm] ' + pathname + ' -> ' + response.status + ' (' + cost + 'ms)')
    return response.ok
  } catch (error) {
    console.error('[warm] ' + pathname + ' 失败：' + error.message)
    return false
  }
}

async function main() {
  const queue = [...PATHS]
  let failed = 0

  const workers = Array.from({ length: CONCURRENCY }, async () => {
    while (queue.length > 0) {
      const pathname = queue.shift()
      if (!pathname) return
      const ok = await warm(pathname)
      if (!ok) failed += 1
    }
  })

  await Promise.all(workers)

  console.log('[warm] 完成，失败 ' + failed + ' 个')
  process.exit(failed > 0 ? 1 : 0)
}

main()
`

const LIB_NOTIFY_PY = `# -*- coding: utf-8 -*-
"""通知封装：脚本统一走这里发通知，避免每个脚本各写一份 webhook 调用。"""

import os
import json
import urllib.request

WEBHOOK = os.environ.get("NOTIFY_WEBHOOK_URL", "")
TIMEOUT = float(os.environ.get("REQUEST_TIMEOUT", "10"))


def send(title: str, content: str) -> bool:
    """发送一条通知。没有配置 webhook 时只打印，不算失败。"""
    if not WEBHOOK:
        print("[notify] 未配置 NOTIFY_WEBHOOK_URL，仅打印")
        print("[notify] %s\\n%s" % (title, content))
        return False

    payload = json.dumps({"title": title, "content": content}).encode("utf-8")
    request = urllib.request.Request(
        WEBHOOK,
        data=payload,
        headers={"Content-Type": "application/json"},
    )

    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
            ok = 200 <= response.status < 300
    except Exception as exc:  # noqa: BLE001 - 通知失败不应该让业务脚本崩掉
        print("[notify] 发送失败：%s" % exc)
        return False

    print("[notify] 已发送：%s" % title)
    return ok
`

const LIB_UTILS_JS = `// 脚本共用的小工具：重试、限流、人类可读的体积格式化
'use strict'

/** 带指数退避的重试。attempts 是总次数，不是额外重试次数。 */
async function retry(fn, { attempts = 3, baseDelay = 500 } = {}) {
  let lastError = null

  for (let i = 0; i < attempts; i += 1) {
    try {
      return await fn(i)
    } catch (error) {
      lastError = error
      if (i === attempts - 1) break
      const delay = baseDelay * Math.pow(2, i)
      console.warn('[retry] 第 ' + (i + 1) + ' 次失败，' + delay + 'ms 后重试：' + error.message)
      await sleep(delay)
    }
  }

  throw lastError
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** 简单的并发闸门：同时最多跑 limit 个任务，保持输入顺序返回结果。 */
async function mapLimit(items, limit, worker) {
  const results = new Array(items.length)
  let cursor = 0

  const runners = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (cursor < items.length) {
      const index = cursor
      cursor += 1
      results[index] = await worker(items[index], index)
    }
  })

  await Promise.all(runners)
  return results
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return (bytes / Math.pow(1024, exponent)).toFixed(1) + ' ' + units[exponent]
}

module.exports = { retry, sleep, mapLimit, formatBytes }
`

const README_MD = `# 脚本工作区

这里是演示环境的脚本目录，按用途分成几个子目录：

| 目录 | 放什么 |
| --- | --- |
| \`monitor/\` | 巡检类脚本，失败时以非零码退出，交给面板触发通知 |
| \`ops/\` | 日常运维：备份、清理、归档、同步 |
| \`report/\` | 报表与汇总 |
| \`data/\` | 数据文件的拉取与更新 |
| \`lib/\` | 各脚本共用的工具，不单独作为任务运行 |

## 约定

1. **退出码即结论**：成功 \`0\`，业务失败非 \`0\`。面板按退出码判定任务成败，
   不要用 \`echo\` 里的关键字表达失败。
2. **配置全部走环境变量**，不要把密钥写进脚本。可用的变量见「环境变量」页。
3. **日志写 stdout / stderr**，面板会实时收流，不需要自己写日志文件。
4. **可重入**：任何脚本都可能被手动重跑一次，写文件时先写临时文件再原子改名。

## 本地调试

\`\`\`bash
export BACKUP_TARGET_DIR=/tmp/demo-backup
bash ops/backup_data.sh
\`\`\`

> 这是在线演示环境，脚本不会真的被执行。
`

const CONFIG_JSON = `{
  "app": {
    "name": "ops-scripts",
    "timezone": "Asia/Shanghai",
    "log_level": "info"
  },
  "retry": {
    "attempts": 3,
    "base_delay_ms": 500
  },
  "notify": {
    "enabled": true,
    "on_success": false,
    "on_failure": true
  },
  "monitor": {
    "targets": [
      "https://example.com/healthz",
      "https://api.example.com/healthz"
    ],
    "alert_threshold": 85
  }
}
`

export const DEMO_SCRIPT_FILES: DemoScriptSeed[] = [
  { path: 'monitor/health_check.sh', content: HEALTH_CHECK_SH, daysAgo: 3 },
  { path: 'monitor/check_ssl.sh', content: CHECK_SSL_SH, daysAgo: 11 },
  { path: 'monitor/cert_expiry.py', content: CERT_EXPIRY_PY, daysAgo: 2 },
  { path: 'monitor/collect_metrics.py', content: COLLECT_METRICS_PY, daysAgo: 6 },
  { path: 'ops/backup_data.sh', content: BACKUP_DATA_SH, daysAgo: 18 },
  { path: 'ops/clean_tmp.sh', content: CLEAN_TMP_SH, daysAgo: 24 },
  { path: 'ops/archive_logs.sh', content: ARCHIVE_LOGS_SH, daysAgo: 9 },
  { path: 'ops/sync_object_storage.sh', content: SYNC_OBJECT_STORAGE_SH, daysAgo: 4 },
  { path: 'ops/sync_config.py', content: SYNC_CONFIG_PY, daysAgo: 1 },
  { path: 'ops/warm_cache.mjs', content: WARM_CACHE_MJS, daysAgo: 33 },
  { path: 'ops/db_optimize.sh', content: OPTIMIZE_SH, daysAgo: 41 },
  { path: 'report/daily_report.py', content: DAILY_REPORT_PY, daysAgo: 7 },
  { path: 'report/export_offline.py', content: EXPORT_OFFLINE_PY, daysAgo: 15 },
  { path: 'data/update_ipdb.mjs', content: UPDATE_IPDB_MJS, daysAgo: 5 },
  { path: 'lib/notify.py', content: LIB_NOTIFY_PY, daysAgo: 21 },
  { path: 'lib/utils.js', content: LIB_UTILS_JS, daysAgo: 28 },
  { path: 'README.md', content: README_MD, daysAgo: 12 },
  { path: 'config.json', content: CONFIG_JSON, daysAgo: 8 },
]

/** 「配置文件」页（GET/PUT /system/config-script）的内容 */
export const DEMO_CONFIG_SCRIPT = `# 呆呆面板 · 自定义配置文件
#
# 这里的内容会在每个任务执行前被 source 一次，适合放全局开关与公共函数。
# 敏感信息请放「环境变量」页，不要写在这里 —— 本文件会被完整包含进备份。

# ---- 全局开关 ----------------------------------------------------------
export TZ="Asia/Shanghai"
export LANG="zh_CN.UTF-8"
export PYTHONUNBUFFERED=1
export NODE_OPTIONS="--max-old-space-size=512"

# 任务默认超时（秒），单个任务可在任务表单里单独覆盖
export DEFAULT_TASK_TIMEOUT=1800

# ---- 公共函数 ----------------------------------------------------------

# 打一条带时间戳的日志
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

# 要求某个环境变量必须存在，缺失时以 78（EX_CONFIG）退出
require_env() {
  local name="$1"
  if [ -z "$(printenv "$name")" ]; then
    echo "缺少必需的环境变量：$name" >&2
    exit 78
  fi
}

# ---- 这是演示环境 ------------------------------------------------------
# 保存会真的生效（数据只存在你自己的浏览器里），刷新页面即恢复初始内容。
`
