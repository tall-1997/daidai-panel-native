<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useResponsive } from '@/composables/useResponsive'

const props = withDefaults(defineProps<{
  modelValue: boolean
  // 提交在途标记：父组件导入请求期间置位，防止弹窗还开着时连点重复导入
  submitting?: boolean
}>(), {
  submitting: false
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  import: [payload: { envs: any[]; mode: string }]
}>()

const importText = ref('')
// #161：默认按「名称 + 值」合并，备注相同的不同值不会再被合成一条
const importMode = ref('merge_value')
const { dialogFullscreen } = useResponsive()

function closeDialog() {
  emit('update:modelValue', false)
}

function handleImport() {
  // 上一发还没回来就不再叠加请求
  if (props.submitting) {
    return
  }

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
      importMode.value = 'merge_value'
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
        <el-radio-group v-model="importMode" class="env-import-dialog__modes">
          <el-radio value="merge_value">按名称 + 值合并（推荐）：名称和值都相同的视为同一条，其余新增</el-radio>
          <el-radio value="merge">按名称 + 备注合并：名称和备注都相同就覆盖值，适合用导入刷新 Token</el-radio>
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
      <el-button type="primary" :loading="submitting" :disabled="submitting" @click="handleImport">导入</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.env-import-dialog__form :deep(.el-form-item__label) {
  white-space: nowrap;
  word-break: keep-all;
}

/* 模式文案较长：每个选项单独一行，超宽时换行 */
.env-import-dialog__modes {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}

.env-import-dialog__modes :deep(.el-radio) {
  height: auto;
  margin-right: 0;
  padding: 4px 0;
  white-space: normal;
  line-height: 1.5;
}

.env-import-dialog__json-item :deep(.el-form-item__label) {
  align-self: flex-start;
}
</style>
