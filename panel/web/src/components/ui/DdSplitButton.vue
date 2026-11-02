<template>
  <el-dropdown
    split-button
    :type="type"
    :size="size"
    :disabled="disabled"
    :placement="placement"
    trigger="click"
    :popper-class="['dd-split-button__popper', popperClass].filter(Boolean).join(' ')"
    @click="emit('click')"
    @command="onCommand"
  >
    <!-- 主体：点了就直接执行主操作，不展开菜单。
         「主体能直接执行操作，才叫 Split Button」——如果主体也只是展开菜单，
         那它就只是个带标签的下拉，不要用这个组件。 -->
    <el-icon v-if="icon" class="dd-split-button__icon"><component :is="icon" /></el-icon>
    <span>{{ label }}</span>

    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item
          v-for="item in visibleItems"
          :key="item.key"
          :command="item.key"
          :disabled="item.disabled"
          :divided="item.divided"
          :class="{
            'dd-split-button__item--danger': item.danger,
            'dd-split-button__item--success': item.success
          }"
        >
          <el-icon v-if="item.icon"><component :is="item.icon" /></el-icon>
          <span>{{ item.label }}</span>
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Component } from 'vue'

export interface SplitButtonItem {
  /** 派发给 @command 的标识 */
  key: string
  label: string
  icon?: Component
  disabled?: boolean
  /** 在这一项之上画一条分隔线。危险操作应当与常规操作分开 */
  divided?: boolean
  /** 危险操作（删除、卸载、清空…），文字标红 */
  danger?: boolean
  /**
   * 可撤销的正向操作（如任务的「启用」），只在 hover / focus 时显示绿色；
   * 常态沿用菜单统一字色，不像 danger 那样常驻着色（样式见 global.scss 的 --success）。
   * 与 danger 互斥，同一项不要两个都设。
   */
  success?: boolean
  /**
   * 是否显示。写成字段而不是让调用方在外面 filter，是为了让「运行 / 停止互斥」
   * 这类联动能直接写在同一份数组里，不至于出现「停止 ▾ 菜单里还挂着停止」。
   */
  visible?: boolean
}

const props = withDefaults(
  defineProps<{
    /** 主操作的文字 */
    label: string
    /** 主操作的图标 */
    icon?: Component
    items: SplitButtonItem[]
    type?: 'primary' | 'success' | 'warning' | 'danger' | 'info' | 'default'
    size?: 'large' | 'default' | 'small'
    disabled?: boolean
    /**
     * ⚠️ EP 在 dropdown.vue 里把 fallback-placements 硬编码成 ['bottom','top']，
     * 无法通过 props 覆盖。所以 bottom-end 只在【放得下】的时候生效；
     * 一旦竖直方向放不下触发 flip，就会退回居中的 bottom/top，右对齐随之丢失。
     * 表格最右列用本组件时，要靠「按钮组本身别顶到列边缘」兜底，别只指望这个值。
     */
    placement?: 'bottom-end' | 'bottom-start' | 'bottom' | 'top-end' | 'top-start' | 'top'
    popperClass?: string
  }>(),
  {
    type: 'primary',
    size: 'small',
    disabled: false,
    placement: 'bottom-end',
    popperClass: '',
  }
)

const emit = defineEmits<{
  /** 点了主体：直接执行主操作 */
  (e: 'click'): void
  /** 从菜单里选了一项 */
  (e: 'command', key: string): void
}>()

const visibleItems = computed(() => props.items.filter((item) => item.visible !== false))

function onCommand(key: string | number | object) {
  emit('command', String(key))
}
</script>

<style scoped lang="scss">
.dd-split-button__icon {
  margin-right: 4px;
}
</style>
