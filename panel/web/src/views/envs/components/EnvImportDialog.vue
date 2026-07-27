<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useResponsive } from '@/composables/useResponsive'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  import: [payload: { envs: any[]; mode: string }]
}>()

const importText = ref('')
const importMode = ref('merge')
const { dialogFullscreen } = useResponsive()

function closeDialog() {
  emit('update:modelValue', false)
}

function handleImport() {
  let envs: any[]
  try {
    envs = JSON.parse(importText.value)
    if (!Array.isArray(envs)) {
      throw new Error('invalid')
    }
  } catch {
    ElMessage.error('JSON 格式不正确，需要数组格式')
    return
  }

  emit('import', {
    envs,
    mode: importMode.value
  })
}

function handleImportFile(file: File) {
  const reader = new FileReader()
  reader.onload = (event) => {
    const text = event.target?.result as string
    try {
      const parsed = JSON.parse(text)
      importText.value = JSON.stringify(parsed, null, 2)
    } catch {
      importText.value = text
      ElMessage.warning('文件内容不是有效的 JSON，已填入原始内容')
    }
  }
  reader.readAsText(file)
  return false
}

watch(
  () => props.modelValue,
  (visible) => {
    if (!visible) {
      importText.value = ''
      importMode.value = 'merge'
    }
  }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="导入环境变量"
    width="600px"
    :fullscreen="dialogFullscreen"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form
      class="env-import-dialog__form"
      :label-width="dialogFullscreen ? 'auto' : '96px'"
      :label-position="dialogFullscreen ? 'top' : 'right'"
    >
      <el-form-item label="导入模式">
        <el-radio-group v-model="importMode">
          <el-radio value="merge">合并 (同名同值更新)</el-radio>
          <el-radio value="replace">替换 (清空后导入)</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item class="env-import-dialog__json-item" label="JSON 数据">
        <div style="width: 100%">
          <el-upload
            :show-file-list="false"
            :before-upload="handleImportFile"
            accept=".json"
            style="margin-bottom: 8px"
          >
            <el-button size="small"><el-icon><Upload /></el-icon>选择 JSON 文件</el-button>
          </el-upload>
          <el-input
            v-model="importText"
            type="textarea"
            :rows="10"
            placeholder='[{"name": "KEY", "value": "VALUE", "remarks": "", "groups": ["prod", "notify"]}]'
          />
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="closeDialog">取消</el-button>
      <el-button type="primary" @click="handleImport">导入</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.env-import-dialog__form :deep(.el-form-item__label) {
  white-space: nowrap;
  word-break: keep-all;
}

.env-import-dialog__json-item :deep(.el-form-item__label) {
  align-self: flex-start;
}
</style>
