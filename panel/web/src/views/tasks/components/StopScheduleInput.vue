<script setup lang="ts">
import { ref, watch } from 'vue'
import { taskApi } from '@/api/task'

type StopRuleState = {
  id: number
  expression: string
  parseResult: any | null
}

const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const rules = ref<StopRuleState[]>([])

let nextRuleId = 1

function createRule(expression = ''): StopRuleState {
  return {
    id: nextRuleId++,
    expression,
    parseResult: null
  }
}

function splitExpressions(value: string) {
  return value
    .split(/\r?\n/)
    .map(item => item.trim())
    .filter(Boolean)
}

function joinExpressions(items: string[]) {
  return items
    .map(item => item.trim())
    .filter(Boolean)
    .join('\n')
}

function syncRulesFromModel(value: string) {
  const expressions = splitExpressions(value)
  rules.value = expressions.length > 0
    ? expressions.map(expression => createRule(expression))
    : [createRule('')]

  rules.value.forEach((_, index) => {
    void parseRule(index)
  })
}

watch(
  () => props.modelValue,
  (value) => {
    const incoming = joinExpressions(splitExpressions(value))
    const current = joinExpressions(rules.value.map(rule => rule.expression))
    if (incoming === current && rules.value.length > 0) {
      return
    }
    syncRulesFromModel(value)
  },
  { immediate: true }
)

function emitRules() {
  emit('update:modelValue', joinExpressions(rules.value.map(rule => rule.expression)))
}

async function parseRule(index: number) {
  const rule = rules.value[index]
  if (!rule) {
    return
  }

  const expression = rule.expression.trim()
  if (!expression) {
    rule.parseResult = null
    return
  }

  try {
    rule.parseResult = await taskApi.cronParse(expression)
  } catch {
    rule.parseResult = null
  }
}

function handleRuleInput(index: number) {
  emitRules()
  void parseRule(index)
}

function addRule(afterIndex = rules.value.length - 1) {
  rules.value.splice(afterIndex + 1, 0, createRule(''))
  emitRules()
}

function removeRule(index: number) {
  if (rules.value.length <= 1) {
    const first = rules.value[0]
    if (first) {
      first.expression = ''
      first.parseResult = null
    }
    emitRules()
    return
  }
  rules.value.splice(index, 1)
  emitRules()
}

function handleKeyDown(event: KeyboardEvent) {
  if (event.key === ' ') {
    event.stopPropagation()
  }
}
</script>

<template>
  <div class="stop-schedule-input">
    <div
      v-for="(rule, index) in rules"
      :key="rule.id"
      class="stop-rule-block"
    >
      <div class="stop-rule-row">
        <el-input
          v-model="rule.expression"
          placeholder="cron 表达式，留空不自动停止（如 0 12 * * *）"
          clearable
          @keydown="handleKeyDown"
          @input="handleRuleInput(index)"
        />
        <div class="stop-rule-actions">
          <el-button
            v-if="index === 0"
            class="stop-add-btn"
            size="small"
            @click="addRule(index)"
          >
            增加停止规则
          </el-button>
          <el-button
            v-else
            text
            size="small"
            class="stop-remove-btn"
            @click="removeRule(index)"
          >
            删除
          </el-button>
        </div>
      </div>
      <div v-if="rule.parseResult" class="stop-rule-info">
        <template v-if="rule.parseResult.is_valid">
          <div class="stop-meta">
            <div class="valid-badge">
              <el-icon class="badge-icon"><CircleCheck /></el-icon>
              <span class="badge-text">{{ rule.parseResult.description }}</span>
            </div>
            <div v-if="rule.parseResult.next_run_times?.length" class="next-times">
              <el-icon class="time-icon"><Clock /></el-icon>
              <span class="label">下次停止</span>
              <span class="time-value">{{ new Date(rule.parseResult.next_run_times[0]).toLocaleString() }}</span>
            </div>
          </div>
        </template>
        <div v-else class="stop-meta">
          <div class="error-badge">
            <el-icon class="badge-icon"><CircleClose /></el-icon>
            <span class="badge-text">{{ rule.parseResult.error }}</span>
          </div>
        </div>
      </div>
    </div>
    <div class="stop-schedule-hint">
      到达设定时间后自动停止正在运行的任务，适合需要在特定时段运行的长驻任务。
    </div>
  </div>
</template>

<style scoped lang="scss">
.stop-schedule-input {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.stop-rule-block {
  padding: 0;
}

.stop-rule-row {
  display: flex;
  gap: 10px;
  align-items: stretch;
}

.stop-rule-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.stop-add-btn {
  flex-shrink: 0;
}

.stop-remove-btn {
  padding-left: 0;
  padding-right: 0;
}

.stop-rule-info {
  margin-top: 6px;
}

.stop-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.stop-schedule-hint {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.valid-badge {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  background: linear-gradient(135deg, #67c23a 0%, #85ce61 100%);
  border-radius: 14px;
  color: #fff;
  font-weight: 500;
  box-shadow: 0 2px 6px rgba(103, 194, 58, 0.25);

  .badge-icon {
    font-size: 14px;
  }

  .badge-text {
    font-size: 12px;
  }
}

.error-badge {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  background: linear-gradient(135deg, #f56c6c 0%, #f78989 100%);
  border-radius: 14px;
  color: #fff;
  font-weight: 500;
  box-shadow: 0 2px 6px rgba(245, 108, 108, 0.25);

  .badge-icon {
    font-size: 14px;
  }

  .badge-text {
    font-size: 12px;
  }
}

.next-times {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  background: linear-gradient(135deg, var(--el-color-warning-light-9) 0%, var(--el-color-warning-light-8) 100%);
  border-radius: 14px;
  color: var(--el-color-warning);
  font-weight: 500;
  border: 1px solid var(--el-color-warning-light-7);

  .time-icon {
    font-size: 13px;
  }

  .label {
    font-size: 11px;
  }

  .time-value {
    font-family: var(--dd-font-mono);
    font-size: 11px;
  }
}

@media (max-width: 768px) {
  .stop-rule-row {
    flex-direction: column;
  }

  .stop-rule-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .stop-add-btn {
    width: 100%;
  }
}
</style>
