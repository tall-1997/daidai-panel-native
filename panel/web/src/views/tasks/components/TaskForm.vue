<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import CronInput from './CronInput.vue'
import StopScheduleInput from './StopScheduleInput.vue'
import { mergeTaskLabels, splitTaskLabels } from '../taskLabels'
import { useResponsive } from '@/composables/useResponsive'
import type { PythonRuntimeInfo } from '@/api/deps'

const props = defineProps<{
  visible: boolean
  task?: any
  prefill?: any
  defaultPythonVersion?: string
  pythonRuntimes?: PythonRuntimeInfo[]
  notificationChannels?: { id: number; name: string; type: string; enabled: boolean }[]
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  'submit': [data: any]
}>()

const form = ref({
  name: '',
  command: '',
  python_version: '3.12',
  cron_expression: '0 0 * * *',
  task_type: 'cron',
  timeout: 0,
  success_exit_codes: '0',
  random_delay_seconds: null as number | null,
  max_retries: 0,
  retry_interval: 60,
  notify_on_failure: false,
  notify_on_success: false,
  notification_channel_id: null as number | null,
  labels: [] as string[],
  depends_on: null as number | null,
  task_before: '',
  task_after: '',
  allow_multiple_instances: false,
  stop_schedule: '',
  notify_on_abort: false,
  group_name: '',
})

const labelInput = ref('')
const activeTab = ref('basic')
const internalLabels = ref<string[]>([])
const randomDelayMode = ref<'inherit' | 'disabled' | 'custom'>('inherit')
const { dialogFullscreen } = useResponsive()
const allPythonVersions = ['3.10', '3.11', '3.12']

// Python 版本下拉选项：带上后端算好的 available/message，
// 未安装版本仍展示但会被标注/禁用，避免任务默认或误选到本机不存在的解释器。
const pythonVersionOptions = computed(() => {
  const runtimes = (props.pythonRuntimes || [])
    .filter(item => allPythonVersions.includes(item.version))
    .map(item => ({ version: item.version, available: item.available, message: item.message }))
  // 后端未返回运行时信息时退回全量候选，且默认视为可用（无从判断可用性）
  return runtimes.length > 0
    ? runtimes
    : allPythonVersions.map(version => ({ version, available: true, message: '' }))
})

// 当前选中版本若未安装，展示后端给的提示，让用户知道该版本不可用及如何处理
const selectedPythonRuntimeMessage = computed(() => {
  const current = pythonVersionOptions.value.find(item => item.version === form.value.python_version)
  return current && !current.available ? current.message : ''
})

function getDefaultPythonVersion() {
  // 默认值优先落在已安装（available）的版本上，不要默认到未安装版本；
  // 单版本 Docker 镜像也只会返回当前镜像支持的小版本。
  const options = pythonVersionOptions.value
  const preferred = props.defaultPythonVersion || ''
  const defaultOption = options.find(item => item.version === preferred)
  if (defaultOption?.available) return defaultOption.version
  const firstAvailable = options.find(item => item.available)
  if (firstAvailable) return firstAvailable.version
  // 全部未安装时退回后端默认/首个候选
  return defaultOption?.version || options[0]?.version || preferred || '3.12'
}

watch(() => props.visible, (val) => {
  if (val && props.task) {
    const { editableLabels, internalLabels: hiddenLabels, groupName } = splitTaskLabels(props.task.labels || [])
    internalLabels.value = hiddenLabels
    const taskRandomDelay = typeof props.task.random_delay_seconds === 'number'
      ? props.task.random_delay_seconds
      : props.task.random_delay_seconds == null
        ? null
        : Number(props.task.random_delay_seconds)
    if (taskRandomDelay == null) {
      randomDelayMode.value = 'inherit'
    } else if (taskRandomDelay <= 0) {
      randomDelayMode.value = 'disabled'
    } else {
      randomDelayMode.value = 'custom'
    }
    form.value = {
      name: props.task.name || '',
      command: props.task.command || '',
      python_version: props.task.python_version || getDefaultPythonVersion(),
      cron_expression: props.task.cron_expression || '* * * * *',
      task_type: props.task.task_type || 'cron',
      timeout: props.task.timeout ?? 0,
      success_exit_codes: props.task.success_exit_codes || '0',
      random_delay_seconds: taskRandomDelay,
      max_retries: props.task.max_retries ?? 0,
      retry_interval: props.task.retry_interval ?? 60,
      notify_on_failure: props.task.notify_on_failure ?? false,
      notify_on_success: props.task.notify_on_success ?? false,
      notification_channel_id: props.task.notification_channel_id ?? null,
      labels: editableLabels,
      group_name: groupName,
      depends_on: props.task.depends_on || null,
      task_before: props.task.task_before || '',
      task_after: props.task.task_after || '',
      allow_multiple_instances: props.task.allow_multiple_instances ?? false,
      stop_schedule: props.task.stop_schedule || '',
      notify_on_abort: props.task.notify_on_abort ?? false,
    }
  } else if (val) {
    const p = props.prefill
    internalLabels.value = []
    randomDelayMode.value = 'inherit'
    form.value = {
      name: p?.name || '', command: p?.command || '',
      python_version: p?.python_version || getDefaultPythonVersion(),
      cron_expression: p?.cron_expression || '* * * * *',
      task_type: p?.task_type || 'cron',
      timeout: 0, success_exit_codes: '0', random_delay_seconds: null, max_retries: 0, retry_interval: 60,
      notify_on_failure: false, notify_on_success: false, notification_channel_id: null, labels: [], depends_on: null,
      task_before: '', task_after: '', allow_multiple_instances: false, group_name: '', stop_schedule: '', notify_on_abort: false,
    }
  }
  activeTab.value = 'basic'
})

// 后端默认版本或运行时可用性变化时，若正在新建任务则重新计算默认 Python 版本
watch([() => props.defaultPythonVersion, () => props.pythonRuntimes], () => {
  if (props.visible && !props.task && !props.prefill?.python_version) {
    form.value.python_version = getDefaultPythonVersion()
  }
})

watch(() => form.value.task_type, (value) => {
  if (value === 'cron' && !form.value.cron_expression) {
    form.value.cron_expression = '0 0 * * *'
  }
})

watch(randomDelayMode, (mode) => {
  if (mode === 'inherit') {
    form.value.random_delay_seconds = null
    return
  }
  if (mode === 'disabled') {
    form.value.random_delay_seconds = 0
    return
  }
  if (form.value.random_delay_seconds == null || form.value.random_delay_seconds <= 0) {
    form.value.random_delay_seconds = 60
  }
})

function addLabel() {
  const val = labelInput.value.trim()
  if (val && !form.value.labels.includes(val)) {
    form.value.labels.push(val)
  }
  labelInput.value = ''
}

function removeLabel(label: string) {
  form.value.labels = form.value.labels.filter(l => l !== label)
}

function handleSubmit() {
  if (!form.value.name || !form.value.command) {
    ElMessage.warning('请填写任务名称和执行命令')
    return
  }
  if (randomDelayMode.value === 'custom') {
    if (form.value.random_delay_seconds == null || form.value.random_delay_seconds <= 0) {
      ElMessage.warning('请输入大于 0 的随机延迟秒数')
      return
    }
  }
  const successExitCodes = form.value.success_exit_codes
    .trim()
    .split(/[,，\s]+/)
    .filter(Boolean)
  if (successExitCodes.length === 0 || successExitCodes.some(code => !/^\d+$/.test(code) || Number(code) > 255)) {
    ElMessage.warning('成功退出码只能填写 0-255 的整数，多个值请用逗号分隔')
    return
  }
  const data = { ...form.value }
  // 提交前统一为英文逗号并去重，后端仍会再次校验，避免异常数据写入数据库。
  data.success_exit_codes = [...new Set(successExitCodes.map(code => String(Number(code))))].join(',')
  if (data.task_type === 'cron') {
    if (!data.cron_expression) return
  } else {
    data.cron_expression = ''
  }
  data.labels = mergeTaskLabels(form.value.labels, internalLabels.value, form.value.group_name)
  if (!data.task_before) data.task_before = ''
  if (!data.task_after) data.task_after = ''
  emit('submit', data)
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    :title="task ? '编辑任务' : '新建任务'"
    width="640px"
    :fullscreen="dialogFullscreen"
    :lock-scroll="false"
    destroy-on-close
    @update:model-value="emit('update:visible', $event)"
  >
    <el-tabs v-model="activeTab">
      <el-tab-pane label="基本信息" name="basic">
        <el-form :model="form" :label-width="dialogFullscreen ? 'auto' : '100px'" :label-position="dialogFullscreen ? 'top' : 'right'">
          <el-form-item label="任务名称" required>
            <el-input v-model="form.name" placeholder="任务名称" />
          </el-form-item>
          <el-form-item label="执行命令" required>
            <el-input v-model="form.command" placeholder="如: script.py 或 python3 script.py" />
            <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px">
              支持 task 脚本名 格式,自动根据扩展名选择解释器 (.py/.js/.ts/.sh)
            </div>
          </el-form-item>
          <el-form-item label="Python版本">
            <el-select v-model="form.python_version" style="width: 100%">
              <el-option
                v-for="option in pythonVersionOptions"
                :key="option.version"
                :label="option.available ? `Python ${option.version}` : `Python ${option.version}（未安装）`"
                :value="option.version"
                :disabled="!option.available"
              />
            </el-select>
            <div
              v-if="selectedPythonRuntimeMessage"
              style="font-size: 12px; color: var(--el-color-warning); margin-top: 4px"
            >
              {{ selectedPythonRuntimeMessage }}
            </div>
          </el-form-item>
          <el-form-item label="定时类型" required>
            <el-select v-model="form.task_type" style="width: 100%">
              <el-option label="常规定时" value="cron" />
              <el-option label="手动运行" value="manual" />
              <el-option label="开机运行" value="startup" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="form.task_type === 'cron'" label="定时规则" required>
            <CronInput v-model="form.cron_expression" />
          </el-form-item>
          <el-form-item v-if="form.task_type === 'cron'" label="定时停止">
            <StopScheduleInput v-model="form.stop_schedule" />
          </el-form-item>
          <el-form-item v-else label="执行说明">
            <div style="font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.7">
              <template v-if="form.task_type === 'manual'">
                不自动调度，仅在你手动点击“运行”或批量运行时执行。
              </template>
              <template v-else>
                每天首次启动面板时自动执行一次；当天再次重启不会重复自动执行，手动点击“运行”不受限制。
              </template>
            </div>
          </el-form-item>
          <el-form-item label="标签">
            <div class="label-area">
              <el-tag
                v-for="label in form.labels"
                :key="label"
                closable
                @close="removeLabel(label)"
              >{{ label }}</el-tag>
              <el-input
                v-model="labelInput"
                size="small"
                style="width: 120px"
                placeholder="添加标签"
                @keyup.enter="addLabel"
              />
            </div>
          </el-form-item>
          <el-form-item label="任务分组">
            <el-input v-model="form.group_name" placeholder="例如 京东 / 日常 / 中国联通" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="高级设置" name="advanced">
        <el-form :model="form" :label-width="dialogFullscreen ? 'auto' : '120px'" :label-position="dialogFullscreen ? 'top' : 'right'">
          <el-form-item label="超时(秒)">
            <el-input-number v-model="form.timeout" :min="0" :max="604800" />
            <div v-if="form.timeout === 0" style="font-size: 11px; color: var(--el-color-warning); margin-top: 4px">
              设置为 0 表示永不超时，任务将持续运行直到手动停止或定时停止。
            </div>
          </el-form-item>
          <el-form-item label="成功退出码">
            <div class="advanced-field-block">
              <el-input v-model="form.success_exit_codes" maxlength="128" placeholder="0" />
              <div class="advanced-field-hint">
                普通任务保持 0。只有脚本业务成功但固定返回 1 时才填写 0,1；超时、启动错误和主动停止不会因此变成成功。
              </div>
            </div>
          </el-form-item>
          <el-form-item label="随机延迟">
            <div class="advanced-field-block">
              <el-radio-group v-model="randomDelayMode">
                <el-radio value="inherit">继承系统设置</el-radio>
                <el-radio value="disabled">不随机延迟</el-radio>
                <el-radio value="custom">任务单独设置</el-radio>
              </el-radio-group>
              <div v-if="randomDelayMode === 'custom'" class="advanced-inline-input">
                <el-input-number v-model="form.random_delay_seconds" :min="1" :max="86400" />
                <span>秒</span>
              </div>
              <div class="advanced-field-hint">
                仅当前任务生效。未单独设置时继续沿用系统设置里的全局随机延迟；设置为“不随机延迟”可明确跳过全局规则。
              </div>
            </div>
          </el-form-item>
          <el-form-item label="最大重试次数">
            <el-input-number v-model="form.max_retries" :min="0" :max="10" />
          </el-form-item>
          <el-form-item label="重试间隔(秒)">
            <el-input-number v-model="form.retry_interval" :min="0" :max="3600" />
          </el-form-item>
          <el-form-item label="依赖任务ID">
            <el-input-number v-model="form.depends_on" :min="0" controls-position="right" placeholder="可选" />
          </el-form-item>
          <el-form-item label="失败时通知">
            <el-switch v-model="form.notify_on_failure" />
          </el-form-item>
          <el-form-item label="成功时通知">
            <el-switch v-model="form.notify_on_success" />
            <span style="font-size: 12px; color: var(--el-text-color-secondary); margin-left: 8px">任务执行成功后发送通知</span>
          </el-form-item>
          <el-form-item label="通知渠道">
            <div style="width: 100%">
              <el-select
                v-model="form.notification_channel_id"
                clearable
                filterable
                placeholder="留空则发送到全部启用渠道"
                style="width: 100%"
              >
                <el-option
                  v-for="channel in (props.notificationChannels || [])"
                  :key="channel.id"
                  :label="channel.enabled ? `${channel.name} (${channel.type})` : `${channel.name} (${channel.type}，已禁用)`"
                  :value="channel.id"
                />
              </el-select>
              <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px">
                可绑定单个通知渠道；留空时仍按全部已启用渠道发送。
              </div>
            </div>
          </el-form-item>
          <el-form-item label="终止时通知">
            <el-switch v-model="form.notify_on_abort" />
            <span style="font-size: 12px; color: var(--el-text-color-secondary); margin-left: 8px">任务被手动停止或定时停止后发送终止通知；终止状态会单独统计，不再算成功或失败。</span>
          </el-form-item>
          <el-form-item label="允许多实例">
            <el-switch v-model="form.allow_multiple_instances" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="前后置脚本" name="hooks">
        <el-form :model="form" :label-width="dialogFullscreen ? 'auto' : '100px'" :label-position="dialogFullscreen ? 'top' : 'right'">
          <div class="hooks-help">
            当前任务专属的 shell 脚本。前置脚本会在目标脚本执行前运行，后置脚本会在目标脚本结束后运行；如果任务命令传入了参数，这里也可以通过 $1、$2 等读取同一份参数。
          </div>
          <el-form-item label="前置脚本">
            <el-input
              v-model="form.task_before"
              type="textarea"
              :rows="4"
              placeholder="任务执行前运行，例如设置代理、准备环境变量、创建临时目录；可用 $1、$2 读取任务参数"
            />
          </el-form-item>
          <el-form-item label="后置脚本">
            <el-input
              v-model="form.task_after"
              type="textarea"
              :rows="4"
              placeholder="任务执行后运行，例如清理临时文件、输出收尾日志、恢复环境；可用 $1、$2 读取任务参数"
            />
          </el-form-item>
        </el-form>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="emit('update:visible', false)">取消</el-button>
      <el-button type="primary" @click="handleSubmit">{{ task ? '更新' : '创建' }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.label-area {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.advanced-field-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.advanced-inline-input {
  display: flex;
  align-items: center;
  gap: 8px;
}

.advanced-field-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

.hooks-help {
  margin: 0 0 14px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
}

@media (max-width: 768px) {
  .label-area {
    align-items: stretch;
  }

  .advanced-inline-input {
    flex-wrap: wrap;
  }
}
</style>
