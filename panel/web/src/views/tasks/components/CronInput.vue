<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { taskApi } from '@/api/task'
import { useResponsive } from '@/composables/useResponsive'

type CronRuleState = {
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

const templates = ref<any[]>([])
const rules = ref<CronRuleState[]>([])
const showAllTemplates = ref(false)
const activeTemplateRuleIndex = ref(0)
const { dialogFullscreen } = useResponsive()

let nextRuleId = 1

function createRule(expression = ''): CronRuleState {
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
    : [createRule('0 0 * * *')]

  rules.value.forEach((_, index) => {
    void parseRule(index)
  })
}

watch(
  () => props.modelValue,
  (value) => {
    const incoming = joinExpressions(splitExpressions(value))
    const current = joinExpressions(rules.value.map(rule => rule.expression))
    if (incoming === current) {
      return
    }
    syncRulesFromModel(value)
  },
  { immediate: true }
)

async function loadTemplates() {
  if (templates.value.length > 0) {
    return
  }
  try {
    const res = await taskApi.cronTemplates()
    templates.value = res || []
  } catch {
    templates.value = []
  }
}

void loadTemplates()

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
  rules.value.splice(afterIndex + 1, 0, createRule('0 0 * * *'))
  emitRules()
  void parseRule(afterIndex + 1)
}

function removeRule(index: number) {
  if (rules.value.length <= 1) {
    return
  }
  rules.value.splice(index, 1)
  emitRules()
}

function openTemplateDialog(index: number) {
  activeTemplateRuleIndex.value = index
  showAllTemplates.value = true
}

function selectTemplate(tmpl: any) {
  const rule = rules.value[activeTemplateRuleIndex.value]
  if (!rule) {
    return
  }
  rule.expression = tmpl.expression
  emitRules()
  void parseRule(activeTemplateRuleIndex.value)
  showAllTemplates.value = false
}

function handleKeyDown(event: KeyboardEvent) {
  if (event.key === ' ') {
    event.stopPropagation()
  }
}

const groupedTemplates = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const template of templates.value) {
    if (!groups[template.category]) {
      groups[template.category] = []
    }
    groups[template.category]!.push(template)
  }
  return groups
})
</script>

<template>
  <div class="cron-input">
    <div
      v-if="rules[0]"
      :key="rules[0].id"
      class="cron-rule-block"
    >
      <div class="cron-rule-row">
        <el-input
          v-model="rules[0].expression"
          placeholder="cron 表达式 (秒 分 时 日 月 周, 如: 0 */5 * * * *)"
          clearable
          @keydown="handleKeyDown"
          @input="handleRuleInput(0)"
        />
        <div class="cron-rule-actions">
          <el-button class="cron-add-btn" size="small" @click="addRule(0)">
            增加定时规则
          </el-button>
        </div>
      </div>
    </div>

    <div
      v-for="(rule, extraIndex) in rules.slice(1)"
      :key="rule.id"
      class="cron-rule-block"
    >
      <div class="cron-rule-row">
        <el-input
          v-model="rule.expression"
          placeholder="cron 表达式 (秒 分 时 日 月 周, 如: 0 */5 * * * *)"
          clearable
          @keydown="handleKeyDown"
          @input="handleRuleInput(extraIndex + 1)"
        />
        <div class="cron-rule-actions">
          <el-button
            text
            size="small"
            class="cron-remove-btn"
            @click="removeRule(extraIndex + 1)"
          >
            删除
          </el-button>
        </div>
      </div>
    </div>

    <div v-if="rules[0]?.parseResult" class="cron-info">
      <template v-if="rules[0].parseResult.is_valid">
        <div class="cron-meta">
          <div class="valid-badge">
            <el-icon class="badge-icon"><CircleCheck /></el-icon>
            <span class="badge-text">{{ rules[0].parseResult.description }}</span>
          </div>
          <div v-if="rules[0].parseResult.next_run_times?.length" class="next-times">
            <el-icon class="time-icon"><Clock /></el-icon>
            <span class="label">下次执行</span>
            <span class="time-value">{{ new Date(rules[0].parseResult.next_run_times[0]).toLocaleString() }}</span>
          </div>
          <el-button text size="small" @click="openTemplateDialog(0)">常用规则</el-button>
        </div>
      </template>
      <div v-else class="cron-meta">
        <div class="error-badge">
          <el-icon class="badge-icon"><CircleClose /></el-icon>
          <span class="badge-text">{{ rules[0].parseResult.error }}</span>
        </div>
        <el-button text size="small" @click="openTemplateDialog(0)">常用规则</el-button>
      </div>
    </div>

    <el-dialog
      v-model="showAllTemplates"
      title="选择定时规则"
      width="700px"
      :fullscreen="dialogFullscreen"
      :lock-scroll="false"
      :close-on-click-modal="false"
    >
      <div class="cron-templates-dialog">
        <div v-for="(items, category) in groupedTemplates" :key="category" class="template-group">
          <div class="group-header">
            <div class="group-title">{{ category }}</div>
            <div class="group-count">{{ items.length }} 个规则</div>
          </div>
          <div class="group-items">
            <div
              v-for="template in items"
              :key="`${category}-${template.expression}`"
              class="template-item"
              :class="{ active: rules[activeTemplateRuleIndex]?.expression === template.expression }"
              @click="selectTemplate(template)"
            >
              <div class="item-header">
                <span class="item-name">{{ template.name }}</span>
                <el-icon
                  v-if="rules[activeTemplateRuleIndex]?.expression === template.expression"
                  class="check-icon"
                >
                  <Check />
                </el-icon>
              </div>
              <div class="item-expr">{{ template.expression }}</div>
            </div>
          </div>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.cron-input {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cron-rule-block {
  padding: 0;
}

.cron-rule-row {
  display: flex;
  gap: 10px;
  align-items: stretch;
}

.cron-rule-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.cron-add-btn {
  flex-shrink: 0;
}

.cron-remove-btn {
  padding-left: 0;
  padding-right: 0;
}

.cron-info {
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  font-size: 13px;
}

.cron-meta {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  flex-wrap: wrap;
}

.valid-badge {
  display: inline-flex;
  width: fit-content;
  align-self: flex-start;
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
  align-self: flex-start;
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
  background: linear-gradient(135deg, var(--el-color-primary-light-9) 0%, var(--el-color-primary-light-8) 100%);
  border-radius: 14px;
  color: var(--el-color-primary);
  font-weight: 500;
  border: 1px solid var(--el-color-primary-light-7);

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

.cron-templates-dialog {
  max-height: 65vh;
  overflow-y: auto;
  padding: 4px;
}

.template-group {
  margin-bottom: 24px;

  &:last-child {
    margin-bottom: 0;
  }
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  padding: 10px 12px;
  background: linear-gradient(135deg, var(--el-fill-color-light) 0%, var(--el-fill-color) 100%);
  border-radius: 6px;
  border-left: 4px solid var(--el-color-primary);
}

.group-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.group-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-bg-color);
  padding: 2px 8px;
  border-radius: 10px;
}

.group-items {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  gap: 12px;
}

.template-item {
  padding: 12px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  background: var(--el-bg-color);
  cursor: pointer;
  transition: all 0.2s ease;

  &:hover {
    border-color: var(--el-color-primary-light-5);
    box-shadow: 0 6px 14px rgba(64, 158, 255, 0.12);
  }

  &.active {
    border-color: var(--el-color-primary);
    background: linear-gradient(135deg, var(--el-color-primary-light-9), var(--el-bg-color));
  }
}

.item-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.item-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.check-icon {
  color: var(--el-color-primary);
}

.item-expr {
  font-family: var(--dd-font-mono);
  font-size: 12px;
  color: var(--el-text-color-secondary);
  word-break: break-all;
}

@media (max-width: 768px) {
  .cron-rule-row {
    flex-direction: column;
  }

  .cron-rule-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .cron-add-btn {
    width: 100%;
  }

  .group-items {
    grid-template-columns: 1fr;
  }
}
</style>
