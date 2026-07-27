<script setup lang="ts">
import { ref, watch } from 'vue'
import { useResponsive } from '@/composables/useResponsive'

const props = defineProps<{
  modelValue: boolean
  groups: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  confirm: [groups: string[]]
}>()

const batchGroups = ref<string[]>([])
const { dialogFullscreen } = useResponsive()

function splitEnvGroups(value: string): string[] {
  return value
    .split(/[,，;；\n\r\t]/)
    .map(group => group.trim())
    .filter((group, index, list) => group !== '' && list.indexOf(group) === index)
}

function normalizeGroupList(groups: string[]): string[] {
  return splitEnvGroups(groups.join(','))
}

function closeDialog() {
  emit('update:modelValue', false)
}

function applyBatchGroupName(group: string) {
  const next = new Set(batchGroups.value)
  if (next.has(group)) {
    next.delete(group)
  } else {
    next.add(group)
  }
  batchGroups.value = [...next]
}

function handleConfirm() {
  emit('confirm', normalizeGroupList(batchGroups.value))
}

watch(
  () => props.modelValue,
  (visible) => {
    if (!visible) {
      batchGroups.value = []
    }
  }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="批量设置分组"
    width="400px"
    :fullscreen="dialogFullscreen"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form :label-width="dialogFullscreen ? 'auto' : '80px'" :label-position="dialogFullscreen ? 'top' : 'right'">
      <el-form-item label="分组名称">
        <el-select
          v-model="batchGroups"
          multiple
          filterable
          allow-create
          default-first-option
          collapse-tags
          collapse-tags-tooltip
          clearable
          placeholder="选择多个分组，或直接输入新分组"
          style="width: 100%"
        >
          <el-option v-for="group in groups" :key="group" :label="group" :value="group" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="groups.length > 0" label="已有分组">
        <div class="batch-group-options">
          <el-tag
            v-for="group in groups"
            :key="group"
            class="batch-group-tag"
            :type="batchGroups.includes(group) ? 'primary' : 'info'"
            effect="plain"
            @click="applyBatchGroupName(group)"
          >
            {{ group }}
          </el-tag>
        </div>
      </el-form-item>
      <el-alert type="info" :closable="false" show-icon>
        确认后会覆盖选中变量的当前分组；留空将清除分组
      </el-alert>
    </el-form>
    <template #footer>
      <el-button @click="closeDialog">取消</el-button>
      <el-button type="primary" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.batch-group-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.batch-group-tag {
  cursor: pointer;
}
</style>
