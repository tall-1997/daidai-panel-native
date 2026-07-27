<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { taskApi } from '@/api/task'
import { useResponsive } from '@/composables/useResponsive'

const props = defineProps<{
  visible: boolean
  taskIds: number[]
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  success: []
}>()

const { dialogFullscreen } = useResponsive()
const labels = ref<string[]>([])
const submitting = ref(false)

watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      labels.value = []
    }
  }
)

function close() {
  emit('update:visible', false)
}

async function handleConfirm() {
  const cleaned = Array.from(
    new Set(labels.value.map(label => label.trim()).filter(label => label !== ''))
  )
  if (cleaned.length === 0) {
    ElMessage.warning('请输入至少一个标签')
    return
  }
  if (props.taskIds.length === 0) {
    ElMessage.warning('请先选择任务')
    return
  }
  submitting.value = true
  try {
    const res = await taskApi.batchAddLabels(props.taskIds, cleaned)
    ElMessage.success(res?.message || `成功为 ${res?.success_count ?? props.taskIds.length} 个任务添加标签`)
    emit('success')
    close()
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || '批量添加标签失败')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="批量添加标签"
    width="460px"
    :fullscreen="dialogFullscreen"
    :close-on-click-modal="false"
    destroy-on-close
    @update:model-value="emit('update:visible', $event)"
  >
    <div class="batch-add-label">
      <p class="batch-add-label__tip">
        将为选中的 {{ taskIds.length }} 个任务追加以下标签（保留原有标签，自动去重）。
      </p>
      <el-select
        v-model="labels"
        multiple
        filterable
        allow-create
        default-first-option
        :reserve-keyword="false"
        placeholder="输入标签后回车，可添加多个"
        style="width: 100%"
      />
    </div>
    <template #footer>
      <el-button @click="close">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.batch-add-label__tip {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>
