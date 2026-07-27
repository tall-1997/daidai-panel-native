import { ref, type ComputedRef, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { scriptApi } from '@/api/script'
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
    }
  }

  async function handleCreateDir() {
    if (!newDirName.value.trim()) return
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

      if (uploadedPaths.length === 1) {
        const targetPath = uploadedPaths[0]
        if (!targetPath) return false
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
    await handleUpload(uploadFileList.value)
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
