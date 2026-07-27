import { onBeforeUnmount, ref } from 'vue'
import { configApi, systemApi, type PanelUpdateStatus } from '@/api/system'
import { ElMessage, ElMessageBox } from 'element-plus'

type UpdateVisualStatus = 'idle' | 'running' | 'restarting' | 'failed' | 'timeout'

export function useSettingsOverview() {
  const systemInfo = ref<any>({})
  const systemStats = ref<any>(null)
  const currentVersion = ref('')
  const updateInfo = ref<any>(null)
  const updateStatus = ref<PanelUpdateStatus | null>(null)
  const checkingUpdate = ref(false)
  const updatingPanel = ref(false)
  const autoUpdateEnabled = ref(false)
  const savingAutoUpdate = ref(false)
  const lastCheckTime = ref('')
  const releaseNotesVisible = ref(false)
  const updateProgressVisible = ref(false)
  const updateProgressStatus = ref<UpdateVisualStatus>('idle')
  const updateProgressError = ref('')
  let updateStatusPollTimer: ReturnType<typeof setTimeout> | null = null
  let updateAvailabilityDelayTimer: ReturnType<typeof setTimeout> | null = null
  let updateAvailabilityTimer: ReturnType<typeof setTimeout> | null = null
  let restartDelayTimer: ReturnType<typeof setTimeout> | null = null
  let restartPollTimer: ReturnType<typeof setInterval> | null = null

  function formatBytes(bytes: number): string {
    if (!bytes) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i]
  }

  function getUsageClass(percent: number): string {
    if (!percent) return ''
    if (percent < 60) return 'usage-success'
    if (percent < 80) return 'usage-warning'
    return 'usage-danger'
  }

  async function loadSystemInfo() {
    try {
      const res = await systemApi.info()
      systemInfo.value = res.data || {}
    } catch {
      // ignore
    }
  }

  async function loadSystemStats() {
    try {
      const res = await systemApi.stats()
      systemStats.value = res.data || {}
    } catch {
      // ignore
    }
  }

  async function loadUpdatePreferences() {
    try {
      const res = await configApi.get('auto_update_enabled')
      const value = String(res.data?.value ?? res.data?.config?.value ?? 'false').trim().toLowerCase()
      autoUpdateEnabled.value = ['1', 'true', 'yes', 'on'].includes(value)
    } catch {
      autoUpdateEnabled.value = false
    }
    try {
      const res = await configApi.get('auto_update_last_checked_at')
      const raw = String(res.data?.value ?? res.data?.config?.value ?? '').trim()
      if (raw) lastCheckTime.value = raw
    } catch { /* ignore */ }
  }

  async function loadVersion() {
    try {
      const res = await systemApi.version()
      currentVersion.value = res.data.version || ''
    } catch {
      // ignore
    }
  }

  async function handleCheckUpdate() {
    checkingUpdate.value = true
    try {
      const res = await systemApi.checkUpdate()
      updateInfo.value = res.data
      const now = new Date().toISOString()
      lastCheckTime.value = now
      void configApi.set({ key: 'auto_update_last_checked_at', value: now }).catch(() => {})
      if (res.data.has_update) {
        releaseNotesVisible.value = true
        if (res.data.auto_update_supported) {
          ElMessage.success(`发现新版本 v${res.data.latest}`)
        } else {
          ElMessage.warning(res.data.update_disabled_reason || '当前部署暂不支持面板内一键更新')
        }
      } else {
        ElMessage.success(`当前版本 v${res.data.current} 已经是最新版了`)
      }
    } catch (err: any) {
      const msg = err?.response?.data?.error || '检查更新失败，请稍后重试'
      ElMessage.error(msg)
    } finally {
      checkingUpdate.value = false
    }
  }

  async function handleToggleAutoUpdate(value: boolean) {
    const previous = autoUpdateEnabled.value
    autoUpdateEnabled.value = value
    savingAutoUpdate.value = true
    try {
      await configApi.set({ key: 'auto_update_enabled', value: value ? 'true' : 'false' })
      ElMessage.success(value ? '静默更新已开启' : '静默更新已关闭')
      if (value) {
        void handleCheckUpdate()
      }
    } catch {
      autoUpdateEnabled.value = previous
      ElMessage.error('保存静默更新设置失败')
    } finally {
      savingAutoUpdate.value = false
    }
  }

  function closeReleaseNotes() {
    releaseNotesVisible.value = false
  }

  function openReleaseNotes() {
    if (updateInfo.value?.has_update) {
      releaseNotesVisible.value = true
    }
  }

  async function handleUpdatePanel() {
    if (updatingPanel.value) {
      ElMessage.warning('更新任务已经在进行中，请稍候')
      return
    }

    if (updateInfo.value?.auto_update_supported === false) {
      ElMessage.warning(updateInfo.value?.update_disabled_reason || '当前部署暂不支持面板内一键更新')
      return
    }

    try {
      const updateTarget = updateInfo.value?.update_target || {}
      const isBinaryUpdate = updateTarget.deployment_type === 'binary'
      const isWatchtowerManaged = updateTarget.update_manager === 'watchtower' || updateTarget.watchtower_managed === true
      const mirrorHost = updateTarget.mirror_host
      const pullImageName = updateTarget.pull_image_name
      const confirmMessage = isWatchtowerManaged
        ? buildWatchtowerUpdateConfirmMessage(updateTarget)
        : isBinaryUpdate
        ? buildBinaryUpdateConfirmMessage(updateTarget)
        : buildDockerUpdateConfirmMessage(mirrorHost, pullImageName)
      await ElMessageBox.confirm(
        confirmMessage,
        '立即更新',
        {
          confirmButtonText: '开始更新',
          cancelButtonText: '取消',
          type: 'warning'
        }
      )
    } catch (err: any) {
      if (err === 'cancel' || err?.toString?.() === 'cancel') {
        return
      }
      ElMessage.error(err?.message || '无法确认更新操作')
      return
    }

    updatingPanel.value = true
    openUpdateProgress({
      status: 'running',
      phase: 'preparing',
      message: '正在提交更新任务',
      started_at: new Date().toISOString(),
    })

    try {
      const res = await systemApi.updatePanel()
      applyUpdateSnapshot(res.data || updateStatus.value)
      if (updateInfo.value?.update_target?.update_manager === 'watchtower' || updateInfo.value?.update_target?.watchtower_managed === true) {
        updatingPanel.value = false
        ElMessage.success('已触发 Watchtower 检查更新，请稍后查看 Watchtower 日志或等待容器重建结果')
        return
      }
      startUpdateStatusPolling()
    } catch (err: any) {
      failUpdateProgress(err?.response?.data?.error || err?.message || '更新失败，请手动更新')
    }
  }

  function buildDockerUpdateConfirmMessage(mirrorHost?: string, pullImageName?: string) {
    const mirrorText = mirrorHost
      ? `当前将通过镜像源 ${mirrorHost} 拉取更新镜像。`
      : '当前将直接从默认镜像仓库拉取更新镜像。'
    const pullTargetText = pullImageName ? `\n拉取目标：${pullImageName}` : ''
    return `确认开始更新面板吗？系统会先拉取最新镜像，再自动重建容器。更新期间服务会短暂中断。\n${mirrorText}${pullTargetText}`
  }

  function buildWatchtowerUpdateConfirmMessage(updateTarget: any) {
    const scheduleText = updateTarget.watchtower_schedule
      ? `\n当前调度：${updateTarget.watchtower_schedule}`
      : ''
    return `确认手动触发 Watchtower 立即检查更新吗？\n这会请求 Watchtower 立刻执行一次更新检查，而不是等待下一次定时窗口。${scheduleText}`
  }

  function buildBinaryUpdateConfirmMessage(updateTarget: any) {
    const assetText = updateTarget.asset_name ? `\n更新包：${updateTarget.asset_name}` : ''
    const installDirText = updateTarget.install_dir ? `\n安装目录：${updateTarget.install_dir}` : ''
    return `确认开始更新面板吗？系统会在后台下载当前平台的二进制更新包，替换程序与前端文件，并自动重启面板。\n后台更新会保留 config.yaml、Dumb-Panel、data、logs、backups 等本地配置与数据目录。${assetText}${installDirText}`
  }

  function openUpdateProgress(snapshot?: PanelUpdateStatus | null) {
    updateProgressVisible.value = true
    updateProgressStatus.value = 'running'
    updateProgressError.value = ''
    updateStatus.value = snapshot || {
      status: 'running',
      phase: 'preparing',
      message: '正在准备更新任务...',
      started_at: new Date().toISOString(),
    }
  }

  function applyUpdateSnapshot(snapshot?: PanelUpdateStatus | null) {
    updateStatus.value = snapshot || {}
    updateProgressVisible.value = true

    if (updateStatus.value?.status === 'failed') {
      updateProgressStatus.value = 'failed'
      updateProgressError.value = updateStatus.value?.error || updateStatus.value?.message || '更新失败'
      updatingPanel.value = false
      return
    }

    if (updateStatus.value?.status === 'restarting') {
      updateProgressStatus.value = 'restarting'
      updateProgressError.value = ''
      return
    }

    updateProgressStatus.value = 'running'
    updateProgressError.value = ''
  }

  function failUpdateProgress(message: string) {
    stopUpdateStatusPolling()
    stopUpdateAvailabilityChecks()
    updateProgressVisible.value = true
    updateProgressStatus.value = 'failed'
    updateProgressError.value = message
    updateStatus.value = {
      ...(updateStatus.value || {}),
      status: 'failed',
      phase: updateStatus.value?.phase || 'failed',
      message,
      error: message,
    }
    updatingPanel.value = false
    ElMessage.error(message)
  }

  function startUpdateStatusPolling() {
    stopUpdateStatusPolling()
    const startedAt = Date.now()

    const poll = async () => {
      try {
        const res = await systemApi.updateStatus()
        applyUpdateSnapshot(res.data || {})

        if (updateStatus.value?.status === 'failed') {
          return
        }

        if (updateStatus.value?.status === 'restarting') {
          stopUpdateStatusPolling()
          waitForAvailability()
          return
        }

        if (Date.now() - startedAt >= 12 * 60 * 1000) {
          failUpdateProgress('更新超时，请手动刷新页面检查')
          return
        }

        updateStatusPollTimer = setTimeout(() => {
          void poll()
        }, 2000)
      } catch (err: any) {
        if (shouldTreatAsRestart(err)) {
          stopUpdateStatusPolling()
          updateProgressStatus.value = 'restarting'
          waitForAvailability()
          return
        }
        failUpdateProgress(err?.response?.data?.error || err?.message || '更新状态获取失败')
      }
    }

    void poll()
  }

  async function handleRestartPanel() {
    try {
      await ElMessageBox.confirm('确定要重启面板吗？重启期间服务将短暂中断。', '重启面板', {
        confirmButtonText: '确认重启',
        cancelButtonText: '取消',
        type: 'warning'
      })
      await systemApi.restart()
      waitForRestart()
    } catch {
      // cancelled
    }
  }

  function stopRestartPolling() {
    if (restartDelayTimer) {
      clearTimeout(restartDelayTimer)
      restartDelayTimer = null
    }
    if (restartPollTimer) {
      clearInterval(restartPollTimer)
      restartPollTimer = null
    }
  }

  function waitForRestart() {
    stopRestartPolling()
    let attempts = 0
    restartDelayTimer = setTimeout(() => {
      restartDelayTimer = null
      restartPollTimer = setInterval(async () => {
        attempts++
        try {
          const res = await fetch('/', { method: 'HEAD' })
          if (res.ok) {
            stopRestartPolling()
            window.location.reload()
          }
        } catch {
          // ignore
        }
        if (attempts >= 60) {
          stopRestartPolling()
          ElMessage.warning('重启超时，请手动刷新页面')
        }
      }, 2000)
    }, 3000)
  }

  function waitForAvailability() {
    stopUpdateAvailabilityChecks()
    updateProgressVisible.value = true
    updateProgressStatus.value = 'restarting'

    let attempts = 0
    updateAvailabilityDelayTimer = setTimeout(() => {
      updateAvailabilityDelayTimer = null
      const probe = async () => {
        attempts++
        try {
          const res = await fetch('/', { method: 'HEAD', cache: 'no-store' })
          if (res.ok) {
            stopUpdateAvailabilityChecks()
            window.location.reload()
            return
          }
        } catch {
          // ignore
        }

        if (attempts >= 80) {
          stopUpdateAvailabilityChecks()
          updateProgressStatus.value = 'timeout'
          updateProgressError.value = '等待新版本启动超时，请稍后手动刷新页面检查'
          updatingPanel.value = false
          ElMessage.warning('等待新版本启动超时，请手动刷新页面检查')
          return
        }

        updateAvailabilityTimer = setTimeout(() => {
          void probe()
        }, 3000)
      }

      void probe()
    }, 2000)
  }

  function stopUpdateStatusPolling() {
    if (updateStatusPollTimer) {
      clearTimeout(updateStatusPollTimer)
      updateStatusPollTimer = null
    }
  }

  function stopUpdateAvailabilityChecks() {
    if (updateAvailabilityDelayTimer) {
      clearTimeout(updateAvailabilityDelayTimer)
      updateAvailabilityDelayTimer = null
    }
    if (updateAvailabilityTimer) {
      clearTimeout(updateAvailabilityTimer)
      updateAvailabilityTimer = null
    }
  }

  function closeUpdateProgress() {
    if (updateProgressStatus.value === 'running' || updateProgressStatus.value === 'restarting') {
      return
    }
    stopUpdateStatusPolling()
    stopUpdateAvailabilityChecks()
    updateProgressVisible.value = false
    updateProgressStatus.value = 'idle'
    updateProgressError.value = ''
  }

  function shouldTreatAsRestart(err: any) {
    if (!updateStatus.value?.status) {
      return false
    }

    if (err?.response) {
      return false
    }

    return updateStatus.value.status === 'running' || updateStatus.value.status === 'restarting'
  }

  function openGitHub() {
    const url = updateInfo.value?.has_update && updateInfo.value?.release_url
      ? updateInfo.value.release_url
      : 'https://github.com/linzixuanzz/daidai-panel/releases'
    window.open(url, '_blank')
  }

  onBeforeUnmount(() => {
    stopUpdateStatusPolling()
    stopUpdateAvailabilityChecks()
    stopRestartPolling()
  })

  return {
    systemInfo,
    systemStats,
    currentVersion,
    updateInfo,
    updateStatus,
    checkingUpdate,
    updatingPanel,
    autoUpdateEnabled,
    savingAutoUpdate,
    lastCheckTime,
    releaseNotesVisible,
    updateProgressVisible,
    updateProgressStatus,
    updateProgressError,
    formatBytes,
    getUsageClass,
    loadSystemInfo,
    loadSystemStats,
    loadVersion,
    loadUpdatePreferences,
    handleCheckUpdate,
    handleUpdatePanel,
    handleRestartPanel,
    handleToggleAutoUpdate,
    openReleaseNotes,
    closeReleaseNotes,
    openGitHub,
    closeUpdateProgress
  }
}
