<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
  version: string;
  lastCheckTime: string;
  autoUpdateEnabled: boolean;
}>();

const emit = defineEmits<{
  "update:autoUpdateEnabled": [value: boolean];
}>();

const lastCheckDisplay = computed(() => {
  if (!props.lastCheckTime) return "从未检查";
  return new Date(props.lastCheckTime).toLocaleString();
});

const nextCheckDisplay = computed(() => {
  if (!props.lastCheckTime) return "-";
  const next = new Date(
    new Date(props.lastCheckTime).getTime() + 24 * 60 * 60 * 1000,
  );
  return next.toLocaleString();
});

const statusText = computed(() => {
  return props.autoUpdateEnabled ? "系统已是最新版本" : "未开启自动检查";
});
</script>

<template>
  <el-card shadow="never" class="usc">
    <div class="usc-layout">
      <div class="usc-header">
        <span class="usc-title">系统更新设置</span>
        <span class="usc-subtitle"
          >保持系统为最新版本以获得更好的稳定性和性能</span
        >
      </div>

      <div class="usc-switch-card">
        <div class="usc-switch-icon-wrap">
          <svg
            class="usc-switch-svg"
            viewBox="0 0 24 24"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              d="M12 4V2M12 4C7.58 4 4 7.58 4 12H2M12 4C16.42 4 20 7.58 20 12H22M12 22V20M12 20C16.42 20 20 16.42 20 12M12 20C7.58 20 4 16.42 4 12"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            />
            <path
              d="M15 9L9 15M9 9L15 15"
              stroke="currentColor"
              stroke-width="0"
            />
            <path
              d="M12 8V12L14 14"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </div>
        <div class="usc-switch-body">
          <div class="usc-switch-title">自动更新</div>
          <div class="usc-switch-desc">
            每 24
            小时自动检查一次新版本，检测到新版本后会在空闲时段尝试更新（不下载生效无变化）。
          </div>
        </div>
        <el-switch
          :model-value="autoUpdateEnabled"
          inline-prompt
          active-text="开"
          inactive-text="关"
          @change="(val: boolean) => emit('update:autoUpdateEnabled', val)"
        />
      </div>

      <div class="usc-footer">
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--blue"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">最后检查时间</span>
            <span class="usc-footer-value">{{ lastCheckDisplay }}</span>
          </div>
        </div>
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--green"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">当前状态</span>
            <span class="usc-footer-value usc-footer-value--status">{{
              statusText
            }}</span>
          </div>
        </div>
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--cyan"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">下次检查时间</span>
            <span class="usc-footer-value">{{ nextCheckDisplay }}</span>
          </div>
        </div>
      </div>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
.usc {
  border-radius: 14px;
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.04);
  height: 100%;

  :deep(.el-card__body) {
    padding: 0;
    height: 100%;
  }
}

.usc-layout {
  padding: 24px;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.usc-header {
  margin-bottom: 20px;
}

.usc-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  display: block;
  margin-bottom: 4px;
}

.usc-subtitle {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.usc-switch-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px;
  border-radius: 12px;
  background: linear-gradient(
    135deg,
    rgba(24, 144, 255, 0.07) 0%,
    rgba(54, 207, 201, 0.05) 100%
  );
  border: 1px solid rgba(59, 130, 246, 0.1);
  margin-bottom: 20px;
  flex: 1;
}

.usc-switch-icon-wrap {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: linear-gradient(135deg, #1890ff, #36cfc9);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(24, 144, 255, 0.28);
}

.usc-switch-svg {
  width: 20px;
  height: 20px;
  color: #fff;
}

.usc-switch-body {
  flex: 1;
  min-width: 0;
}

.usc-switch-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 2px;
}

.usc-switch-desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.usc-footer {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: auto;
}

.usc-footer-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.usc-footer-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  margin-top: 4px;

  &--blue {
    background: #3b82f6;
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
  }
  &--green {
    background: #10b981;
    box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.15);
  }
  &--cyan {
    background: #36cfc9;
    box-shadow: 0 0 0 3px rgba(54, 207, 201, 0.18);
  }
}

.usc-footer-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.usc-footer-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.usc-footer-value {
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  font-family: var(--dd-font-mono, monospace);

  &--status {
    color: #10b981;
    font-family: var(--dd-font-ui), sans-serif;
    font-weight: 600;
  }
}

@media (max-width: 768px) {
  .usc-footer {
    grid-template-columns: 1fr;
    gap: 10px;
  }

  .usc-switch-card {
    flex-direction: column;
    align-items: stretch;
    text-align: center;
  }

  .usc-switch-icon-wrap {
    align-self: center;
  }
}
</style>
