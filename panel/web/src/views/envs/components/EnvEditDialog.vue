<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useResponsive } from '@/composables/useResponsive'

type EnvFormModel = {
  id: number
  name: string
  value: string
  remarks: string
  group?: string
  groups: string[]
}

const props = withDefaults(defineProps<{
  modelValue: boolean
  mode: 'create' | 'edit'
  initialData?: EnvFormModel | null
  groups?: string[]
}>(), {
  initialData: null,
  groups: () => []
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [value: EnvFormModel | EnvFormModel[]]
}>()

// 与后端 envNamePattern 对齐：字母/数字/下划线，且不能以数字开头
const ENV_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/

function splitEnvGroups(value: string): string[] {
  return value
    .split(/[,，;；\n\r\t]/)
    .map(group => group.trim())
    .filter((group, index, list) => group !== '' && list.indexOf(group) === index)
}

function normalizeGroupList(groups: string[]): string[] {
  return splitEnvGroups(groups.join(','))
}

function createEmptyForm(): EnvFormModel {
  return { id: 0, name: '', value: '', remarks: '', group: '', groups: [] }
}

const form = ref<EnvFormModel>(createEmptyForm())
const splitMode = ref(false)
const { dialogFullscreen } = useResponsive()

const isCreate = computed(() => props.mode === 'create')
const dialogTitle = computed(() => isCreate.value ? '新建环境变量' : '编辑环境变量')
const submitText = computed(() => isCreate.value ? '创建' : '保存')

// 已输入但不符合规则时，提示文字变红
const nameInvalid = computed(() => {
  const name = form.value.name.trim()
  return name !== '' && !ENV_NAME_PATTERN.test(name)
})

function syncForm() {
  const initial = props.initialData ?? createEmptyForm()
  const initialGroups = initial.groups?.length
    ? normalizeGroupList(initial.groups)
    : splitEnvGroups(initial.group || '')
  form.value = {
    ...createEmptyForm(),
    ...initial,
    group: initialGroups.join(','),
    groups: initialGroups
  }
  splitMode.value = false
}

function closeDialog() {
  emit('update:modelValue', false)
}

function handleSave() {
  const name = form.value.name.trim()
  const remarks = form.value.remarks.trim()
  const groups = normalizeGroupList(form.value.groups)
  const group = groups.join(',')

  if (!name) {
    ElMessage.warning('变量名不能为空')
    return
  }

  if (!ENV_NAME_PATTERN.test(name)) {
    ElMessage.warning('变量名只能包含字母、数字、下划线，且不能以数字开头')
    return
  }

  if (isCreate.value && splitMode.value) {
    const lines = form.value.value.split('\n').filter(line => line.trim() !== '')
    if (lines.length === 0) {
      ElMessage.warning('请输入至少一行变量值')
      return
    }
    const items: EnvFormModel[] = lines.map(line => ({
      id: 0,
      name,
      value: line.trim(),
      remarks,
      group,
      groups
    }))
    emit('save', items)
  } else {
    emit('save', {
      id: form.value.id,
      name,
      value: form.value.value,
      remarks,
      group,
      groups
    })
  }
}

watch(
  () => [props.modelValue, props.initialData, props.mode],
  ([visible]) => {
    if (visible) {
      syncForm()
    }
  },
  { immediate: true }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="dialogTitle"
    width="760px"
    top="8vh"
    class="env-edit-dialog"
    :fullscreen="dialogFullscreen"
    :close-on-click-modal="false"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form
      class="env-edit-dialog__form"
      :model="form"
      :label-width="dialogFullscreen ? 'auto' : '84px'"
      :label-position="dialogFullscreen ? 'top' : 'right'"
    >
      <el-form-item label="变量名">
        <el-input v-model="form.name" placeholder="变量名 (如: API_KEY)" />
        <div class="env-edit-dialog__name-hint" :class="{ 'is-error': nameInvalid }">
          只能输入字母、数字、下划线，且不能以数字开头
        </div>
      </el-form-item>
      <el-form-item v-if="isCreate" label="按行拆分">
        <div style="display: flex; align-items: center; gap: 8px; width: 100%">
          <el-switch v-model="splitMode" />
          <span style="font-size: 12px; color: var(--el-text-color-secondary)">
            {{ splitMode ? '每行创建一个变量' : '所有行作为一个变量值' }}
          </span>
        </div>
      </el-form-item>
      <el-form-item class="env-edit-dialog__value-item" label="值">
        <el-input
          v-model="form.value"
          type="textarea"
          :rows="isCreate ? 10 : 12"
          :placeholder="splitMode ? '每行一个值' : '变量值'"
        />
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="form.remarks" placeholder="备注说明" />
      </el-form-item>
      <el-form-item label="分组">
        <el-select
          v-model="form.groups"
          multiple
          filterable
          allow-create
          default-first-option
          collapse-tags
          collapse-tags-tooltip
          clearable
          placeholder="可选择多个分组，也可直接输入新分组"
          style="width: 100%"
        >
          <el-option v-for="group in groups" :key="group" :label="group" :value="group" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="closeDialog">取消</el-button>
      <el-button type="primary" @click="handleSave">{{ submitText }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
:deep(.env-edit-dialog) {
  max-width: calc(100vw - 48px);
}

:deep(.env-edit-dialog .el-dialog__header) {
  padding: 20px 24px 14px;
  margin-right: 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

:deep(.env-edit-dialog .el-dialog__body) {
  max-height: calc(78vh - 128px);
  padding: 0 24px;
  overflow-y: auto;
}

:deep(.env-edit-dialog .el-dialog__footer) {
  padding: 14px 24px 18px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.env-edit-dialog__form {
  padding: 18px 0 20px;
}

.env-edit-dialog__form :deep(.el-form-item__label) {
  white-space: nowrap;
  word-break: keep-all;
}

.env-edit-dialog__name-hint {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}

.env-edit-dialog__name-hint.is-error {
  color: var(--el-color-danger);
}

.env-edit-dialog__value-item :deep(.el-form-item__label) {
  align-self: flex-start;
}

.env-edit-dialog__value-item :deep(.el-textarea__inner) {
  min-height: 280px;
  max-height: 42vh;
  overflow: auto;
  resize: vertical;
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
}

@media (max-width: 768px) {
  :deep(.env-edit-dialog) {
    max-width: 100vw;
  }

  :deep(.env-edit-dialog .el-dialog__header) {
    padding: 16px 18px 12px;
  }

  :deep(.env-edit-dialog .el-dialog__body) {
    max-height: none;
    padding: 0 18px;
  }

  :deep(.env-edit-dialog .el-dialog__footer) {
    padding: 12px 18px 16px;
  }

  .env-edit-dialog__form {
    padding: 14px 0 18px;
  }

  .env-edit-dialog__value-item :deep(.el-textarea__inner) {
    min-height: 45vh;
    max-height: none;
  }
}
</style>
