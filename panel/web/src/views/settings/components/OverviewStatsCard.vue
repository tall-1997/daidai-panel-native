<script setup lang="ts">
import { InfoFilled } from '@element-plus/icons-vue'

defineProps<{
  systemStats: any
}>()
</script>

<template>
  <el-card shadow="never" class="stats-card" v-if="systemStats">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><InfoFilled /></el-icon> 系统概况</span>
      </div>
    </template>
    <div class="overview-stats-grid">
      <div class="os-item">
        <div class="os-label">任务总数</div>
        <div class="os-value">{{ systemStats.tasks?.total || 0 }}</div>
      </div>
      <div class="os-item">
        <div class="os-label">已启用</div>
        <div class="os-value color-success">{{ systemStats.tasks?.enabled || 0 }}</div>
      </div>
      <div class="os-item">
        <div class="os-label">运行中</div>
        <div class="os-value color-warning">{{ systemStats.tasks?.running || 0 }}</div>
      </div>
      <div class="os-item">
        <div class="os-label">执行日志</div>
        <div class="os-value">{{ systemStats.logs?.total || 0 }}</div>
      </div>
      <div class="os-item">
        <div class="os-label">成功率</div>
        <div class="os-value color-success">{{ (systemStats.logs?.success_rate || 0).toFixed(1) }}%</div>
      </div>
      <div class="os-item">
        <div class="os-label">脚本数</div>
        <div class="os-value">{{ systemStats.scripts?.total || 0 }}</div>
      </div>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;

.stats-card {
  // 圆角/阴影对齐设计令牌（原写死阴影在暗色下不会变深，统一为 --dd-shadow-card）
  border-radius: var(--dd-card-radius);
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: var(--dd-shadow-card);
  height: 100%;
}

.overview-stats-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  text-align: center;
  gap: 10px;
}

.os-item {
  padding: 18px 10px;
  border-radius: 12px;
  transition: background 0.18s, transform 0.18s;
  background: var(--el-fill-color-light);

  &:hover {
    background: color-mix(in srgb, var(--el-color-primary) 6%, var(--el-fill-color-light));
    transform: translateY(-1px);
  }
}

.os-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
  font-weight: 500;
}

.os-value {
  font-size: 24px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-family: 'Inter', var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  -webkit-font-smoothing: antialiased;
  letter-spacing: -0.01em;
}

.color-success {
  color: #10b981;
}

.color-warning {
  color: #f59e0b;
}

@media (max-width: 768px) {
  .overview-stats-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
