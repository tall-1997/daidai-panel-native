import { onActivated, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useScriptWorkspaceActions } from './useScriptWorkspaceActions'
import { useScriptWorkspaceBrowser } from './useScriptWorkspaceBrowser'

export function useScriptWorkspace() {
  const router = useRouter()
  const route = useRoute()

  const browser = useScriptWorkspaceBrowser()
  const actions = useScriptWorkspaceActions({
    selectedFile: browser.selectedFile,
    fileContent: browser.fileContent,
    originalContent: browser.originalContent,
    isBinary: browser.isBinary,
    isEditing: browser.isEditing,
    hasChanges: browser.hasChanges,
    loadTree: browser.loadTree,
    loadFileContent: browser.loadFileContent,
    extractScriptErrorMessage: browser.extractScriptErrorMessage,
    openFile: browser.openFile,
    triggerEditorAutoFocus: browser.triggerEditorAutoFocus
  })
  let skipInitialActivated = true

  onMounted(() => {
    window.addEventListener('keydown', actions.handleKeyDown)
    void browser.loadTree()
  })

  onActivated(() => {
    if (skipInitialActivated) {
      skipInitialActivated = false
      return
    }
    void browser.loadTree()
  })

  async function openFileFromRoute(fileParam?: string) {
    if (!fileParam) return

    await browser.openFile(fileParam)
    await router.replace({ path: '/scripts' })
  }

  watch(
    () => route.query.file,
    (fileParam) => {
      if (typeof fileParam !== 'string' || !fileParam.trim()) {
        return
      }
      void openFileFromRoute(fileParam)
    },
    { immediate: true }
  )

  watch(
    () => route.query.upload,
    (uploadParam) => {
      if (uploadParam !== '1') {
        return
      }
      actions.openUploadDialog()
      void router.replace({ path: '/scripts' })
    },
    { immediate: true }
  )

  onBeforeUnmount(() => {
    window.removeEventListener('keydown', actions.handleKeyDown)
  })

  return {
    ...browser,
    ...actions
  }
}
