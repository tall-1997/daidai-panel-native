<script setup lang="ts">
import { Clock, Document } from '@element-plus/icons-vue'
import type { SettingsConfigForm } from '../types'

defineProps<{
  configsLoading: boolean
  configsSaving: boolean
  form: SettingsConfigForm
  onSave: () => void
}>()
</script>

<template>
  <el-card shadow="never" v-loading="configsLoading">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Clock /></el-icon> 任务执行</span>
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
    <div class="switch-row">
      <div class="switch-item">
        <span class="switch-label">自动安装缺失依赖</span>
        <el-switch v-model="form.auto_install_deps" inline-prompt active-text="开" inactive-text="关" />
      </div>
    </div>
    <span class="form-hint">脚本运行失败且检测到缺失依赖时，自动尝试安装后重试</span>
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
