<template>
  <el-dropdown
    v-if="visibleItems.length > 0"
    class="dd-mobile-card__more-wrap"
    trigger="click"
    placement="bottom-end"
    popper-class="dd-split-button__popper"
    :disabled="disabled"
    @command="onCommand"
  >
    <button
      type="button"
      class="dd-mobile-card__more"
      :aria-label="ariaLabel"
      :disabled="disabled"
    >
      <el-icon><MoreFilled /></el-icon>
    </button>

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
// 移动端卡片右上角的「···」更多菜单（v3.3.1，issue #143）。用法：
//   <div class="dd-mobile-card__head"> 复选框 / 名称 / 标签 … <DdMoreMenu :items="xxxActionItems(row)" @command="onXxxAction(row, $event)" /></div>
// 与 DdSplitButton 共用同一个 popper-class 和同一种菜单项结构：
//   - 菜单字色加深、danger 项红字、success 项（如「启用」）hover 变绿，都由 global.scss 里
//     .dd-split-button__popper 那几条全局规则给，桌面操作列与移动端卡片两边观感一致；
//   - 页面直接把桌面 Split Button 用的那份 items 数组传进来即可，visible / disabled / divided 语义不变。
// 一项都不可见时整个不渲染：空菜单点开只有一块空白浮层，看起来像坏了。
// 根元素挂 dd-mobile-card__more-wrap（margin-left:auto），放在 .dd-mobile-card__head 的最后就会贴到右侧。
// 触发器用原生 button：键盘可达、读屏能念出 aria-label；type="button" 防止放进表单时被当成提交按钮。
// 样式（32×32、只换字色、focus 描边、贴近卡角）都在 global.scss 的 .dd-mobile-card__more，本组件不写 scoped 样式：
// 菜单浮层 teleport 到 body，本来就只能靠全局规则。

import { computed } from 'vue'
// 图标显式局部引入，不依赖 main.ts 的全局注册（本期约定不改 main.ts）
import { MoreFilled } from '@element-plus/icons-vue'
import type { SplitButtonItem } from './DdSplitButton.vue'

const props = withDefaults(
  defineProps<{
    /** 菜单项，结构与 DdSplitButton 完全相同，可直接复用桌面那份数组 */
    items: SplitButtonItem[]
    /** 触发器只有图标，没有可读文字，读屏靠它念出按钮用途 */
    ariaLabel?: string
    disabled?: boolean
  }>(),
  {
    ariaLabel: '更多操作',
    disabled: false,
  }
)

const emit = defineEmits<{
  /** 从菜单里选了一项，参数是该项的 key */
  (e: 'command', key: string): void
}>()

const visibleItems = computed(() => props.items.filter((item) => item.visible !== false))

function onCommand(key: string | number | object) {
  emit('command', String(key))
}
</script>
