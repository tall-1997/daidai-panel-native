<script setup lang="ts">
import {
  Box,
  CircleCheckFilled,
  Files,
  RefreshRight,
  Select,
  WarningFilled,
} from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { BackupSelection } from '@/api/system'

type RestoreVisualStatus =
  | 'idle'
  | 'running'
  | 'completed'
  | 'queued-restart'
  | 'restarting'
  | 'failed'
  | 'restart-timeout'

const props = defineProps<{
  visible: boolean
  fullscreen: boolean
  filename: string
  status: string
  stage: string
  message: string
  percent: number
  source?: string
  selection?: Partial<BackupSelection>
  startedAt?: string
  countdown: number
  errorMessage: string
  onClose: () => void | Promise<void>
  onRestartNow: () => void | Promise<void>
}>()

const now = ref(Date.now())
let elapsedTimer: ReturnType<typeof setInterval> | null = null

type RestoreStep = { key: string; title: string; hint: string }

const restoreSelection = computed<BackupSelection>(() => ({
  configs: Boolean(props.selection?.configs),
  tasks: Boolean(props.selection?.tasks),
  subscriptions: Boolean(props.selection?.subscriptions),
  env_vars: Boolean(props.selection?.env_vars),
  logs: Boolean(props.selection?.logs),
  scripts: Boolean(props.selection?.scripts),
  dependencies: Boolean(props.selection?.dependencies),
  task_views: Boolean(props.selection?.task_views),
}))

const sourceLabel = computed(() => {
  switch (props.source) {
    case 'qinglong':
      return '青龙备份'
    case 'daidai-panel':
      return '呆呆面板备份'
    default:
      return props.source ? `${props.source} 备份` : '面板备份'
  }
})

const selectedSummaryItems = computed(() => {
  const items: string[] = []
  if (restoreSelection.value.configs) items.push('配置')
  if (restoreSelection.value.tasks) items.push('任务')
  if (restoreSelection.value.subscriptions) items.push('订阅')
  if (restoreSelection.value.env_vars) items.push('环境变量')
  if (restoreSelection.value.scripts) items.push('脚本')
  if (restoreSelection.value.logs) items.push('日志')
  if (restoreSelection.value.dependencies) items.push('依赖')
  return items
})

const selectedSummaryText = computed(() => {
  if (selectedSummaryItems.value.length === 0) {
    return '正在按备份内容恢复面板数据'
  }
  return `本次将恢复：${selectedSummaryItems.value.join('、')}`
})

const restoreSteps = computed<RestoreStep[]>(() => {
  const hasCoreData =
    restoreSelection.value.configs ||
    restoreSelection.value.tasks ||
    restoreSelection.value.subscriptions ||
    restoreSelection.value.env_vars

  const hasFiles =
    restoreSelection.value.scripts ||
    restoreSelection.value.logs

  const hasRuntime =
    restoreSelection.value.dependencies ||
    restoreSelection.value.configs

  const writeHintParts: string[] = []
  if (restoreSelection.value.configs) writeHintParts.push('系统配置')
  if (restoreSelection.value.tasks) writeHintParts.push('任务定义')
  if (restoreSelection.value.subscriptions) writeHintParts.push('订阅与 SSH 密钥')
  if (restoreSelection.value.env_vars) writeHintParts.push('环境变量')

  const settleHintParts: string[] = []
  if (restoreSelection.value.scripts) settleHintParts.push('脚本文件')
  if (restoreSelection.value.logs) settleHintParts.push('日志文件')
  if (restoreSelection.value.dependencies) settleHintParts.push('依赖重装任务')
  if (restoreSelection.value.configs) settleHintParts.push('镜像与调度')

  return [
    {
      key: 'prepare',
      title: '校验备份',
      hint: props.source === 'qinglong' ? '识别青龙结构并准备转换' : '读取文件并进行安全校验',
    },
    {
      key: 'unpack',
      title: '解包分析',
      hint: '识别清单内容与恢复范围',
    },
    {
      key: 'write',
      title: hasCoreData ? '恢复核心数据' : '整理恢复计划',
      hint: writeHintParts.length > 0 ? `写入 ${writeHintParts.join('、')}` : '本次恢复以文件与运行时状态为主',
    },
    {
      key: 'settle',
      title: hasFiles ? '恢复文件资源' : hasRuntime ? '整理运行环境' : '整理恢复结果',
      hint: settleHintParts.length > 0 ? `处理 ${settleHintParts.join('、')}` : '刷新任务状态与运行时配置',
    },
    {
      key: 'restart',
      title: '准备重启',
      hint: '等待面板重新上线',
    },
  ]
})

const statusText = computed(() => {
  switch (props.status) {
    case 'queued-restart':
      return '等待重启'
    case 'restarting':
      return '重启中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '恢复失败'
    case 'restart-timeout':
      return '等待超时'
    default:
      return '恢复中'
  }
})

const dialogTitle = computed(() => {
  switch (props.status) {
    case 'queued-restart':
    case 'completed':
      return '恢复完成'
    case 'restarting':
      return '面板正在重启'
    case 'failed':
    case 'restart-timeout':
      return '恢复状态'
    default:
      return '正在恢复备份'
  }
})

const heroIcon = computed(() => {
  switch (props.status) {
    case 'queued-restart':
    case 'completed':
      return CircleCheckFilled
    case 'failed':
    case 'restart-timeout':
      return WarningFilled
    case 'restarting':
      return RefreshRight
    default:
      return Box
  }
})

const themeClass = computed(() => {
  switch (props.status) {
    case 'queued-restart':
    case 'completed':
      return 'is-success'
    case 'failed':
    case 'restart-timeout':
      return 'is-danger'
    case 'restarting':
      return 'is-restarting'
    default:
      return 'is-running'
  }
})

const progressWidth = computed(() => `${Math.max(2, Math.min(100, props.percent || 0))}%`)

const currentStepIndex = computed(() => {
  if (props.status === 'queued-restart' || props.status === 'completed' || props.status === 'restarting' || props.status === 'restart-timeout') {
    return 4
  }

  switch (props.stage) {
    case 'preparing':
    case 'reading':
    case 'decrypting':
      return 0
    case 'extracting':
    case 'analyzing':
      return 1
    case 'restoring-data':
      return 2
    case 'restoring-files':
    case 'restoring-mirrors':
    case 'finalizing':
      return 3
    case 'completed':
      return 4
    case 'failed':
      return 0
    default:
      return 0
  }
})

function stepState(index: number) {
  if (props.status === 'failed') {
    if (index < currentStepIndex.value) {
      return 'is-done'
    }
    if (index === currentStepIndex.value) {
      return 'is-failed'
    }
    return 'is-pending'
  }

  if (index < currentStepIndex.value) {
    return 'is-done'
  }
  if (index === currentStepIndex.value) {
    if (props.status === 'queued-restart' || props.status === 'completed') {
      return 'is-done'
    }
    return 'is-active'
  }
  return 'is-pending'
}

function tagIcon(item: string) {
  switch (item) {
    case '脚本':
    case '日志':
      return Files
    default:
      return Box
  }
}

const stageLabel = computed(() => {
  switch (props.stage) {
    case 'preparing':
      return '准备恢复'
    case 'reading':
      return '读取备份'
    case 'decrypting':
      return '解密备份'
    case 'extracting':
      return '解包校验'
    case 'analyzing':
      return '分析结构'
    case 'restoring-data':
      return '写入数据'
    case 'restoring-files':
      return '恢复文件'
    case 'restoring-mirrors':
      return '恢复镜像'
    case 'finalizing':
      return '收尾整理'
    case 'completed':
      return '恢复完成'
    default:
      return '处理中'
  }
})

const elapsedLabel = computed(() => {
  if (!props.startedAt) {
    return ''
  }
  const startedAt = new Date(props.startedAt).getTime()
  if (Number.isNaN(startedAt)) {
    return ''
  }
  const seconds = Math.max(0, Math.floor((now.value - startedAt) / 1000))
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
})

function syncElapsedTimer(visible: boolean) {
  if (elapsedTimer) {
    clearInterval(elapsedTimer)
    elapsedTimer = null
  }
  if (!visible) {
    return
  }
  now.value = Date.now()
  elapsedTimer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
}

watch(
  () => props.visible,
  (visible) => {
    syncElapsedTimer(visible)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  syncElapsedTimer(false)
})
</script>

<template>
  <el-dialog
    :model-value="visible"
    class="restore-progress-dialog"
    :title="dialogTitle"
    width="620px"
    :fullscreen="fullscreen"
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="status === 'failed' || status === 'restart-timeout'"
    :show-close="status === 'failed' || status === 'restart-timeout'"
    @close="onClose"
  >
    <div class="restore-progress-shell" :class="themeClass">
      <div class="restore-progress-hero">
        <div class="restore-progress-visual">
          <div class="restore-progress-orbit" />
          <div class="restore-progress-rings">
            <span class="ring ring--outer" />
            <span class="ring ring--inner" />
          </div>
          <div class="restore-progress-core">
            <el-icon class="restore-progress-core__icon"><component :is="heroIcon" /></el-icon>
            <strong>{{ percent }}%</strong>
            <span>{{ statusText }}</span>
          </div>
        </div>

        <div class="restore-progress-copy">
          <span class="restore-progress-eyebrow">{{ dialogTitle }}</span>
          <h3>{{ filename || '备份恢复任务' }}</h3>
          <p>{{ message }}</p>
          <div class="restore-progress-meta">
            <span>{{ sourceLabel }}</span>
            <span>{{ stageLabel }}</span>
            <span v-if="elapsedLabel">已耗时 {{ elapsedLabel }}</span>
            <span v-if="countdown > 0"> {{ countdown }} 秒后自动重启 </span>
          </div>
        </div>
      </div>

      <div class="restore-progress-bar">
        <div class="restore-progress-bar__track">
          <div class="restore-progress-bar__fill" :style="{ width: progressWidth }" />
        </div>
        <div class="restore-progress-bar__labels">
          <span>{{ selectedSummaryText }}</span>
          <strong>{{ percent }}%</strong>
        </div>
      </div>

      <div v-if="selectedSummaryItems.length > 0" class="restore-progress-tags">
        <span v-for="item in selectedSummaryItems" :key="item" class="restore-progress-tag">
          <el-icon><component :is="tagIcon(item)" /></el-icon>
          {{ item }}
        </span>
      </div>

      <div class="restore-progress-steps">
        <div
          v-for="(step, index) in restoreSteps"
          :key="step.key"
          class="restore-step"
          :class="stepState(index)"
        >
          <div class="restore-step__badge">
            <el-icon v-if="stepState(index) === 'is-done'"><Select /></el-icon>
            <el-icon v-else-if="stepState(index) === 'is-failed'"><WarningFilled /></el-icon>
            <span v-else>{{ index + 1 }}</span>
          </div>
          <div class="restore-step__content">
            <strong>{{ step.title }}</strong>
            <span>{{ step.hint }}</span>
          </div>
        </div>
      </div>

      <div v-if="errorMessage && (status === 'failed' || status === 'restart-timeout')" class="restore-progress-error">
        <el-icon><WarningFilled /></el-icon>
        <div>
          <strong>{{ status === 'restart-timeout' ? '面板尚未重新连通' : '恢复失败原因' }}</strong>
          <p>{{ errorMessage }}</p>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="restore-progress-footer">
        <el-button
          v-if="status === 'queued-restart'"
          type="primary"
          @click="onRestartNow"
        >
          立即重启
        </el-button>
        <el-button
          v-if="status === 'failed' || status === 'restart-timeout'"
          @click="onClose"
        >
          关闭
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.restore-progress-shell {
  --dd-restore-accent: #2563eb;
  --dd-restore-accent-soft: rgba(37, 99, 235, 0.16);
  --dd-restore-accent-strong: rgba(37, 99, 235, 0.28);
  --dd-restore-surface:
    radial-gradient(circle at top, rgba(255, 255, 255, 0.92), rgba(255, 255, 255, 0.76)),
    linear-gradient(180deg, rgba(37, 99, 235, 0.06), rgba(15, 23, 42, 0.02));
  display: flex;
  flex-direction: column;
  gap: 22px;
  padding: 6px 2px 2px;

  &.is-success {
    --dd-restore-accent: #0f9f6e;
    --dd-restore-accent-soft: rgba(15, 159, 110, 0.16);
    --dd-restore-accent-strong: rgba(15, 159, 110, 0.28);
  }

  &.is-danger {
    --dd-restore-accent: #dc2626;
    --dd-restore-accent-soft: rgba(220, 38, 38, 0.14);
    --dd-restore-accent-strong: rgba(220, 38, 38, 0.24);
  }

  &.is-restarting {
    --dd-restore-accent: #d97706;
    --dd-restore-accent-soft: rgba(217, 119, 6, 0.16);
    --dd-restore-accent-strong: rgba(217, 119, 6, 0.26);
  }
}

.restore-progress-hero {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  gap: 22px;
  align-items: center;
  padding: 22px;
  border-radius: 24px;
  background: var(--dd-restore-surface);
  border: 1px solid var(--dd-restore-accent-soft);
  box-shadow: 0 18px 42px rgba(15, 23, 42, 0.08);
}

.restore-progress-visual {
  position: relative;
  width: 168px;
  height: 168px;
  margin: 0 auto;
}

.restore-progress-orbit {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background:
    conic-gradient(from 210deg, transparent 0deg, transparent 42deg, var(--dd-restore-accent) 120deg, transparent 188deg, rgba(255, 255, 255, 0.8) 320deg, transparent 360deg);
  filter: blur(0.4px);
  animation: restore-spin 5s linear infinite;
}

.restore-progress-rings .ring {
  position: absolute;
  inset: 18px;
  border-radius: 50%;
  border: 1px solid var(--dd-restore-accent-soft);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.5);
}

.restore-progress-rings .ring--outer {
  inset: 4px;
  animation: restore-pulse 2.4s ease-in-out infinite;
}

.restore-progress-rings .ring--inner {
  inset: 30px;
  border-style: dashed;
  animation: restore-pulse 2.4s ease-in-out infinite reverse;
}

.restore-progress-core {
  position: absolute;
  inset: 34px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: 50%;
  text-align: center;
  background:
    radial-gradient(circle at top, rgba(255, 255, 255, 0.96), rgba(248, 250, 252, 0.84)),
    linear-gradient(180deg, rgba(255, 255, 255, 0.9), rgba(255, 255, 255, 0.74));
  box-shadow:
    0 14px 28px rgba(15, 23, 42, 0.12),
    inset 0 0 0 1px rgba(255, 255, 255, 0.9);

  strong {
    font-size: 30px;
    line-height: 1;
    color: var(--dd-restore-accent);
  }

  span {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    letter-spacing: 0.08em;
  }
}

.restore-progress-core__icon {
  font-size: 22px;
  color: var(--dd-restore-accent);
}

.restore-progress-copy {
  min-width: 0;

  h3 {
    margin: 8px 0 10px;
    font-size: 24px;
    line-height: 1.2;
    color: var(--el-text-color-primary);
  }

  p {
    margin: 0;
    font-size: 14px;
    line-height: 1.8;
    color: var(--el-text-color-regular);
  }
}

.restore-progress-eyebrow {
  display: inline-flex;
  padding: 6px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: var(--dd-restore-accent);
  background: var(--dd-restore-accent-soft);
}

.restore-progress-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;

  span {
    display: inline-flex;
    align-items: center;
    min-height: 30px;
    padding: 0 12px;
    border-radius: 999px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    background: rgba(255, 255, 255, 0.72);
    border: 1px solid rgba(148, 163, 184, 0.18);
  }
}

.restore-progress-bar {
  padding: 18px 20px;
  border-radius: 20px;
  background: rgba(248, 250, 252, 0.9);
  border: 1px solid rgba(148, 163, 184, 0.16);
}

.restore-progress-bar__track {
  position: relative;
  height: 12px;
  overflow: hidden;
  border-radius: 999px;
  background: rgba(148, 163, 184, 0.16);
}

.restore-progress-bar__fill {
  position: relative;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, color-mix(in srgb, var(--dd-restore-accent) 82%, white), var(--dd-restore-accent));
  transition: width 0.45s ease;

  &::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.55), transparent);
    transform: translateX(-100%);
    animation: restore-shimmer 1.8s ease-in-out infinite;
  }
}

.restore-progress-bar__labels {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 12px;
  font-size: 13px;
  color: var(--el-text-color-secondary);

  strong {
    font-size: 15px;
    color: var(--dd-restore-accent);
  }
}

.restore-progress-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.restore-progress-tag {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 14px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.95), rgba(248, 250, 252, 0.88));
  border: 1px solid rgba(148, 163, 184, 0.16);
  box-shadow: 0 10px 22px rgba(15, 23, 42, 0.05);

  .el-icon {
    color: var(--dd-restore-accent);
    font-size: 13px;
  }
}

.restore-progress-steps {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}

.restore-step {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 16px 12px;
  border-radius: 18px;
  text-align: center;
  background: rgba(248, 250, 252, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.14);
  transition: transform 0.25s ease, border-color 0.25s ease, box-shadow 0.25s ease;

  &.is-active {
    border-color: var(--dd-restore-accent-soft);
    box-shadow: 0 14px 32px var(--dd-restore-accent-soft);
    transform: translateY(-2px);
  }

  &.is-done {
    border-color: color-mix(in srgb, var(--dd-restore-accent) 36%, white);
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.95), color-mix(in srgb, var(--dd-restore-accent) 8%, white));
  }

  &.is-failed {
    border-color: rgba(220, 38, 38, 0.3);
    background: rgba(254, 242, 242, 0.88);
  }
}

.restore-step__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  font-size: 14px;
  font-weight: 700;
  color: var(--el-text-color-secondary);
  background: rgba(255, 255, 255, 0.92);
  border: 1px solid rgba(148, 163, 184, 0.18);
}

.restore-step.is-active .restore-step__badge,
.restore-step.is-done .restore-step__badge {
  color: var(--dd-restore-accent);
  border-color: var(--dd-restore-accent-soft);
  box-shadow: 0 0 0 6px color-mix(in srgb, var(--dd-restore-accent) 10%, transparent);
}

.restore-step.is-failed .restore-step__badge {
  color: #dc2626;
  border-color: rgba(220, 38, 38, 0.18);
}

.restore-step__content {
  strong {
    display: block;
    font-size: 13px;
    color: var(--el-text-color-primary);
  }

  span {
    display: block;
    margin-top: 6px;
    font-size: 12px;
    line-height: 1.55;
    color: var(--el-text-color-secondary);
  }
}

.restore-progress-error {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  padding: 16px 18px;
  border-radius: 18px;
  background: rgba(254, 242, 242, 0.92);
  border: 1px solid rgba(220, 38, 38, 0.18);
  color: #b42318;

  strong {
    display: block;
    margin-bottom: 6px;
    font-size: 14px;
  }

  p {
    margin: 0;
    line-height: 1.7;
    color: #7f1d1d;
    word-break: break-word;
  }
}

.restore-progress-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@keyframes restore-spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

@keyframes restore-pulse {
  0%,
  100% {
    transform: scale(1);
    opacity: 0.7;
  }
  50% {
    transform: scale(1.03);
    opacity: 1;
  }
}

@keyframes restore-shimmer {
  to {
    transform: translateX(100%);
  }
}

@media (max-width: 768px) {
  .restore-progress-hero {
    grid-template-columns: 1fr;
    text-align: center;
  }

  .restore-progress-meta {
    justify-content: center;
  }

  .restore-progress-steps {
    grid-template-columns: 1fr;
  }
}

:global(html.dark) {
  .restore-progress-hero {
    background:
      radial-gradient(circle at top, rgba(30, 41, 59, 0.96), rgba(15, 23, 42, 0.88)),
      linear-gradient(180deg, rgba(59, 130, 246, 0.08), rgba(15, 23, 42, 0.24));
    box-shadow: 0 18px 42px rgba(0, 0, 0, 0.3);
  }

  .restore-progress-core,
  .restore-progress-bar,
  .restore-progress-tag,
  .restore-step,
  .restore-progress-meta span {
    background: color-mix(in srgb, var(--el-bg-color-overlay) 92%, black);
    border-color: var(--el-border-color-darker);
    box-shadow: none;
  }

  .restore-step.is-done {
    background: color-mix(in srgb, var(--el-color-primary) 12%, var(--el-bg-color-overlay));
  }

  .restore-step.is-failed,
  .restore-progress-error {
    background: color-mix(in srgb, #7f1d1d 35%, var(--el-bg-color-overlay));
    border-color: rgba(220, 38, 38, 0.3);
    color: #fecaca;
  }

  .restore-progress-error p {
    color: #fecaca;
  }
}
</style>
