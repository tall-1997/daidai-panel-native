<script setup lang="ts">
import { computed } from 'vue'
import { getDisplayTaskLabels } from '../taskLabels'
import { useResponsive } from '@/composables/useResponsive'
import TaskCronList from './TaskCronList.vue'

const props = defineProps<{
  visible: boolean
  task: any
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()
const { dialogFullscreen } = useResponsive()

const statusText = computed(() => {
  if (props.task?.status === 0) return '已禁用'
  if (props.task?.status === 0.5) return '排队中'
  if (props.task?.status === 2) return '运行中'
  return '已启用'
})

const statusType = computed(() => {
  if (props.task?.status === 0) return 'info'
  if (props.task?.status === 0.5) return 'warning'
  if (props.task?.status === 2) return 'warning'
  return 'success'
})

const displayLabels = computed(() => {
  if (Array.isArray(props.task?.display_labels) && props.task.display_labels.length > 0) {
    return props.task.display_labels
  }
  return getDisplayTaskLabels(props.task?.labels || [])
})

const formatTime = (t: string) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const taskTypeText = computed(() => {
  if (props.task?.task_type === 'manual') return '手动运行'
  if (props.task?.task_type === 'startup') return '开机运行'
  return '常规定时'
})

const cronExpressions = computed(() => {
  if (Array.isArray(props.task?.cron_expressions) && props.task.cron_expressions.length > 0) {
    return props.task.cron_expressions
  }
  return String(props.task?.cron_expression || '')
    .split(/\r?\n/)
    .map(item => item.trim())
    .filter(Boolean)
})

function handleClose() {
  emit('update:visible', false)
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="任务详情"
    width="700px"
    :fullscreen="dialogFullscreen"
    :lock-scroll="false"
    @close="handleClose"
  >
    <el-descriptions v-if="task" :column="dialogFullscreen ? 1 : 2" border>
      <el-descriptions-item label="任务名称" :span="2">
        <div style="display: flex; align-items: center; gap: 8px">
          <el-icon v-if="task.is_pinned" color="var(--el-color-warning)"><Star /></el-icon>
          <span>{{ task.name }}</span>
        </div>
      </el-descriptions-item>
      <el-descriptions-item label="任务ID">{{ task.id }}</el-descriptions-item>
      <el-descriptions-item label="状态">
        <el-tag :type="statusType" size="small">{{ statusText }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="定时类型">
        {{ taskTypeText }}
      </el-descriptions-item>
      <el-descriptions-item label="定时规则">
        <TaskCronList
          v-if="task.task_type === 'cron'"
          :expressions="cronExpressions"
        />
        <span v-else style="color: var(--el-text-color-placeholder)">不使用 Cron</span>
      </el-descriptions-item>
      <el-descriptions-item label="执行命令" :span="2">
        <code style="word-break: break-all">{{ task.command }}</code>
      </el-descriptions-item>
      <el-descriptions-item label="随机延迟">
        <span v-if="task.random_delay_seconds == null" style="color: var(--el-text-color-secondary)">继承系统设置</span>
        <span v-else-if="task.random_delay_seconds === 0">不随机延迟</span>
        <span v-else>最多 {{ task.random_delay_seconds }} 秒</span>
      </el-descriptions-item>
      <el-descriptions-item label="标签" :span="2">
        <el-tag v-for="label in displayLabels" :key="label" size="small" effect="plain" style="margin-right: 6px">
          {{ label }}
        </el-tag>
        <span v-if="displayLabels.length === 0" style="color: var(--el-text-color-placeholder)">无</span>
      </el-descriptions-item>
      <el-descriptions-item label="上次运行状态">
        <el-tag v-if="task.last_run_status === null" type="info" size="small">未运行</el-tag>
        <el-tag v-else-if="task.last_run_status === 0" type="success" size="small">成功</el-tag>
        <el-tag v-else-if="task.last_run_status === 2" type="warning" size="small">已终止</el-tag>
        <el-tag v-else type="danger" size="small">失败</el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="上次运行耗时">
        <span v-if="task.last_running_time != null">{{ task.last_running_time.toFixed(2) }}s</span>
        <span v-else style="color: var(--el-text-color-placeholder)">-</span>
      </el-descriptions-item>
      <el-descriptions-item label="上次运行时间" :span="2">
        {{ formatTime(task.last_run_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="通知渠道" :span="2">
        <span v-if="task.notification_channel_name">
          {{ task.notification_channel_name }}
          <span v-if="task.notification_channel_enabled === false" style="color: var(--el-color-danger)">（已禁用）</span>
        </span>
        <span v-else style="color: var(--el-text-color-placeholder)">全部已启用渠道</span>
      </el-descriptions-item>
      <el-descriptions-item label="下次运行时间" :span="2">
        {{ formatTime(task.next_run_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="创建时间">{{ formatTime(task.created_at) }}</el-descriptions-item>
      <el-descriptions-item label="更新时间">{{ formatTime(task.updated_at) }}</el-descriptions-item>
      <el-descriptions-item label="备注" :span="2">
        <span v-if="task.remark">{{ task.remark }}</span>
        <span v-else style="color: var(--el-text-color-placeholder)">无</span>
      </el-descriptions-item>
    </el-descriptions>
  </el-dialog>
</template>
