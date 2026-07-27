<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  expressions?: string[]
  compact?: boolean
  numbered?: boolean
}>(), {
  expressions: () => [],
  compact: false,
  numbered: true,
})

const normalizedExpressions = computed(() =>
  (props.expressions || [])
    .map(expression => String(expression || '').trim())
    .filter(Boolean)
)

const isMultiLine = computed(() => normalizedExpressions.value.length > 1)
</script>

<template>
  <div
    class="task-cron-list"
    :class="{
      'task-cron-list--compact': compact,
      'task-cron-list--multi': isMultiLine
    }"
  >
    <div
      v-for="(expression, index) in normalizedExpressions"
      :key="`${index}-${expression}`"
      class="task-cron-list__item"
    >
      <span
        v-if="numbered && isMultiLine"
        class="task-cron-list__index"
      >
        {{ index + 1 }}
      </span>
      <code class="task-cron-list__code">{{ expression }}</code>
    </div>
  </div>
</template>

<style scoped lang="scss">
.task-cron-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.task-cron-list--compact {
  gap: 4px;
}

.task-cron-list__item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
}

.task-cron-list__index {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-top: 1px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--el-color-primary) 10%, white);
  color: var(--el-color-primary);
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
}

.task-cron-list__code {
  display: block;
  min-width: 0;
  width: 100%;
  padding: 6px 10px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--el-fill-color-light) 82%, white);
  border: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-secondary);
  font-family: var(--dd-font-mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}

.task-cron-list--compact .task-cron-list__code {
  padding: 4px 8px;
  font-size: 11px;
  line-height: 1.45;
  border-radius: 7px;
}

.task-cron-list--multi .task-cron-list__code {
  color: var(--el-text-color-primary);
}
</style>
