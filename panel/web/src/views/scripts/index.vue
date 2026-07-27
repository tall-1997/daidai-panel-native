<script setup lang="ts">
import { ElMessageBox } from 'element-plus'
import ScriptExecutionDialogs from './components/ScriptExecutionDialogs.vue'
import ScriptManageDialogs from './components/ScriptManageDialogs.vue'
import ScriptsEditorPane from './components/ScriptsEditorPane.vue'
import ScriptsSidebar from './components/ScriptsSidebar.vue'
import { useScriptExecution } from './useScriptExecution'
import { useScriptWorkspace } from './useScriptWorkspace'

const workspace = useScriptWorkspace()
const execution = useScriptExecution({
  selectedFile: workspace.selectedFile,
  fileContent: workspace.fileContent
})

const {
  isMobile,
  isCompactLayout,
  mobileShowEditor,
  fileTree,
  selectedFile,
  fileContent,
  originalContent,
  isBinary,
  loading,
  saving,
  treeLoading,
  isEditing,
  editorAutoFocusTicket,
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
  formatting,
  editorLanguage,
  hasChanges,
  allFolders,
  loadTree,
  handleNodeClick,
  handleSave,
  handleCreateFile,
  handleCreateDir,
  handleDelete,
  handleMoveToRoot,
  allowDrag,
  allowDrop,
  handleNodeDrop,
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
  handleMobileBack
} = workspace

const {
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
  handleDebugStop,
  openCodeRunner,
  handleRunCode,
  handleStopRunner
} = execution

async function handleDebugSave() {
  if (!selectedFile.value || isBinary.value) {
    return
  }
  fileContent.value = debugCode.value
  isEditing.value = true
  await handleSave()
  debugCodeChanged.value = debugCode.value !== originalContent.value
}

function openCreateFileDialog() {
  showCreateFileDialog.value = true
}

function openCreateDirDialog() {
  showCreateDirDialog.value = true
}

function openSelectedFileRenameDialog() {
  openRename(selectedFile.value)
}

function handleDeleteSelectedFile() {
  return handleDelete(selectedFile.value)
}

async function handleCancelEdit() {
  if (hasChanges.value) {
    try {
      await ElMessageBox.confirm('当前有未保存的改动，确认放弃修改并退出编辑？', '退出编辑', {
        confirmButtonText: '放弃改动',
        cancelButtonText: '继续编辑',
        type: 'warning'
      })
    } catch {
      return
    }
    fileContent.value = originalContent.value
  }
  isEditing.value = false
}
</script>

<template>
  <div class="scripts-page dd-fixed-page dd-page-hide-heading" :class="{ mobile: isMobile, compact: isCompactLayout }">
    <div class="page-header">
      <div>
        <h2>脚本管理</h2>
        <p class="page-subtitle">在线编辑、调试和管理您的自动化脚本文件</p>
      </div>
    </div>

    <div class="scripts-workspace" :class="{ 'mobile-show-editor': isCompactLayout && mobileShowEditor }">
      <ScriptsSidebar
        :is-mobile="isCompactLayout"
        :mobile-show-editor="mobileShowEditor"
        :tree-loading="treeLoading"
        :file-tree="fileTree"
        :allow-drag="allowDrag"
        :allow-drop="allowDrop"
        :on-open-create-file="openCreateFileDialog"
        :on-open-create-dir="openCreateDirDialog"
        :on-open-upload="openUploadDialog"
        :on-open-code-runner="openCodeRunner"
        :on-refresh="loadTree"
        :on-node-click="handleNodeClick"
        :on-node-drop="handleNodeDrop"
        :on-open-rename="openRename"
        :on-delete="handleDelete"
        :on-move-to-root="handleMoveToRoot"
      />

      <ScriptsEditorPane
        v-model:file-content="fileContent"
        v-model:is-editing="isEditing"
        :is-mobile="isCompactLayout"
        :mobile-show-editor="mobileShowEditor"
        :selected-file="selectedFile"
        :is-binary="isBinary"
        :has-changes="hasChanges"
        :saving="saving"
        :formatting="formatting"
        :loading="loading"
        :editor-language="editorLanguage"
        :editor-auto-focus-ticket="editorAutoFocusTicket"
        :on-mobile-back="handleMobileBack"
        :on-debug-run="handleDebugRun"
        :on-open-create-file="openCreateFileDialog"
        :on-add-to-task="handleAddToTask"
        :on-save="handleSave"
        :on-cancel-edit="handleCancelEdit"
        :on-format="handleFormat"
        :on-load-versions="loadVersions"
        :on-open-rename="openSelectedFileRenameDialog"
        :on-download="handleDownload"
        :on-delete="handleDeleteSelectedFile"
      />
    </div>

    <ScriptManageDialogs
      v-model:show-create-file-dialog="showCreateFileDialog"
      v-model:show-create-dir-dialog="showCreateDirDialog"
      v-model:show-rename-dialog="showRenameDialog"
      v-model:show-version-dialog="showVersionDialog"
      v-model:show-version-diff-dialog="showVersionDiffDialog"
      v-model:show-upload-dialog="showUploadDialog"
      v-model:new-file-name="newFileName"
      v-model:new-file-parent="newFileParent"
      v-model:new-dir-name="newDirName"
      v-model:new-dir-parent="newDirParent"
      v-model:rename-target="renameTarget"
      v-model:upload-dir="uploadDir"
      v-model:version-diff-original-title="versionDiffOriginalTitle"
      v-model:version-diff-modified-title="versionDiffModifiedTitle"
      v-model:version-diff-original-content="versionDiffOriginalContent"
      v-model:version-diff-modified-content="versionDiffModifiedContent"
      :is-mobile="isMobile"
      :selected-file="selectedFile"
      :all-folders="allFolders"
      :editor-language="editorLanguage"
      :versions="versions"
      :versions-loading="versionsLoading"
      :version-diff-loading="versionDiffLoading"
      :on-create-file="handleCreateFile"
      :on-create-dir="handleCreateDir"
      :on-rename="handleRename"
      :on-compare-version="handleCompareVersion"
      :on-rollback="handleRollback"
      :on-clear-versions="handleClearVersions"
      :on-upload-file-change="handleUploadFileChange"
      :on-upload-submit="handleUploadSubmit"
    />

    <ScriptExecutionDialogs
      v-model:show-code-runner="showCodeRunner"
      v-model:runner-code="runnerCode"
      v-model:runner-language="runnerLanguage"
      v-model:show-debug-dialog="showDebugDialog"
      v-model:debug-code="debugCode"
      v-model:debug-code-changed="debugCodeChanged"
      :is-mobile="isMobile"
      :editor-language="editorLanguage"
      :debug-file-name="debugFileName"
      :debug-logs="debugLogs"
      :debug-running="debugRunning"
      :debug-error="debugError"
      :debug-exit-code="debugExitCode"
      :runner-logs="runnerLogs"
      :runner-running="runnerRunning"
      :runner-exit-code="runnerExitCode"
      :runner-error="runnerError"
      :debug-saving="saving"
      :on-debug-start="handleDebugStart"
      :on-debug-save="handleDebugSave"
      :on-debug-stop="handleDebugStop"
      :on-run-code="handleRunCode"
      :on-stop-runner="handleStopRunner"
    />
  </div>
</template>

<style scoped lang="scss">
.scripts-page {
  --scripts-accent: #3b82f6;
  --scripts-surface: var(--el-bg-color);
  --scripts-surface-muted: color-mix(in srgb, var(--el-fill-color) 70%, transparent);
  --scripts-border-soft: var(--el-border-color-lighter);

  padding: 0;
  font-size: 14px;
  font-family: var(--dd-font-ui);
  min-height: 0;
}

/* ---- Page Header (design system) ---- */
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 18px;
  gap: 16px;

  h2 {
    margin: 0;
    font-size: 22px;
    font-weight: 700;
    color: var(--el-text-color-primary);
    line-height: 1.3;
  }

  .page-subtitle {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    margin: 4px 0 0;
  }
}

/* ---- Workspace（两卡容器：透明 flex，卡片间留间隙）---- */
.scripts-workspace {
  display: flex;
  flex: 1 1 auto;
  width: 100%;
  height: 0;
  min-width: 0;
  min-height: 0;
  /* 目录树卡与编辑器卡之间的间隙 */
  gap: 14px;
  /* 容器本身透明，圆角/边框/阴影下放到两张子卡 */
  background: transparent;
}

/* ---- 目录树卡（独立圆角卡片）---- */
:deep(.scripts-sidebar) {
  flex: 0 0 300px;
  min-height: 0;
  overflow: hidden; /* 裁切卡片圆角 */
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--dd-card-radius);
  box-shadow: var(--dd-shadow-card);
}

/* ---- 编辑器卡（独立圆角卡片）---- */
:deep(.scripts-editor) {
  min-height: 0;
  overflow: hidden; /* 裁切卡片圆角 */
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--dd-card-radius);
  box-shadow: var(--dd-shadow-card);
}

:deep(.editor-hero) {
  border-bottom: 1px solid var(--el-border-color-lighter);
}

:deep(.editor-statusbar) {
  border-top: 1px solid var(--el-border-color-lighter);
}

/* ---- Mobile layout ---- */
.scripts-page.mobile {
  height: 100%;
  min-height: 0;
  overflow: hidden;

  .page-header {
    flex-direction: column;
    gap: 10px;
    margin-bottom: 14px;

    h2 {
      font-size: 18px;
    }
  }

  /* 移动端：两卡纵向堆叠，各占满宽度并保留卡片圆角 */
  .scripts-workspace {
    flex-direction: column;
    flex: 1 1 auto;
    width: 100%;
    height: 100%;
    min-height: 0;
    gap: 12px;

    :deep(.scripts-sidebar) {
      width: 100%;
      min-width: unset;
      flex: 1 1 auto;
      min-height: 0;
      overflow: hidden; /* 裁圆角；目录滚动由内部 .sidebar-tree 负责 */
    }

    :deep(.scripts-editor) {
      width: 100%;
      flex: 1 1 auto;
      min-height: 0;
    }
  }

  /* 切到「显示编辑器」时隐藏目录卡、铺满编辑器卡 */
  .scripts-workspace.mobile-show-editor {
    :deep(.scripts-sidebar) {
      display: none !important;
    }

    :deep(.scripts-editor) {
      display: flex !important;
      height: 100%;
      min-height: 0;
    }
  }
}

/* ---- Compact desktop/tablet layout ---- */
.scripts-page.compact:not(.mobile) {
  .page-header {
    flex-direction: column;
    gap: 10px;
    margin-bottom: 14px;

    h2 {
      font-size: 18px;
    }
  }

  /* compact：同样纵向堆叠两卡，保留卡片圆角与间隙 */
  .scripts-workspace {
    flex-direction: column;
    min-height: 0;
    height: 0;
    gap: 12px;

    :deep(.scripts-sidebar) {
      width: 100%;
      min-width: 0;
      flex: 1 1 auto;
      min-height: 0;
      overflow: hidden;
    }

    :deep(.scripts-editor) {
      width: 100%;
      min-width: 0;
      flex: 1 1 auto;
      min-height: 0;
    }
  }

  .scripts-workspace.mobile-show-editor {
    :deep(.scripts-editor) {
      height: 100%;
    }
  }
}
</style>
