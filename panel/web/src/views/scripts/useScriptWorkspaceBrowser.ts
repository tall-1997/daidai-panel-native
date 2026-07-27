import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { scriptApi } from '@/api/script'
import { useResponsive } from '@/composables/useResponsive'
import type { TreeNode } from './types'

type ScriptBrowserState = {
  selectedFile: string
  fileContent: string
  originalContent: string
  isBinary: boolean
  isEditing: boolean
  mobileShowEditor: boolean
  editorAutoFocusTicket: number
}

export function useScriptWorkspaceBrowser() {
  const { isMobile, isTablet } = useResponsive()
  const isCompactLayout = computed(() => isTablet.value)
  const mobileShowEditor = ref(false)

  const fileTree = ref<TreeNode[]>([])
  const selectedFile = ref('')
  const fileContent = ref('')
  const originalContent = ref('')
  const isBinary = ref(false)
  const loading = ref(false)
  const treeLoading = ref(false)
  const isEditing = ref(false)
  const editorAutoFocusTicket = ref(0)

  const editorLanguage = computed(() => {
    if (!selectedFile.value) return 'javascript'
    const ext = selectedFile.value.split('.').pop()?.toLowerCase()
    const langMap: Record<string, string> = {
      js: 'javascript',
      mjs: 'javascript',
      ts: 'typescript',
      py: 'python',
      sh: 'shell',
      go: 'go',
      json: 'json',
      yaml: 'yaml',
      yml: 'yaml',
      md: 'markdown',
      html: 'html',
      css: 'css',
      xml: 'xml'
    }
    return langMap[ext || ''] || 'plaintext'
  })

  const hasChanges = computed(() => fileContent.value !== originalContent.value)

  function extractScriptErrorMessage(err: any, fallback: string) {
    const message = String(err?.response?.data?.error || err?.message || '').trim()
    if (!message) {
      return fallback
    }

    if (message.includes('当前路径是目录')) {
      return '当前选中的是目录，不是可编辑脚本文件'
    }
    if (message.includes('文件不存在')) {
      return '脚本不存在，可能已被删除、重命名或移动'
    }
    if (message.includes('不允许路径穿越') || message.includes('检测到路径穿越') || message.includes('路径包含非法字符')) {
      return '脚本路径无效，请刷新文件树后重试'
    }

    return message
  }

  function shouldSkipFolder(path: string) {
    return path
      .split('/')
      .map(segment => segment.trim().toLowerCase())
      .some(segment => segment === 'node_modules' || segment === '__pycache__')
  }

  function normalizeTreeNodes(nodes: TreeNode[]): TreeNode[] {
    return nodes
      .filter(node => !shouldSkipFolder(node.key))
      .map((node) => {
        if (node.isLeaf) {
          return node
        }
        return {
          ...node,
          children: normalizeTreeNodes(node.children || [])
        }
      })
  }

  const allFolders = computed(() => {
    const folders: string[] = ['']
    const collectFolders = (nodes: TreeNode[], prefix = '') => {
      for (const node of nodes) {
        if (!node.isLeaf) {
          const path = prefix ? `${prefix}/${node.title}` : node.title
          folders.push(path)
          if (node.children) {
            collectFolders(node.children, path)
          }
        }
      }
    }
    collectFolders(fileTree.value)
    return folders
  })

  watch(isCompactLayout, (compact) => {
    if (!compact) {
      mobileShowEditor.value = false
    }
  })

  function snapshotState(): ScriptBrowserState {
    return {
      selectedFile: selectedFile.value,
      fileContent: fileContent.value,
      originalContent: originalContent.value,
      isBinary: isBinary.value,
      isEditing: isEditing.value,
      mobileShowEditor: mobileShowEditor.value,
      editorAutoFocusTicket: editorAutoFocusTicket.value
    }
  }

  function restoreState(state: ScriptBrowserState) {
    selectedFile.value = state.selectedFile
    fileContent.value = state.fileContent
    originalContent.value = state.originalContent
    isBinary.value = state.isBinary
    isEditing.value = state.isEditing
    mobileShowEditor.value = state.mobileShowEditor
    editorAutoFocusTicket.value = state.editorAutoFocusTicket
  }

  async function loadTree() {
    treeLoading.value = true
    try {
      const res = await scriptApi.tree()
      fileTree.value = normalizeTreeNodes(res.data || [])
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || err?.message || '加载文件树失败')
    } finally {
      treeLoading.value = false
    }
  }

  async function loadFileContent(path: string, options: { silent?: boolean } = {}) {
    loading.value = true
    try {
      const res = await scriptApi.getContent(path)
      isBinary.value = res.data.is_binary ?? res.data.binary ?? false
      fileContent.value = res.data.content
      originalContent.value = res.data.content
      return true
    } catch (err: any) {
      if (!options.silent) {
        ElMessage.error(extractScriptErrorMessage(err, '加载文件内容失败'))
      }
      return false
    } finally {
      loading.value = false
    }
  }

  async function confirmOpenFile(path: string, skipUnsavedCheck = false) {
    if (skipUnsavedCheck || !hasChanges.value || path === selectedFile.value) {
      return true
    }

    try {
      await ElMessageBox.confirm('当前文件有未保存的修改，是否放弃？', '提示', {
        confirmButtonText: '放弃',
        cancelButtonText: '取消',
        type: 'warning'
      })
      return true
    } catch {
      return false
    }
  }

  async function openFile(path: string, options: { skipUnsavedCheck?: boolean } = {}) {
    const normalizedPath = path.trim()
    if (!normalizedPath) {
      return false
    }

    if (normalizedPath === selectedFile.value) {
      mobileShowEditor.value = true
      return true
    }

    const canProceed = await confirmOpenFile(normalizedPath, options.skipUnsavedCheck ?? false)
    if (!canProceed) {
      return false
    }

    const previousState = snapshotState()
    selectedFile.value = normalizedPath
    isEditing.value = false
    const loaded = await loadFileContent(normalizedPath)
    if (!loaded) {
      restoreState(previousState)
      return false
    }

    mobileShowEditor.value = true
    return true
  }

  function triggerEditorAutoFocus() {
    editorAutoFocusTicket.value += 1
  }

  async function handleNodeClick(data: TreeNode) {
    if (!data.isLeaf) return
    await openFile(data.key)
  }

  function allowDrag(draggingNode: any) {
    return draggingNode.data.isLeaf
  }

  function allowDrop(draggingNode: any, dropNode: any, type: string) {
    if (type === 'inner') {
      return !dropNode.data.isLeaf
    }
    if (type === 'before' || type === 'after') {
      return dropNode.level === 1
    }
    return false
  }

  async function handleNodeDrop(draggingNode: any, dropNode: any, dropType: string) {
    const sourcePath = draggingNode.data.key
    const targetDir = dropType === 'inner' ? dropNode.data.key : ''
    try {
      await scriptApi.move(sourcePath, targetDir)
      ElMessage.success('移动成功')
      if (selectedFile.value === sourcePath) {
        const fileName = sourcePath.split('/').pop() || sourcePath
        selectedFile.value = targetDir ? `${targetDir}/${fileName}` : fileName
      }
      await loadTree()
    } catch {
      ElMessage.error('移动失败')
      await loadTree()
    }
  }

  function handleMobileBack() {
    mobileShowEditor.value = false
  }

  return {
    isMobile,
    isCompactLayout,
    mobileShowEditor,
    fileTree,
    selectedFile,
    fileContent,
    originalContent,
    isBinary,
    loading,
    treeLoading,
    isEditing,
    editorAutoFocusTicket,
    editorLanguage,
    hasChanges,
    allFolders,
    extractScriptErrorMessage,
    loadTree,
    loadFileContent,
    openFile,
    triggerEditorAutoFocus,
    handleNodeClick,
    allowDrag,
    allowDrop,
    handleNodeDrop,
    handleMobileBack
  }
}
