<script setup lang="ts">
import { ref, watch } from 'vue'
import { useResponsive } from '@/composables/useResponsive'

const props = withDefaults(defineProps<{
  modelValue: boolean
  // 提交在途标记：父组件批量改名请求期间置位，防止连点/连按回车重复提交
  submitting?: boolean
}>(), {
  submitting: false
})

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
  // 输入框上还挂着回车提交，光靠按钮 loading 拦不住，这里再兜一道在途判断
  if (props.submitting) {
    return
  }
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
      <el-button type="primary" :loading="submitting" :disabled="submitting" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>
