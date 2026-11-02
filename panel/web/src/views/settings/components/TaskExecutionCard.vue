<script setup lang="ts">
import { computed } from 'vue'
import { Clock, Document } from '@element-plus/icons-vue'
import type { SettingsConfigForm } from '../types'
import { formatConfigRangeHint, type ParsedSystemConfigItem } from '../systemConfigSchema'

const props = defineProps<{
  configsLoading: boolean
  configsSaving: boolean
  form: SettingsConfigForm
  /** 全部注册项的服务端 schema，按 key 索引（useSettingsConfig 的 configSchema） */
  configSchema: Record<string, ParsedSystemConfigItem>
  onSave: () => void
}>()

// 这两项 v3.2.9 从「通用设置 → 其它配置项」兜底区挪进来：值走 form，
// 标题、说明、取值范围仍取服务端 schema，不在 Web 另抄一份；schema 还没加载到时整项不渲染（卡片此时在 loading）
const installTimeoutItem = computed(() => props.configSchema.dependency_install_timeout_minutes)
const silentExitItem = computed(() => props.configSchema.detect_silent_exit)
// v3.3.6（#159）新增的「任务结束后清理残留进程」直接放进本卡，写法同 detect_silent_exit：
// 标题、说明取服务端 schema（APP 设置页按同一份 schema 渲染这个开关），schema 还没加载到时整项不渲染
const leftoverCleanupItem = computed(() => props.configSchema.cleanup_leftover_processes)
</script>

<template>
  <el-card shadow="never" v-loading="configsLoading">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Clock /></el-icon> 任务运行</span>
        <el-button type="primary" :loading="configsSaving" @click="onSave">
          <el-icon><Document /></el-icon>保存配置
        </el-button>
      </div>
    </template>

    <div class="form-field">
      <label>定时任务并发数</label>
      <el-input v-model.number="form.max_concurrent_tasks" />
      <span class="form-hint">同时执行的最大任务数量</span>
    </div>
    <div class="form-field">
      <label>日志删除频率</label>
      <div class="compound-input">
        <span>每</span>
        <el-input v-model.number="form.log_retention_days" class="retention-input" />
        <span>天</span>
      </div>
      <span class="form-hint">日志清理接口默认保留最近多少天的数据</span>
    </div>
    <div class="form-field">
      <label>日志内容上限</label>
      <el-input v-model.number="form.max_log_content_size" />
      <span class="form-hint">单次任务在数据库中保留的日志字节数，默认 102400000</span>
    </div>
    <div v-if="installTimeoutItem" class="form-field">
      <label>{{ installTimeoutItem.label }}</label>
      <!-- 留空保存时服务端按默认值处理，占位符把默认值亮出来 -->
      <el-input v-model.number="form.dependency_install_timeout_minutes" :placeholder="installTimeoutItem.defaultValue" />
      <span v-if="installTimeoutItem.description" class="form-hint">{{ installTimeoutItem.description }}</span>
      <span v-if="formatConfigRangeHint(installTimeoutItem)" class="form-hint">
        {{ formatConfigRangeHint(installTimeoutItem) }}
      </span>
    </div>
    <div class="form-field">
      <label>系统命令行超时(分钟)</label>
      <el-input v-model.number="form.console_timeout_minutes" />
      <span class="form-hint">依赖管理页「系统命令行」里单条命令的最长执行时间，取值 1-720</span>
    </div>
    <div class="form-field">
      <label>随机延迟最大秒数</label>
      <el-input v-model="form.random_delay" placeholder="如 300 表示 1~300 秒随机延迟" />
      <span class="form-hint">留空或 0 表示不延迟</span>
    </div>
    <div class="form-field">
      <label>延迟文件后缀</label>
      <el-input v-model="form.random_delay_extensions" placeholder="如 js py" />
      <span class="form-hint">空格分隔，留空表示全部任务；现在已接入真实执行逻辑</span>
    </div>
    <!-- 开关项与 MCP 卡同一写法（form-field 包开关 + 说明），两个开关上下排时间距才一致 -->
    <div class="form-field">
      <div class="switch-item">
        <span class="switch-label">自动安装缺失依赖</span>
        <el-switch v-model="form.auto_install_deps" inline-prompt active-text="开" inactive-text="关" />
      </div>
      <span class="form-hint">脚本运行失败且检测到缺失依赖时，自动尝试安装后重试</span>
    </div>
    <div v-if="silentExitItem" class="form-field">
      <div class="switch-item">
        <span class="switch-label">{{ silentExitItem.label }}</span>
        <el-switch v-model="form.detect_silent_exit" inline-prompt active-text="开" inactive-text="关" />
      </div>
      <span v-if="silentExitItem.description" class="form-hint">{{ silentExitItem.description }}</span>
    </div>
    <div v-if="leftoverCleanupItem" class="form-field">
      <div class="switch-item">
        <span class="switch-label">{{ leftoverCleanupItem.label }}</span>
        <el-switch v-model="form.cleanup_leftover_processes" inline-prompt active-text="开" inactive-text="关" />
      </div>
      <span v-if="leftoverCleanupItem.description" class="form-hint">{{ leftoverCleanupItem.description }}</span>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;

.retention-input {
  width: 120px;
}

@media (max-width: 768px) {
  .retention-input {
    width: 100%;
  }
}
</style>
