<script setup lang="ts">
import { ref, watch } from 'vue'
import { useResponsive } from '@/composables/useResponsive'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  confirm: [payload: { name: string }]
}>()

const newName = ref('')
const { dialogFullscreen } = useResponsive()

function closeDialog() {
  emit('update:modelValue', false)
}

function handleConfirm() {
  emit('confirm', { name: newName.value })
}

watch(
  () => props.modelValue,
  (visible) => {
    if (!visible) {
      newName.value = ''
    }
  }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="批量修改变量名"
    width="420px"
    :fullscreen="dialogFullscreen"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form :label-width="dialogFullscreen ? 'auto' : '80px'" :label-position="dialogFullscreen ? 'top' : 'right'">
      <el-form-item label="新变量名">
        <el-input
          v-model="newName"
          clearable
          placeholder="请输入新的变量名"
          @keyup.enter="handleConfirm"
        />
      </el-form-item>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="所有选中的环境变量将统一改为此名称，变量值和备注不会变化。"
      />
    </el-form>
    <template #footer>
      <el-button @click="closeDialog">取消</el-button>
      <el-button type="primary" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>
