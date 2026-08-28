import { nextTick, onBeforeUnmount, onDeactivated, onMounted, ref, watch, type Ref } from 'vue'
import { ElMessage } from 'element-plus'
import { scriptApi } from '@/api/script'

interface UseScriptExecutionOptions {
  selectedFile: Ref<string>
  fileContent: Ref<string>
}

export function useScriptExecution({ selectedFile, fileContent }: UseScriptExecutionOptions) {
  const showDebugDialog = ref(false)
  const debugCode = ref('')
  const debugFileName = ref('')
  const debugRunId = ref('')
  const debugLogs = ref<string[]>([])
  const debugRunning = ref(false)
  const debugError = ref('')
  const debugExitCode = ref<number | null>(null)
  const debugCodeChanged = ref(false)

  const showCodeRunner = ref(false)
  const runnerCode = ref('')
  const runnerLanguage = ref('python')
  const runnerRunId = ref('')
  const runnerLogs = ref<string[]>([])
  const runnerRunning = ref(false)
  const runnerExitCode = ref<number | null>(null)
  const runnerError = ref('')

  let debugTimer: ReturnType<typeof setTimeout> | null = null
  let runnerTimer: ReturnType<typeof setTimeout> | null = null
  let debugGeneration = 0
  let runnerGeneration = 0

  function getFileName(path: string) {
    return path.split('/').pop() || path
  }

  function clearDebugTimer() {
    if (debugTimer) {
      clearTimeout(debugTimer)
      debugTimer = null
    }
  }

  function clearRunnerTimer() {
    if (runnerTimer) {
      clearTimeout(runnerTimer)
      runnerTimer = null
    }
  }

  async function stopDebugRun(options?: { keepalive?: boolean; preserveLogs?: boolean }) {
    debugGeneration += 1
    const runId = debugRunId.value
    clearDebugTimer()
    debugRunning.value = false
    debugRunId.value = ''

    if (!runId) {
      return
    }

    if (options?.keepalive) {
      scriptApi.debugStopKeepalive(runId)
      return
    }

    try {
      await scriptApi.debugStop(runId)
    } catch {
      // ignore
    }

    if (!options?.preserveLogs) {
      return
    }

    try {
      const res = await scriptApi.debugLogs(runId)
      debugLogs.value = res.data.logs || []
      if (res.data.status === 'failed') {
        debugExitCode.value = res.data.exit_code ?? null
        debugError.value = 'failed'
      }
    } catch {
      // ignore
    }
  }

  async function stopRunnerRun(options?: { keepalive?: boolean }) {
    runnerGeneration += 1
    const runId = runnerRunId.value
    clearRunnerTimer()
    runnerRunning.value = false
    runnerRunId.value = ''

    if (!runId) {
      return
    }

    if (options?.keepalive) {
      scriptApi.debugStopKeepalive(runId)
      return
    }

    try {
      await scriptApi.debugStop(runId)
    } catch {
      // ignore
    }
  }

  function handleWindowPageHide() {
    void stopDebugRun({ keepalive: true })
    void stopRunnerRun({ keepalive: true })
  }

  watch(showDebugDialog, (val) => {
    if (!val) {
      void stopDebugRun()
    }
  })

  watch(showCodeRunner, (val) => {
    if (!val) {
      void stopRunnerRun()
    }
  })

  onMounted(() => {
    window.addEventListener('pagehide', handleWindowPageHide)
    window.addEventListener('beforeunload', handleWindowPageHide)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('pagehide', handleWindowPageHide)
    window.removeEventListener('beforeunload', handleWindowPageHide)
    void stopDebugRun()
    void stopRunnerRun()
  })

  onDeactivated(() => {
    void stopDebugRun()
    void stopRunnerRun()
  })

  async function handleDebugRun() {
    if (!selectedFile.value) return
    debugCode.value = fileContent.value
    debugFileName.value = getFileName(selectedFile.value)
    debugLogs.value = []
    debugRunning.value = false
    debugError.value = ''
    debugExitCode.value = null
    debugRunId.value = ''
    debugCodeChanged.value = false
    showDebugDialog.value = true
  }

  async function handleDebugStart(options?: { forceEditorContent?: boolean }) {
    if (!selectedFile.value) return
    const generation = ++debugGeneration
    clearDebugTimer()
    debugLogs.value = []
    debugError.value = ''
    debugExitCode.value = null
    debugRunning.value = true
    try {
      const shouldRunTempContent = !!options?.forceEditorContent || debugCodeChanged.value || debugCode.value !== fileContent.value
      const res = shouldRunTempContent
        ? await scriptApi.debugRun({
            path: selectedFile.value,
            content: debugCode.value,
            language: runnerLanguageForFile(selectedFile.value)
          })
        : await scriptApi.debugRun({ path: selectedFile.value })
      if (generation !== debugGeneration) {
        scriptApi.debugStopKeepalive(res.run_id)
        return
      }
      debugRunId.value = res.run_id
      pollDebugLogs(res.run_id, generation)
    } catch (err: any) {
      if (generation !== debugGeneration) return
      debugError.value = err?.response?.data?.error || err?.message || '调试运行失败'
      ElMessage.error(debugError.value)
      debugRunning.value = false
    }
  }

  async function openDebugAndStart(options?: { useEditorContent?: boolean }) {
    if (!selectedFile.value) return
    await handleDebugRun()
    if (options?.useEditorContent) {
      debugCode.value = fileContent.value
      debugCodeChanged.value = true
    }
    await nextTick()
    await handleDebugStart({ forceEditorContent: !!options?.useEditorContent })
  }

  function runnerLanguageForFile(path: string) {
    const lower = path.toLowerCase()
    if (lower.endsWith('.py')) return 'python'
    if (lower.endsWith('.js')) return 'javascript'
    if (lower.endsWith('.ts')) return 'typescript'
    if (lower.endsWith('.sh')) return 'shell'
    if (lower.endsWith('.go')) return 'go'
    return 'python'
  }

  function pollDebugLogs(runId: string, generation: number) {
    clearDebugTimer()
    debugTimer = setTimeout(async () => {
      if (generation !== debugGeneration || debugRunId.value !== runId) return
      try {
        const res = await scriptApi.debugLogs(runId)
        if (generation !== debugGeneration || debugRunId.value !== runId) return
        debugLogs.value = res.data.logs || []
        if (res.data.done) {
          debugRunning.value = false
          clearDebugTimer()
          debugRunId.value = ''
          if (res.data.status === 'failed') {
            debugExitCode.value = res.data.exit_code ?? null
            debugError.value = 'failed'
          }
        } else {
          pollDebugLogs(runId, generation)
        }
      } catch {
        if (generation !== debugGeneration || debugRunId.value !== runId) return
        debugRunning.value = false
        clearDebugTimer()
      }
    }, 500)
  }

  async function handleDebugStop() {
    await stopDebugRun({ preserveLogs: true })
  }

  function openCodeRunner() {
    runnerCode.value = ''
    runnerLanguage.value = 'python'
    runnerLogs.value = []
    runnerRunning.value = false
    runnerExitCode.value = null
    runnerError.value = ''
    runnerRunId.value = ''
    showCodeRunner.value = true
  }

  async function handleRunCode() {
    if (!runnerCode.value.trim()) {
      ElMessage.warning('请输入代码')
      return
    }
    const generation = ++runnerGeneration
    clearRunnerTimer()
    runnerLogs.value = []
    runnerExitCode.value = null
    runnerError.value = ''
    runnerRunning.value = true
    try {
      const res = await scriptApi.runCode(runnerCode.value, runnerLanguage.value)
      if (generation !== runnerGeneration) {
        scriptApi.debugStopKeepalive(res.run_id)
        return
      }
      runnerRunId.value = res.run_id
      pollRunnerLogs(res.run_id, generation)
    } catch (err: any) {
      if (generation !== runnerGeneration) return
      const msg = err?.response?.data?.error || err?.message || '运行失败'
      runnerError.value = msg
      ElMessage.error(msg)
      runnerRunning.value = false
    }
  }

  function pollRunnerLogs(runId: string, generation: number) {
    clearRunnerTimer()
    runnerTimer = setTimeout(async () => {
      if (generation !== runnerGeneration || runnerRunId.value !== runId) return
      try {
        const res = await scriptApi.debugLogs(runId)
        if (generation !== runnerGeneration || runnerRunId.value !== runId) return
        runnerLogs.value = res.data.logs || []
        if (res.data.done) {
          runnerRunning.value = false
          runnerExitCode.value = res.data.exit_code ?? null
          if (res.data.status === 'failed') {
            runnerError.value = 'failed'
          }
          clearRunnerTimer()
          runnerRunId.value = ''
        } else {
          pollRunnerLogs(runId, generation)
        }
      } catch {
        if (generation !== runnerGeneration || runnerRunId.value !== runId) return
        runnerRunning.value = false
        runnerError.value = '获取运行日志失败，请检查服务状态'
        clearRunnerTimer()
      }
    }, 500)
  }

  async function handleStopRunner() {
    await stopRunnerRun()
  }

  return {
    showDebugDialog,
    debugCode,
    debugFileName,
    debugLogs,
    debugRunning,
    debugError,
    debugExitCode,
    debugCodeChanged,
    showCodeRunner,
    runnerCode,
    runnerLanguage,
    runnerLogs,
    runnerRunning,
    runnerExitCode,
    runnerError,
    handleDebugRun,
    handleDebugStart,
    openDebugAndStart,
    handleDebugStop,
    openCodeRunner,
    handleRunCode,
    handleStopRunner
  }
}
