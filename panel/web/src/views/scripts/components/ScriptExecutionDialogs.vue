<script setup lang="ts">
import { Edit, RefreshRight, Select, Tickets, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import { computed, defineAsyncComponent } from 'vue'
import { ansiToHtml, normalizeAnsi } from '@/utils/ansi'

const MonacoEditor = defineAsyncComponent(() => import('@/components/MonacoEditor.vue'))

const showCodeRunner = defineModel<boolean>('showCodeRunner', { required: true })
const runnerCode = defineModel<string>('runnerCode', { required: true })
const runnerLanguage = defineModel<string>('runnerLanguage', { required: true })
const showDebugDialog = defineModel<boolean>('showDebugDialog', { required: true })
const debugCode = defineModel<string>('debugCode', { required: true })
const debugCodeChanged = defineModel<boolean>('debugCodeChanged', { required: true })

const props = defineProps<{
  isMobile: boolean
  editorLanguage: string
  debugFileName: string
  debugLogs: string[]
  debugRunning: boolean
  debugSaving: boolean
  debugError: string
  debugExitCode: number | null
  runnerLogs: string[]
  runnerRunning: boolean
  runnerExitCode: number | null
  runnerError: string
  onDebugStart: () => void | Promise<void>
  onDebugSave: () => void | Promise<void>
  onDebugStop: () => void | Promise<void>
  onRunCode: () => void | Promise<void>
  onStopRunner: () => void | Promise<void>
}>()

const debugLogsHtml = computed(() => ansiToHtml(normalizeAnsi(props.debugLogs.join('\n'))))
const runnerLogsHtml = computed(() => ansiToHtml(normalizeAnsi(props.runnerLogs.join('\n'))))

function markDebugCodeChanged() {
  debugCodeChanged.value = true
}
</script>

<template>
  <el-dialog
    v-model="showCodeRunner"
    title="代码运行器"
    fullscreen
    class="script-execution-fullscreen-dialog"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div class="debug-container debug-dialog-container" :class="{ mobile: isMobile }">
      <div class="debug-code-panel">
        <div class="panel-header">
          <el-icon><Edit /></el-icon>
          <span>代码编辑</span>
          <el-select v-model="runnerLanguage" size="small" style="width: 130px; margin-left: auto">
            <el-option label="Python" value="python" />
            <el-option label="JavaScript" value="javascript" />
            <el-option label="TypeScript" value="typescript" />
            <el-option label="Shell" value="shell" />
            <el-option label="Go" value="go" />
          </el-select>
        </div>
        <div class="panel-content" style="padding: 0">
          <MonacoEditor
            v-if="showCodeRunner"
            v-model="runnerCode"
            :language="runnerLanguage === 'shell' ? 'shell' : runnerLanguage"
            min-height="0"
            style="height: 100%; min-height: 0"
          />
        </div>
      </div>
      <div class="debug-log-panel">
        <div class="panel-header">
          <el-icon><Tickets /></el-icon>
          <span>运行输出</span>
          <el-tag v-if="runnerRunning" type="warning" size="small" effect="plain">运行中</el-tag>
          <el-tag v-else-if="runnerError || runnerLogs.length > 0" :type="runnerExitCode === 0 ? 'success' : 'danger'" size="small" effect="plain">
            {{ runnerExitCode === 0 ? '成功' : '失败' }}
          </el-tag>
        </div>
        <div class="panel-content debug-log-content dd-log-surface">
          <div v-if="runnerError" class="debug-error">
            <el-alert type="error" :title="runnerError === 'failed' ? `退出码: ${runnerExitCode}` : runnerError" :closable="false" show-icon />
          </div>
          <pre v-if="runnerLogs.length > 0" class="debug-logs" v-html="runnerLogsHtml"></pre>
          <el-empty v-if="!runnerLogs.length && !runnerError" description="点击运行按钮执行代码" :image-size="80" />
        </div>
      </div>
    </div>
    <template #footer>
      <el-button v-if="!runnerRunning && !runnerLogs.length && !runnerError" type="primary" @click="onRunCode">
        <el-icon><VideoPlay /></el-icon>运行
      </el-button>
      <el-button v-if="runnerRunning" type="danger" @click="onStopRunner">
        <el-icon><VideoPause /></el-icon>停止
      </el-button>
      <el-button v-if="!runnerRunning && (runnerLogs.length > 0 || runnerError)" type="primary" @click="onRunCode">
        <el-icon><RefreshRight /></el-icon>重新运行
      </el-button>
      <el-button @click="showCodeRunner = false">关闭</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="showDebugDialog"
    title="调试运行"
    fullscreen
    class="script-execution-fullscreen-dialog"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div class="debug-container debug-dialog-container" :class="{ mobile: isMobile }">
      <div class="debug-code-panel">
        <div class="panel-header">
          <el-icon><Edit /></el-icon>
          <span>{{ debugFileName }}</span>
          <el-tag v-if="debugCodeChanged" type="warning" size="small" effect="plain">已修改</el-tag>
        </div>
        <div class="panel-content" style="padding: 0">
          <MonacoEditor
            v-if="showDebugDialog"
            v-model="debugCode"
            :language="editorLanguage"
            min-height="0"
            style="height: 100%; min-height: 0"
            @update:modelValue="markDebugCodeChanged"
          />
        </div>
      </div>
      <div class="debug-log-panel">
        <div class="panel-header">
          <el-icon><Tickets /></el-icon>
          <span>调试日志</span>
          <el-tag v-if="debugRunning" type="warning" size="small" effect="plain">运行中</el-tag>
          <el-tag v-else-if="debugLogs.length > 0" type="success" size="small" effect="plain">已完成</el-tag>
        </div>
        <div class="panel-content debug-log-content dd-log-surface">
          <div v-if="debugError" class="debug-error">
            <el-alert type="error" :title="`退出码: ${debugExitCode}`" :closable="false" show-icon />
          </div>
          <pre v-if="debugLogs.length > 0" class="debug-logs" v-html="debugLogsHtml"></pre>
          <el-empty v-if="!debugLogs.length && !debugError" description="点击运行按钮开始调试" :image-size="80" />
        </div>
      </div>
    </div>
    <template #footer>
      <el-button v-if="!debugRunning && !debugLogs.length && !debugError" type="primary" @click="onDebugStart">
        <el-icon><VideoPlay /></el-icon>运行
      </el-button>
      <el-button :disabled="!debugCodeChanged || debugRunning || debugSaving" @click="onDebugSave">
        <el-icon><Select /></el-icon>{{ debugSaving ? '保存中' : '保存' }}
      </el-button>
      <el-button v-if="debugRunning" type="danger" @click="onDebugStop">
        <el-icon><VideoPause /></el-icon>停止
      </el-button>
      <el-button v-if="!debugRunning && (debugLogs.length > 0 || debugError)" type="primary" @click="onDebugStart">
        <el-icon><RefreshRight /></el-icon>重新运行
      </el-button>
      <el-button @click="showDebugDialog = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.debug-container {
  display: flex;
  gap: 16px;
  // 全屏调试时让代码区和日志区尽量吃满可用高度，不再像小弹窗一样压缩内容。
  height: 100%;
  min-height: 0;
  max-height: none;
  min-width: 0;
}

.debug-code-panel,
.debug-log-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-bg-color);
}

/*
  整屏滑入后，内部两块面板错落淡入，营造层次感。
  只用 opacity，不用 transform：.debug-code-panel 内含 Monaco，残留 transform
  会成为其 fixed 浮层的包含块导致错位；opacity 结束为 1 不产生层叠上下文，安全。
*/
.debug-code-panel {
  animation: dd-script-panel-fade var(--dd-motion-normal) var(--dd-ease-decelerate) both;
  animation-delay: 60ms;
}

.debug-log-panel {
  animation: dd-script-panel-fade var(--dd-motion-normal) var(--dd-ease-decelerate) both;
  animation-delay: 140ms;
}

@keyframes dd-script-panel-fade {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

.debug-dialog-container {
  height: 100%;
  min-height: 0;
  max-height: none;
}

.panel-header {
  padding: 12px 16px;
  background: var(--el-fill-color-light);
  border-bottom: 1px solid var(--el-border-color-light);
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
  flex-shrink: 0;
}

.panel-content {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
}

.debug-log-content.dd-log-surface {
  // 脚本运行输出和调试日志也属于日志窗口，复用统一滚动条增强；
  // 外层面板已经有边框，这里去掉重复边框和阴影，避免视觉变厚。
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

.debug-error {
  margin-bottom: 12px;
}

.debug-logs {
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.6;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--el-text-color-primary);
  flex: 1;
}

.debug-container.mobile {
  flex-direction: column;
  height: 100%;
  min-height: 0;
  max-height: none;

  .debug-code-panel,
  .debug-log-panel {
    flex: 1 1 0;
    min-height: 180px;
    max-height: none;
  }

  .panel-content {
    padding: 8px;
  }
}
</style>

<style lang="scss">
/*
  Element Plus 的 el-dialog 会 teleport 到 body，scoped 样式很难稳定命中根节点。
  这里用唯一 class 只作用于脚本调试/代码运行器，让桌面端和移动端都使用最大化工作区。
*/
.script-execution-fullscreen-dialog {
  display: flex;
  flex-direction: column;

  .el-dialog__header {
    flex-shrink: 0;
    padding: 14px 18px;
    margin: 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }

  .el-dialog__body {
    flex: 1 1 0;
    min-height: 0;
    padding: 14px 18px;
    overflow: hidden;
  }

  .el-dialog__footer {
    flex-shrink: 0;
    padding: 12px 18px;
    border-top: 1px solid var(--el-border-color-lighter);
  }
}

/*
  打开时整屏工作区从底部滑入升起，比默认缩放更有"工作台升起"的高级感。
  用 .el-dialog.script-execution-fullscreen-dialog 双 class 提高特异性，
  覆盖全局 .el-dialog / .dialog-fade-enter-active 的默认进场动画。
*/
.el-dialog.script-execution-fullscreen-dialog {
  animation: dd-script-sheet-up var(--dd-motion-page) var(--dd-ease-decelerate) !important;
  transform-origin: center bottom;
}

/*
  不加 both / forwards：动画收尾后 transform 自然清空。
  否则全屏 dialog 残留 transform 会成为 Monaco 内部 position:fixed 浮层
  （补全/悬浮提示）的包含块，导致弹层错位。to 为单位变换，视觉无跳变。
*/
@keyframes dd-script-sheet-up {
  from {
    opacity: 0;
    transform: translateY(40px) scale(0.985);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}
</style>
