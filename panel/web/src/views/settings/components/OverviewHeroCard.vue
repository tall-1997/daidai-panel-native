<script setup lang="ts">
import type { PanelUpdateStatus } from '@/api/system'
import UpdateProgressDialog from './UpdateProgressDialog.vue'

defineProps<{
  isAdmin: boolean
  currentVersion: string
  updateInfo: any
  updateStatus: PanelUpdateStatus | null
  checkingUpdate: boolean
  updatingPanel: boolean
  autoUpdateEnabled: boolean
  savingAutoUpdate: boolean
  releaseNotesVisible: boolean
  updateProgressVisible: boolean
  updateProgressStatus: 'idle' | 'running' | 'restarting' | 'failed' | 'timeout'
  updateProgressError: string
  onCheckUpdate: () => void | Promise<void>
  onStartUpdate: () => void | Promise<void>
  onRestartPanel: () => void | Promise<void>
  onToggleAutoUpdate: (value: boolean) => void | Promise<void>
  onOpenReleaseNotes: () => void | Promise<void>
  onCloseReleaseNotes: () => void | Promise<void>
  onOpenGitHub: () => void
  onCloseUpdateProgress: () => void | Promise<void>
}>()
</script>

<template>
  <el-card shadow="never" class="hero-card">
    <div class="hero-layout">
      <div class="hero-section-title">产品与版本</div>

      <div class="hero-center">
        <div class="hero-logo">
          <img src="/favicon.svg" alt="呆呆面板" class="hero-logo-img" />
        </div>
        <h2 class="hero-name">呆呆面板</h2>
        <p class="hero-desc">轻量级定时任务管理面板</p>

        <div class="hero-version-row">
          <div class="hero-version-item">
            <span class="hero-version-label">当前版本</span>
            <span class="hero-version-value">v{{ currentVersion }}</span>
          </div>
          <div class="hero-version-item">
            <span class="hero-version-label">技术栈</span>
            <span class="hero-version-value hero-version-value--tech">Gin + Vue3</span>
          </div>
        </div>

        <div class="hero-actions">
          <el-button v-if="isAdmin" type="primary" round :loading="checkingUpdate" @click="onCheckUpdate" class="hero-btn hero-btn--primary">
            检查系统更新
          </el-button>
          <el-button v-if="isAdmin" type="warning" round @click="onRestartPanel" class="hero-btn hero-btn--warning">
            重启面板
          </el-button>
          <el-button round @click="onOpenGitHub" class="hero-btn hero-btn--ghost">
            访问 GitHub
          </el-button>
        </div>
      </div>

      <div v-if="updateInfo" class="hero-alert">
        <el-alert
          :type="updateInfo.has_update ? (updateInfo.auto_update_supported ? 'success' : 'warning') : 'info'"
          :title="updateInfo.has_update ? `发现新版本 v${updateInfo.latest}` : '当前已是最新版本'"
          :closable="false"
        >
          <div v-if="updateInfo.has_update">
            <p>发布时间: {{ new Date(updateInfo.published_at).toLocaleString() }}</p>
            <p v-if="updateInfo.update_target?.deployment_type === 'binary'" class="hero-meta">更新方式：二进制后台更新</p>
            <p v-if="updateInfo.update_target?.update_manager === 'watchtower' || updateInfo.update_target?.watchtower_managed" class="hero-meta">更新方式：Watchtower 托管更新</p>
            <p v-if="updateInfo.update_target?.asset_name" class="hero-meta">更新包：{{ updateInfo.update_target.asset_name }}</p>
            <p v-if="updateInfo.update_target?.install_dir" class="hero-meta">安装目录：{{ updateInfo.update_target.install_dir }}</p>
            <p v-if="updateInfo.update_target?.deployment_type !== 'binary' && updateInfo.update_target?.mirror_host" class="hero-meta">镜像源：{{ updateInfo.update_target.mirror_host }}</p>
            <p v-if="updateInfo.update_target?.deployment_type !== 'binary' && updateInfo.update_target?.channel" class="hero-meta">渠道：{{ updateInfo.update_target.channel === 'debian' ? 'Debian' : 'Latest (Alpine)' }}</p>
            <p v-if="updateInfo.update_target?.watchtower_schedule" class="hero-meta">Watchtower 调度：{{ updateInfo.update_target.watchtower_schedule }}</p>
            <p v-if="updateInfo.update_target?.update_manager === 'watchtower' || updateInfo.update_target?.watchtower_managed" class="hero-meta">
              当前部署由 Watchtower 负责自动更新；如已配置 HTTP API，可在这里手动触发一次检查。
            </p>
            <p v-if="!updateInfo.auto_update_supported" class="hero-meta">{{ updateInfo.update_disabled_reason || '当前部署暂不支持一键更新' }}</p>
            <p v-if="!updateInfo.auto_update_supported && updateInfo.update_target?.deployment_type !== 'binary'" class="hero-meta">
              推荐使用 Watchtower 托管自动更新；需要立即手动更新时，在宿主机执行 docker compose pull && docker compose up -d。
            </p>
            <div class="hero-alert-actions">
              <el-button v-if="isAdmin && updateInfo.auto_update_supported" type="primary" size="small" round :loading="updatingPanel" @click="onStartUpdate">
                {{ updateInfo.update_target?.update_manager === 'watchtower' || updateInfo.update_target?.watchtower_managed ? '触发 Watchtower 检查' : '立即更新' }}
              </el-button>
              <el-button size="small" round @click="onOpenReleaseNotes">查看更新日志</el-button>
            </div>
          </div>
        </el-alert>
      </div>

      <div v-if="updateStatus && updateStatus.status && updateStatus.status !== 'idle'" class="hero-alert">
        <el-alert
          :type="updateStatus.status === 'failed' ? 'error' : (updateStatus.status === 'restarting' ? 'success' : 'warning')"
          :title="updateStatus.status === 'failed' ? '更新失败' : (updateStatus.status === 'restarting' ? '正在切换到新版本' : '更新进行中')"
          :closable="false"
        >
          <p>{{ updateStatus.message }}</p>
        </el-alert>
      </div>
    </div>
  </el-card>

  <UpdateProgressDialog
    :visible="updateProgressVisible"
    :current-version="currentVersion"
    :latest-version="updateInfo?.latest"
    :release-url="updateInfo?.release_url"
    :status="updateProgressStatus"
    :update-status="updateStatus"
    :error-message="updateProgressError"
    :on-close="onCloseUpdateProgress"
  />

  <el-dialog :model-value="releaseNotesVisible" title="发现新版本" width="720px" append-to-body @close="onCloseReleaseNotes">
    <div v-if="updateInfo" class="release-notes-shell">
      <div class="release-notes-meta">
        <strong>版本：v{{ updateInfo.latest }}</strong>
        <span v-if="updateInfo.release_name">{{ updateInfo.release_name }}</span>
        <span v-if="updateInfo.published_at">发布时间：{{ new Date(updateInfo.published_at).toLocaleString() }}</span>
      </div>
      <pre class="release-notes-content">{{ updateInfo.release_notes || '当前版本未提供更新日志。' }}</pre>
    </div>
    <template #footer>
      <el-button @click="onCloseReleaseNotes">关闭</el-button>
      <el-button v-if="isAdmin && updateInfo?.auto_update_supported" type="primary" :loading="updatingPanel" @click="onStartUpdate">
        {{ updateInfo?.update_target?.update_manager === 'watchtower' || updateInfo?.update_target?.watchtower_managed ? '触发 Watchtower 检查' : '立即更新' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.hero-card {
  border-radius: 14px;
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.04);
  height: 100%;

  :deep(.el-card__body) { padding: 0; height: 100%; }
}

.hero-layout {
  padding: 24px;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.hero-section-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin-bottom: 20px;
}

.hero-center {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  flex: 1;
}

.hero-logo {
  width: 72px;
  height: 72px;
  border-radius: 20px;
  overflow: hidden;
  box-shadow: 0 6px 20px rgba(59, 130, 246, 0.15), 0 2px 6px rgba(0, 0, 0, 0.06);
  margin-bottom: 14px;
}

.hero-logo-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.hero-name {
  font-size: 20px;
  font-weight: 800;
  margin: 0 0 4px;
  letter-spacing: -0.01em;
}

.hero-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0 0 16px;
}

.hero-version-row {
  display: flex;
  gap: 32px;
  margin-bottom: 18px;
}

.hero-version-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
}

.hero-version-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.hero-version-value {
  font-size: 18px;
  font-weight: 700;
  color: var(--el-color-primary);
  font-family: 'Inter', var(--dd-font-ui), sans-serif;

  &--tech {
    font-size: 14px;
    font-weight: 500;
    color: var(--el-text-color-secondary);
  }
}

.hero-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: center;
}

.hero-btn {
  font-weight: 600 !important;
  font-size: 13px !important;
  padding: 8px 18px !important;
  border-radius: 20px !important;
}

.hero-btn-icon {
  margin-right: 4px;
}

.hero-btn--ghost {
  background: var(--el-fill-color-light) !important;
  border-color: var(--el-border-color-light) !important;
  color: var(--el-text-color-regular) !important;

  &:hover {
    background: var(--el-fill-color) !important;
    color: var(--el-text-color-primary) !important;
  }
}

.hero-alert {
  margin-top: 16px;
  width: 100%;
}

.hero-alert-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.hero-meta {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.release-notes-shell {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.release-notes-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
  color: var(--el-text-color-secondary);
}

.release-notes-content {
  margin: 0;
  max-height: 46vh;
  overflow: auto;
  padding: 16px;
  border-radius: 12px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.65;
  font-family: var(--dd-font-mono);
  font-size: 13px;
}

@media (max-width: 768px) {
  .hero-actions {
    width: 100%;
    flex-direction: column;
  }

  .hero-version-row {
    gap: 24px;
  }
}
</style>
