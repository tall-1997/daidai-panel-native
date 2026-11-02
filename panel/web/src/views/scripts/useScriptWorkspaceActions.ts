import { ref, type ComputedRef, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { scriptApi } from '@/api/script'
import { taskApi } from '@/api/task'
import { isTaskRunnableScriptPath, taskCommandMatchesScript } from '@/utils/taskCommandScript'
import type { ScriptVersionDetail, ScriptVersionRecord } from './types'

interface ScriptWorkspaceActionsOptions {
  selectedFile: Ref<string>
  fileContent: Ref<string>
  originalContent: Ref<string>
  isBinary: Ref<boolean>
  isEditing: Ref<boolean>
  hasChanges: ComputedRef<boolean>
  loadTree: () => Promise<void>
  loadFileContent: (path: string, options?: { silent?: boolean }) => Promise<boolean>
  extractScriptErrorMessage: (err: any, fallback: string) => string
  openFile?: (path: string, options?: { skipUnsavedCheck?: boolean }) => Promise<boolean>
  triggerEditorAutoFocus?: () => void
}

export function useScriptWorkspaceActions({
  selectedFile,
  fileContent,
  originalContent,
  isBinary,
  isEditing,
  hasChanges,
  loadTree,
  loadFileContent,
  extractScriptErrorMessage,
  openFile,
  triggerEditorAutoFocus
}: ScriptWorkspaceActionsOptions) {
  const router = useRouter()

  const saving = ref(false)
  const formatting = ref(false)
  // 新建文件 / 新建目录 / 上传各自一把在途锁，对齐上面 saving 的写法。
  // 刻意不合并成一个 flag：合并后点「新建文件」会让「新建目录」「上传」的按钮一起转圈。
  const creatingFile = ref(false)
  const creatingDir = ref(false)
  const uploading = ref(false)

  const showCreateFileDialog = ref(false)
  const showCreateDirDialog = ref(false)
  const showRenameDialog = ref(false)
  const showVersionDialog = ref(false)
  const showVersionDiffDialog = ref(false)
  const showUploadDialog = ref(false)

  const uploadDir = ref('')
  const uploadFileList = ref<File[]>([])

  const newFileName = ref('')
  const newFileParent = ref('')
  const newDirName = ref('')
  const newDirParent = ref('')
  const renameTarget = ref('')
  const renamePath = ref('')

  const versions = ref<ScriptVersionRecord[]>([])
  const versionsLoading = ref(false)
  const versionDiffLoading = ref(false)
  const versionDiffOriginalTitle = ref('')
  const versionDiffModifiedTitle = ref('')
  const versionDiffOriginalContent = ref('')
  const versionDiffModifiedContent = ref('')

  function isActionCancelled(err: unknown) {
    return err === 'cancel' || err === 'close' || String(err) === 'cancel' || String(err) === 'close'
  }

  async function verifyEditableTarget(path: string) {
    const normalizedPath = path.trim()
    if (!normalizedPath) {
      ElMessage.warning('当前没有可保存的脚本')
      return false
    }

    const loaded = await loadFileContent(normalizedPath, { silent: true })
    if (!loaded) {
      ElMessage.error('保存失败：脚本可能已被删除、移动，或当前选中的不是可编辑文件')
      return false
    }

    if (isBinary.value) {
      ElMessage.warning('当前文件为二进制文件，不能在线保存')
      return false
    }

    return true
  }

  async function saveCurrentFile() {
    if (!selectedFile.value || isBinary.value) return
    saving.value = true
    try {
      const currentPath = selectedFile.value
      const snapshotContent = fileContent.value
      const verified = await verifyEditableTarget(currentPath)
      if (!verified) {
        return false
      }

      // 校验会刷新后端最新内容；如果用户本地正有改动，需要把待保存内容覆盖回去。
      fileContent.value = snapshotContent

      let versionMessage = 'V1 初始版本'
      if (originalContent.value !== '') {
        try {
          const res = await scriptApi.listVersions(currentPath)
          const versionCount = res.data?.length || 0
          versionMessage = `V${versionCount + 1} 更新`
        } catch {
          versionMessage = 'V2 更新'
        }
      }
      await scriptApi.saveContent(currentPath, fileContent.value, versionMessage)
      originalContent.value = fileContent.value
      ElMessage.success('保存成功')
      return true
    } catch (err: any) {
      ElMessage.error(extractScriptErrorMessage(err, '保存失败'))
      return false
    } finally {
      saving.value = false
    }
  }

  async function handleSave() {
    await saveCurrentFile()
  }

  async function handleCreateFile() {
    if (!newFileName.value.trim()) return
    // 输入框上还挂着 @keyup.enter="onCreateFile"，按钮的 :loading/:disabled 拦不住回车入口：
    // 请求期间 newFileName 还没清空（清空在 await 之后）、弹窗也还开着，
    // 连按两次回车会把同一个文件写两遍（版本历史里出现两条「V1 初始版本」）。这里补一道在途判断。
    if (creatingFile.value) return
    creatingFile.value = true
    try {
      const fullPath = newFileParent.value
        ? `${newFileParent.value}/${newFileName.value.trim()}`
        : newFileName.value.trim()
      await scriptApi.saveContent(fullPath, '', 'V1 初始版本')
      ElMessage.success('创建成功')
      showCreateFileDialog.value = false
      newFileName.value = ''
      newFileParent.value = ''
      await loadTree()
      if (openFile) {
        const opened = await openFile(fullPath, { skipUnsavedCheck: true })
        if (opened) {
          isEditing.value = true
          triggerEditorAutoFocus?.()
        }
      } else {
        selectedFile.value = fullPath
        isEditing.value = true
        await loadFileContent(fullPath)
        triggerEditorAutoFocus?.()
      }
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '创建失败')
    } finally {
      creatingFile.value = false
    }
  }

  async function handleCreateDir() {
    if (!newDirName.value.trim()) return
    // 同 handleCreateFile：目录名输入框上挂着 @keyup.enter="onCreateDir"，绕开按钮的 :loading。
    // 连按回车时第二次会撞上「目录已存在」，用户会看到「创建成功」后紧跟一条红色报错。
    if (creatingDir.value) return
    creatingDir.value = true
    try {
      const fullPath = newDirParent.value
        ? `${newDirParent.value}/${newDirName.value.trim()}`
        : newDirName.value.trim()
      await scriptApi.createDirectory(fullPath)
      ElMessage.success('创建成功')
      showCreateDirDialog.value = false
      newDirName.value = ''
      newDirParent.value = ''
      await loadTree()
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '创建失败')
    } finally {
      creatingDir.value = false
    }
  }

  async function handleMoveToRoot(path: string, _isDir = false) {
    const fileName = path.split('/').pop() || path
    try {
      await ElMessageBox.confirm(`确定要将 ${fileName} 移动到根���录吗？`, '移动到根目录', { type: 'info' })
      await scriptApi.move(path, '/')
      ElMessage.success('移动成功')
      if (selectedFile.value === path) {
        selectedFile.value = fileName
        await loadFileContent(fileName)
      }
      await loadTree()
    } catch (err: any) {
      if (isActionCancelled(err)) return
      ElMessage.error(err?.response?.data?.error || err?.message || '移动失败')
    }
  }

  async function handleDelete(path: string, isDir = false) {
    try {
      await ElMessageBox.confirm(`确定要删除 ${path} 吗？${isDir ? '\n注意：将同时删除文件夹内所有文件！' : ''}`, '确认删除', { type: 'warning' })
      await scriptApi.delete(path, isDir ? 'directory' : 'file')
      ElMessage.success('删除成功')
      if (selectedFile.value === path || (isDir && selectedFile.value.startsWith(path + '/'))) {
        selectedFile.value = ''
        fileContent.value = ''
        originalContent.value = ''
      }
      await loadTree()
    } catch (err: any) {
      if (isActionCancelled(err)) return
      ElMessage.error(err?.response?.data?.error || err?.message || '删除失败')
    }
  }

  async function handleRename() {
    if (!renameTarget.value.trim()) return
    try {
      const res = await scriptApi.rename(renamePath.value, renameTarget.value.trim())
      ElMessage.success('重命名成功')
      showRenameDialog.value = false
      if (selectedFile.value === renamePath.value) {
        selectedFile.value = res.new_path || renameTarget.value.trim()
      }
      await loadTree()
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '重命名失败')
    }
  }

  function openRename(path: string) {
    renamePath.value = path
    renameTarget.value = path.split('/').pop() || path
    showRenameDialog.value = true
  }

  function openUploadDialog() {
    showUploadDialog.value = true
    uploadDir.value = ''
    uploadFileList.value = []
  }

  async function handleUpload(files: File[]) {
    const formData = new FormData()
    for (const file of files) {
      formData.append('file', file)
    }
    if (uploadDir.value) {
      formData.append('dir', uploadDir.value)
    }
    try {
      const res = await scriptApi.upload(formData)
      const uploadedPaths = Array.isArray(res.paths) && res.paths.length > 0
        ? res.paths
        : files.map((file) => (uploadDir.value ? `${uploadDir.value}/${file.name}` : file.name))

      ElMessage.success(uploadedPaths.length > 1 ? `成功上传 ${uploadedPaths.length} 个文件` : '上传成功')
      showUploadDialog.value = false
      uploadDir.value = ''
      uploadFileList.value = []
      await loadTree()

      // 多文件上传刻意不问：一次传 N 个文件对应不了一条任务，这里维持原状。
      if (uploadedPaths.length === 1) {
        const targetPath = uploadedPaths[0]
        if (!targetPath) return false
        await askAddUploadedScriptToTask(targetPath)
      }
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '上传失败')
    }
    return false
  }

  function handleUploadFileChange(_file: { raw?: File } | undefined, files: Array<{ raw?: File }>) {
    uploadFileList.value = files
      .map((item) => item.raw)
      .filter((file): file is File => Boolean(file))
  }

  async function handleUploadSubmit() {
    if (uploadFileList.value.length === 0) {
      ElMessage.warning('请至少选择一个文件')
      return
    }
    // 上传目前只有按钮一个入口、不存在回车绕过，这道在途判断是和上面两个函数保持一致：
    // 三处写法统一，将来给上传弹窗补回车提交时不会再漏一次防重。
    if (uploading.value) return
    // 上传本身耗时较长，且成功后还会弹「是否加到定时任务」确认框，
    // 整段都要锁住，否则连点会重复上传并连开多个确认框。
    uploading.value = true
    try {
      await handleUpload(uploadFileList.value)
    } finally {
      uploading.value = false
    }
  }

  /**
   * 统计「这个脚本已经有几条定时任务」。
   * 返回 null = 没查成（网络抖动 / 401 / 后端报错），调用方必须按「不知道」处理，也就是照旧弹窗 ——
   * 重复问一次只是烦，静默吞掉「上传后加定时任务」这个入口要糟糕得多。
   */
  async function countTasksUsingScript(targetPath: string) {
    try {
      // keyword 用 basename 不用整路径：keyword 是 name/command 的子串匹配，
      // 而同一个脚本在库里至少有 8 种合法命令形态（`task "a b/x.py"`、`task a\ b/x.py` …），
      // 整路径在这些形态里根本不成子串，会漏。宽召回交给下面的逐条解析去收窄。
      const basename = targetPath.split('/').pop() || targetPath
      if (!basename) return null
      // 刻意不传 filters / sort_rules：带上任意一个都会让后端切到内存全表路径
      // （无 LIMIT 地 Find 出全部匹配行再在内存里筛排），任务多的面板上是实打实的开销。
      const res = await taskApi.list({ keyword: basename, all: 1 })
      const rows = Array.isArray(res?.data) ? res.data : []

      let total = 0
      let disabled = 0
      for (const row of rows) {
        const command = typeof row?.command === 'string' ? row.command : ''
        // 必须整体相等比较，绝不能拿 basename 裸 includes：
        // `jd/qd.py` 的任务不是 `qd.py` 的任务，误判会把弹窗错误地吞掉。
        if (!command || !taskCommandMatchesScript(command, targetPath)) continue
        total++
        // status=0 是已禁用。它同样占着「这个脚本已经配过任务了」的事实，要算进去，
        // 但得在文案里点明，否则用户在列表里按默认筛选找不到那几条会以为面板在骗人。
        if (Number(row?.status) === 0) disabled++
      }
      return { total, disabled }
    } catch {
      return null
    }
  }

  /**
   * 上传单个文件后的「是否加到定时任务」询问，两道前置判断（issue #118 建议 3）：
   * 1. 扩展名不在 task 能跑的 6 种里 —— 问了也只会生成一条永远跑不起来的 `task config.json`；
   * 2. 该脚本已经有定时任务 —— 覆盖式重传是最常见的用法，不该每次都拦一下，降级成一条不阻断的提示。
   * 注意这里只管上传入口；编辑器右上角的「添加任务」按钮保持不拦，
   * 同一脚本按账号拆 `desi` / `conc` 建多条任务是官方文档推荐的正常用法，那是用户的逃生门。
   */
  async function askAddUploadedScriptToTask(targetPath: string) {
    if (!isTaskRunnableScriptPath(targetPath)) return

    const existing = await countTasksUsingScript(targetPath)
    if (existing && existing.total > 0) {
      const disabledNote = existing.disabled > 0 ? `（${existing.disabled} 条已禁用）` : ''
      ElMessage.info(`该脚本已有 ${existing.total} 条定时任务${disabledNote}，未重复询问`)
      return
    }

    try {
      await ElMessageBox.confirm('是否将此脚本添加到定时任务？', '提示', {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'info'
      })
      navigateToTaskWithScript(targetPath)
    } catch {
      // cancelled
    }
  }

  function navigateToTaskWithScript(filePath: string) {
    const fileName = filePath.split('/').pop() || filePath
    const taskName = fileName.replace(/\.[^/.]+$/, '')
    const command = `task ${filePath}`
    void router.push({
      path: '/tasks',
      query: { autoCreate: '1', name: taskName, command }
    })
  }

  function handleAddToTask() {
    if (!selectedFile.value) return
    navigateToTaskWithScript(selectedFile.value)
  }

  async function loadVersions() {
    if (!selectedFile.value) return
    versionsLoading.value = true
    showVersionDialog.value = true
    try {
      const res = await scriptApi.listVersions(selectedFile.value)
      versions.value = res.data || []
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '加载版本历史失败')
    } finally {
      versionsLoading.value = false
    }
  }

  async function handleRollback(versionId: number) {
    try {
      await ElMessageBox.confirm('确定要回滚到此版本吗？', '确认回滚', { type: 'warning' })
    } catch {
      return
    }
    try {
      await scriptApi.rollback(versionId)
      ElMessage.success('回滚成功')
      showVersionDialog.value = false
      await loadFileContent(selectedFile.value)
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || '回滚失败')
    }
  }

  async function handleClearVersions() {
    if (!selectedFile.value) return

    try {
      await ElMessageBox.confirm(
        `确定要清空 ${selectedFile.value} 的全部版本历史吗？\n此操作不可恢复，但不会删除当前脚本文件。`,
        '清空版本历史',
        {
          type: 'warning',
          confirmButtonText: '确认清空',
          cancelButtonText: '取消'
        }
      )

      const res = await scriptApi.clearVersions(selectedFile.value)
      const clearedCount = Number(res.cleared_count || versions.value.length || 0)
      versions.value = []
      showVersionDiffDialog.value = false
      versionDiffOriginalTitle.value = ''
      versionDiffModifiedTitle.value = ''
      versionDiffOriginalContent.value = ''
      versionDiffModifiedContent.value = ''
      ElMessage.success(clearedCount > 0 ? `已清空 ${clearedCount} 条版本记录` : '版本历史已清空')
    } catch (err: any) {
      if (isActionCancelled(err)) return
      ElMessage.error(err?.response?.data?.error || err?.message || '清空版本历史失败')
    }
  }

  function buildVersionLabel(version: ScriptVersionRecord) {
    const message = version.message?.trim()
    return message ? `V${version.version} · ${message}` : `V${version.version}`
  }

  async function handleCompareVersion(version: ScriptVersionRecord) {
    if (!selectedFile.value) return

    const currentContentSnapshot = fileContent.value
    const currentFileName = getFileName(selectedFile.value)

    versionDiffLoading.value = true
    versionDiffOriginalTitle.value = buildVersionLabel(version)
    versionDiffModifiedTitle.value = hasChanges.value
      ? `${currentFileName} · 当前未保存代码`
      : `${currentFileName} · 当前代码`
    versionDiffOriginalContent.value = ''
    versionDiffModifiedContent.value = currentContentSnapshot
    showVersionDiffDialog.value = true

    try {
      const res = await scriptApi.getVersion(version.id)
      const detail = res.data as ScriptVersionDetail | undefined
      versionDiffOriginalContent.value = detail?.content || ''
    } catch (err: any) {
      showVersionDiffDialog.value = false
      ElMessage.error(err?.response?.data?.error || err?.message || '加载版本对比失败')
    } finally {
      versionDiffLoading.value = false
    }
  }

  async function handleFormat() {
    if (!selectedFile.value || isBinary.value) return
    const langMap: Record<string, string> = {
      py: 'python',
      sh: 'shell',
      go: 'go',
      json: 'json'
    }
    const ext = selectedFile.value.split('.').pop()?.toLowerCase() || ''
    const lang = langMap[ext]
    if (!lang) {
      ElMessage.warning('该文件类型不支持格式化')
      return
    }
    formatting.value = true
    try {
      const res = await scriptApi.format({ content: fileContent.value, language: lang })
      if (res.data?.content) {
        fileContent.value = res.data.content
        ElMessage.success('格式化完成')
      }
    } catch {
      ElMessage.error('格式化失败')
    } finally {
      formatting.value = false
    }
  }

  function getFileName(path: string) {
    return path.split('/').pop() || path
  }

  function handleDownload() {
    if (!selectedFile.value) return
    void (async () => {
      try {
        if (hasChanges.value && !isBinary.value) {
          const saved = await saveCurrentFile()
          if (!saved) {
            return
          }
        }
        const blob = await scriptApi.download(selectedFile.value)
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = getFileName(selectedFile.value)
        a.click()
        URL.revokeObjectURL(url)
      } catch (err: any) {
        ElMessage.error(err?.response?.data?.error || err?.message || '下载失败')
      }
    })()
  }

  function handleKeyDown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault()
      if (selectedFile.value && !isBinary.value && hasChanges.value) {
        void handleSave()
      }
    }
  }

  return {
    saving,
    formatting,
    creatingFile,
    creatingDir,
    uploading,
    showCreateFileDialog,
    showCreateDirDialog,
    showRenameDialog,
    showVersionDialog,
    showVersionDiffDialog,
    showUploadDialog,
    uploadDir,
    newFileName,
    newFileParent,
    newDirName,
    newDirParent,
    renameTarget,
    versions,
    versionsLoading,
    versionDiffLoading,
    versionDiffOriginalTitle,
    versionDiffModifiedTitle,
    versionDiffOriginalContent,
    versionDiffModifiedContent,
    handleSave,
    handleCreateFile,
    handleCreateDir,
    handleDelete,
    handleMoveToRoot,
    handleRename,
    openRename,
    openUploadDialog,
    handleUploadFileChange,
    handleUploadSubmit,
    handleAddToTask,
    loadVersions,
    handleRollback,
    handleClearVersions,
    handleCompareVersion,
    handleFormat,
    handleDownload,
    handleKeyDown
  }
}
