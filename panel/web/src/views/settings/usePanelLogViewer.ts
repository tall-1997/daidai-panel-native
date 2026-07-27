import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { systemApi } from '@/api/system'
import { copyText } from '@/utils/clipboard'
import { usePageActivity } from '@/composables/usePageActivity'

export type PanelLogLevel = '' | 'debug' | 'info' | 'warn' | 'error'

const defaultPanelLogLines = 200
const defaultPanelLogLevel: PanelLogLevel = 'info'
const updatePanelLogKeyword = '更新'

export function usePanelLogViewer() {
  const { isPageActive } = usePageActivity()

  const loading = ref(false)
  const refreshing = ref(false)
  const lines = ref(defaultPanelLogLines)
  const keyword = ref('')
  const level = ref<PanelLogLevel>(defaultPanelLogLevel)
  const autoRefresh = ref(true)
  const logs = ref<string[]>([])
  const total = ref(0)
  const lastLoadedAt = ref('')

  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  let filterTimer: ReturnType<typeof setTimeout> | null = null

  const joinedLogText = computed(() => logs.value.join('\n'))
  const hasLogs = computed(() => logs.value.length > 0)
  const activePreset = computed<'default' | 'updates' | 'errors' | ''>(() => {
    if (keyword.value.trim() === updatePanelLogKeyword && level.value === defaultPanelLogLevel) {
      return 'updates'
    }
    if (!keyword.value.trim() && level.value === 'error') {
      return 'errors'
    }
    if (!keyword.value.trim() && level.value === defaultPanelLogLevel && lines.value === defaultPanelLogLines) {
      return 'default'
    }
    return ''
  })
  const byteSizeLabel = computed(() => {
    if (!joinedLogText.value) return '0 B'
    const bytes = new Blob([joinedLogText.value]).size
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  })

  function stopAutoRefresh() {
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
  }

  function stopFilterReload() {
    if (filterTimer) {
      clearTimeout(filterTimer)
      filterTimer = null
    }
  }

  function scheduleAutoRefresh() {
    stopAutoRefresh()
    if (!autoRefresh.value || !isPageActive.value) {
      return
    }
    refreshTimer = setTimeout(() => {
      void loadPanelLogs(false)
    }, 3000)
  }

  function scheduleFilterReload() {
    stopFilterReload()
    filterTimer = setTimeout(() => {
      void loadPanelLogs(false)
    }, 220)
  }

  async function loadPanelLogs(showError = true) {
    const firstLoad = logs.value.length === 0 && !lastLoadedAt.value
    if (firstLoad) {
      loading.value = true
    } else {
      refreshing.value = true
    }
    try {
      const res = await systemApi.panelLog({
        lines: lines.value,
        keyword: keyword.value.trim() || undefined,
        level: level.value,
      })
      logs.value = res.data?.logs || []
      total.value = Number(res.data?.total || 0)
      lastLoadedAt.value = new Date().toLocaleString()
    } catch (err: any) {
      if (showError) {
        ElMessage.error(err?.response?.data?.error || err?.message || '加载面板日志失败')
      }
    } finally {
      loading.value = false
      refreshing.value = false
      scheduleAutoRefresh()
    }
  }

  async function refreshNow() {
    await loadPanelLogs(true)
  }

  function applyUpdatePreset() {
    keyword.value = updatePanelLogKeyword
    level.value = defaultPanelLogLevel
  }

  function applyErrorPreset() {
    keyword.value = ''
    level.value = 'error'
  }

  function resetFilters() {
    lines.value = defaultPanelLogLines
    keyword.value = ''
    level.value = defaultPanelLogLevel
    autoRefresh.value = true
    void loadPanelLogs(false)
  }

  async function copyLogs() {
    if (!hasLogs.value) {
      ElMessage.warning('暂无日志可复制')
      return
    }
    try {
      await copyText(joinedLogText.value)
      ElMessage.success('已复制面板日志')
    } catch {
      ElMessage.error('复制失败')
    }
  }

  function downloadLogs() {
    if (!hasLogs.value) {
      ElMessage.warning('暂无日志可下载')
      return
    }
    const blob = new Blob([joinedLogText.value], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `panel-log-${Date.now()}.log`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success('已下载面板日志')
  }

  watch(
    () => [autoRefresh.value, isPageActive.value],
    () => {
      scheduleAutoRefresh()
    }
  )

  watch(
    () => [lines.value, keyword.value, level.value, isPageActive.value],
    (_value, _oldValue) => {
      if (!isPageActive.value) {
        return
      }
      scheduleFilterReload()
    }
  )

  onBeforeUnmount(() => {
    stopAutoRefresh()
    stopFilterReload()
  })

  return {
    loading,
    refreshing,
    lines,
    keyword,
    level,
    autoRefresh,
    logs,
    total,
    lastLoadedAt,
    hasLogs,
    activePreset,
    joinedLogText,
    byteSizeLabel,
    loadPanelLogs,
    refreshNow,
    applyUpdatePreset,
    applyErrorPreset,
    resetFilters,
    copyLogs,
    downloadLogs,
  }
}
