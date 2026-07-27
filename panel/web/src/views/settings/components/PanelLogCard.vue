<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { CopyDocument, Download, Refresh, Search, Tickets } from '@element-plus/icons-vue'
import { ansiToHtml, normalizeAnsi } from '@/utils/ansi'
import type { PanelLogLevel } from '../usePanelLogViewer'

const props = defineProps<{
  loading: boolean
  refreshing?: boolean
  lines: number
  keyword: string
  level: PanelLogLevel
  autoRefresh: boolean
  logs: string[]
  total: number
  lastLoadedAt: string
  byteSizeLabel: string
  activePreset: 'default' | 'updates' | 'errors' | ''
  onRefresh: () => void | Promise<void>
  onApplyUpdatePreset: () => void
  onApplyErrorPreset: () => void
  onResetFilters: () => void
  onCopy: () => void | Promise<void>
  onDownload: () => void
}>()

const emit = defineEmits<{
  'update:lines': [value: number]
  'update:keyword': [value: string]
  'update:level': [value: PanelLogLevel]
  'update:autoRefresh': [value: boolean]
}>()

const levelOptions: Array<{ label: string; value: PanelLogLevel }> = [
  { label: '全部级别', value: '' },
  { label: 'Debug 及以上', value: 'debug' },
  { label: 'Info 及以上', value: 'info' },
  { label: 'Warn 及以上', value: 'warn' },
  { label: '仅 Error', value: 'error' },
]

const lineOptions = [100, 200, 500, 1000]

const renderedHtml = computed(() => ansiToHtml(normalizeAnsi(props.logs.join('\n'))))

const logViewRef = ref<HTMLElement>()

watch(() => props.logs, () => {
  nextTick(() => {
    const el = logViewRef.value
    if (el) {
      el.scrollTop = el.scrollHeight
    }
  })
}, { flush: 'post' })
</script>

<template>
  <el-card shadow="never" class="panel-log-card" v-loading="loading">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Tickets /></el-icon> 面板日志</span>
      <div class="panel-log-card__meta">
          <span>共 {{ total }} 行</span>
          <span>{{ byteSizeLabel }}</span>
          <span v-if="lastLoadedAt">更新于 {{ lastLoadedAt }}</span>
          <span v-if="refreshing" class="panel-log-card__badge">同步中</span>
        </div>
      </div>
    </template>

    <div class="panel-log-toolbar">
      <el-input
        :model-value="keyword"
        placeholder="搜索关键词..."
        clearable
        class="panel-log-toolbar__search"
        :prefix-icon="Search"
        @update:model-value="emit('update:keyword', String($event || ''))"
      />

      <el-select
        :model-value="level"
        class="panel-log-toolbar__select"
        @update:model-value="emit('update:level', $event as PanelLogLevel)"
      >
        <el-option
          v-for="option in levelOptions"
          :key="option.value || 'all'"
          :label="option.label"
          :value="option.value"
        />
      </el-select>

      <el-select
        :model-value="lines"
        class="panel-log-toolbar__select panel-log-toolbar__select--lines"
        @update:model-value="emit('update:lines', Number($event))"
      >
        <el-option v-for="count in lineOptions" :key="count" :label="`最近 ${count} 行`" :value="count" />
      </el-select>

      <div class="panel-log-toolbar__actions">
        <el-switch
          :model-value="autoRefresh"
          inline-prompt
          active-text="自动刷新"
          inactive-text="手动"
          @update:model-value="emit('update:autoRefresh', Boolean($event))"
        />
        <div class="panel-log-toolbar__presets">
          <el-button
            size="small"
            :type="activePreset === 'updates' ? 'primary' : 'default'"
            plain
            @click="onApplyUpdatePreset"
          >
            只看更新日志
          </el-button>
          <el-button
            size="small"
            :type="activePreset === 'errors' ? 'danger' : 'default'"
            plain
            @click="onApplyErrorPreset"
          >
            只看错误日志
          </el-button>
          <el-button
            size="small"
            :type="activePreset === 'default' ? 'success' : 'default'"
            plain
            @click="onResetFilters"
          >
            恢复默认视图
          </el-button>
        </div>
        <el-button @click="onRefresh">
          <el-icon><Refresh /></el-icon>刷新
        </el-button>
        <el-button :disabled="logs.length === 0" @click="onCopy">
          <el-icon><CopyDocument /></el-icon>复制
        </el-button>
        <el-button :disabled="logs.length === 0" @click="onDownload">
          <el-icon><Download /></el-icon>下载
        </el-button>
      </div>
    </div>

    <div ref="logViewRef" class="panel-log-view dd-log-surface">
      <div v-if="logs.length === 0" class="panel-log-empty">
        当前筛选条件下暂无日志
      </div>
      <pre v-else class="panel-log-pre" v-html="renderedHtml"></pre>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;

.panel-log-card {
  border-radius: 14px;
  border: 1px solid var(--el-border-color-lighter);
}

.panel-log-card__meta {
  display: inline-flex;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.panel-log-card__badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
}

.panel-log-toolbar {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) 180px 140px auto;
  gap: 12px;
  margin-bottom: 16px;
}

.panel-log-toolbar__search {
  min-width: 0;
}

.panel-log-toolbar__select {
  width: 100%;
}

.panel-log-toolbar__actions {
  padding: 4px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--el-fill-color-light) 84%, transparent);
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.panel-log-toolbar__presets {
  display: inline-flex;
  gap: 8px;
  flex-wrap: wrap;
}

.panel-log-view {
  min-height: 540px;
  max-height: 76vh;
  overflow: auto;
  padding: 18px 20px;
}

.panel-log-pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.7;
}

.panel-log-empty {
  min-height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: color-mix(in srgb, var(--dd-log-text-color) 68%, transparent);
  font-size: 13px;
}

@media (max-width: 768px) {
  .panel-log-toolbar {
    grid-template-columns: 1fr;
  }

  .panel-log-toolbar__actions {
    justify-content: stretch;
  }

  .panel-log-view {
    min-height: 360px;
    max-height: calc(100dvh - 280px);
    padding: 14px;
  }

  .panel-log-card__meta {
    gap: 8px;
  }
}
</style>
