<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { terminalApi, type TerminalSession } from '@/api/terminal'
import { extractError } from '@/utils/error'
import { TerminalLogBuffer } from '@/utils/terminalLogBuffer.mjs'

const session = ref<TerminalSession | null>(null)
const output = ref('')
const outputBuffer = new TerminalLogBuffer({ maxChars: 1_000_000, maxLines: 10_000 })
const command = ref('')
const terminal = ref<HTMLElement>()
const busy = ref(false)
const decoder = new TextDecoder()
let cursor = 0
let timer: number | null = null
let generation = 0

const running = computed(() => session.value?.status === 'running')
const statusLabel = computed(() => running.value ? '在线' : session.value ? `已结束 (${session.value.exit_code ?? '-'})` : '未连接')

function dimensions() {
  const width = terminal.value?.clientWidth || 800
  const height = terminal.value?.clientHeight || 480
  return {
    columns: Math.max(40, Math.min(200, Math.floor(width / 8.4))),
    rows: Math.max(12, Math.min(80, Math.floor(height / 19))),
  }
}

function decodeBase64(value: string) {
  const raw = atob(value)
  const bytes = Uint8Array.from(raw, char => char.charCodeAt(0))
  return decoder.decode(bytes, { stream: true })
}

async function start() {
  const requestGeneration = ++generation
  busy.value = true
  try {
    const size = dimensions()
    const response = await terminalApi.create(size.rows, size.columns)
    if (requestGeneration !== generation) {
      void terminalApi.remove(response.data.id).catch(() => undefined)
      return
    }
    session.value = response.data
    outputBuffer.clear()
    output.value = ''
    cursor = 0
    schedulePoll(50)
  } catch (error) {
    if (requestGeneration !== generation) return
    ElMessage.error(extractError(error, '启动终端失败'))
  } finally {
    if (requestGeneration === generation) busy.value = false
  }
}

async function poll() {
  if (!session.value) return
  const requestGeneration = generation
  const sessionId = session.value.id
  try {
    const response = await terminalApi.get(sessionId, cursor)
    if (requestGeneration !== generation || session.value?.id !== sessionId) return
    for (const chunk of response.data.output || []) {
      outputBuffer.append(decodeBase64(chunk.data))
      cursor = Math.max(cursor, chunk.cursor)
    }
    output.value = outputBuffer.text
    session.value = response.data
    await nextTick()
    if (requestGeneration !== generation || session.value?.id !== sessionId) return
    if (terminal.value) terminal.value.scrollTop = terminal.value.scrollHeight
    if (running.value) schedulePoll(180)
  } catch (error) {
    if (requestGeneration !== generation) return
    ElMessage.error(extractError(error, '终端连接中断'))
  }
}

function schedulePoll(delay: number) {
  if (timer !== null) window.clearTimeout(timer)
  timer = window.setTimeout(() => void poll(), delay)
}

async function send(value = `${command.value}\n`) {
  if (!session.value || !running.value || !value) return
  try {
    await terminalApi.input(session.value.id, value)
    command.value = ''
    schedulePoll(20)
  } catch (error) {
    ElMessage.error(extractError(error, '发送终端输入失败'))
  }
}

async function stop() {
  if (!session.value || !running.value) return
  try {
    const response = await terminalApi.stop(session.value.id)
    session.value = response.data
    schedulePoll(20)
  } catch (error) {
    ElMessage.error(extractError(error, '停止终端失败'))
  }
}

async function resize() {
  if (!session.value || !running.value) return
  const size = dimensions()
  await terminalApi.resize(session.value.id, size.rows, size.columns).catch(() => undefined)
}

onMounted(() => {
  window.addEventListener('resize', resize)
  void start()
})

onBeforeUnmount(() => {
  generation++
  if (timer !== null) window.clearTimeout(timer)
  window.removeEventListener('resize', resize)
  if (session.value) void terminalApi.remove(session.value.id).catch(() => undefined)
})
</script>

<template>
  <section class="terminal-page">
    <header class="terminal-header">
      <div>
        <p class="eyebrow">ANDROID ROOTFS</p>
        <h1>交互式终端</h1>
        <p class="subtitle">Alpine Linux · Bash · PTY session</p>
      </div>
      <div class="terminal-actions">
        <span class="status" :class="{ live: running }"><i />{{ statusLabel }}</span>
        <el-button v-if="running" plain @click="send('\u0003')">Ctrl-C</el-button>
        <el-button v-if="running" type="danger" plain @click="stop">停止</el-button>
        <el-button v-else type="primary" :loading="busy" @click="start">新建会话</el-button>
      </div>
    </header>

    <div ref="terminal" class="terminal-screen" aria-live="polite" aria-label="交互式终端输出">
      <pre>{{ output || '正在建立 rootfs PTY 会话...' }}</pre>
    </div>

    <form class="terminal-input" @submit.prevent="send()">
      <span>$</span>
      <input v-model="command" :disabled="!running" autocomplete="off" spellcheck="false" placeholder="输入 Linux 命令并按 Enter" />
      <button type="submit" :disabled="!running || !command">执行</button>
    </form>
  </section>
</template>

<style scoped>
.terminal-page { min-height: calc(100vh - 104px); padding: 26px; color: #d8f3e5; background: radial-gradient(circle at 84% 10%, #153f35 0, transparent 34%), #07110e; border-radius: 18px; display: grid; grid-template-rows: auto minmax(360px, 1fr) auto; gap: 18px; }
.terminal-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.eyebrow { margin: 0 0 4px; color: #5ee6a8; font: 700 11px/1.2 ui-monospace, monospace; letter-spacing: .18em; }
h1 { margin: 0; color: #f1fff7; font: 700 28px/1.2 system-ui, sans-serif; }
.subtitle { margin: 5px 0 0; color: #80a596; font-size: 13px; }
.terminal-actions { display: flex; align-items: center; gap: 9px; }
.status { display: inline-flex; align-items: center; gap: 8px; padding: 7px 11px; border: 1px solid #29493d; border-radius: 999px; color: #94aa9f; font-size: 12px; }
.status i { width: 7px; height: 7px; border-radius: 50%; background: #67776f; }
.status.live { color: #8af0ba; }
.status.live i { background: #42e692; box-shadow: 0 0 12px #42e692; }
.terminal-screen { overflow: auto; padding: 22px; border: 1px solid #204237; border-radius: 14px; background: rgba(1, 7, 5, .88); box-shadow: inset 0 0 60px rgba(22, 91, 67, .12); }
pre { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; color: #c7f5dc; font: 13px/1.55 "SFMono-Regular", Consolas, monospace; }
.terminal-input { display: grid; grid-template-columns: auto 1fr auto; align-items: center; gap: 12px; padding: 11px 13px; border: 1px solid #285344; border-radius: 12px; background: #0b1d17; color: #4fe09a; font: 600 14px ui-monospace, monospace; }
.terminal-input input { min-width: 0; border: 0; outline: 0; color: #e7fff1; background: transparent; font: inherit; }
.terminal-input input::placeholder { color: #557064; }
.terminal-input button { border: 0; border-radius: 8px; padding: 8px 14px; background: #34d78b; color: #052116; font-weight: 700; cursor: pointer; }
.terminal-input button:disabled { opacity: .35; cursor: default; }
@media (max-width: 720px) { .terminal-page { min-height: calc(100vh - 78px); padding: 15px; border-radius: 0; } .terminal-header { align-items: flex-start; flex-direction: column; } .terminal-actions { width: 100%; overflow-x: auto; } .terminal-screen { padding: 15px; } }
</style>
