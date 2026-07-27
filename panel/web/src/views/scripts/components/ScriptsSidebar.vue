<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  ArrowDown,
  DocumentAdd,
  FolderAdd,
  Refresh,
  Search,
  Upload,
  VideoPlay
} from '@element-plus/icons-vue'
import ScriptTreeNode from './ScriptTreeNode.vue'
import type { TreeNode } from '../types'

defineProps<{
  isMobile: boolean
  mobileShowEditor: boolean
  treeLoading: boolean
  fileTree: TreeNode[]
  allowDrag: (draggingNode: any) => boolean
  allowDrop: (draggingNode: any, dropNode: any, type: string) => boolean
  onOpenCreateFile: () => void
  onOpenCreateDir: () => void
  onOpenUpload: () => void
  onOpenCodeRunner: () => void
  onRefresh: () => void | Promise<void>
  onNodeClick: (data: TreeNode) => void | Promise<void>
  onNodeDrop: (draggingNode: any, dropNode: any, dropType: string) => void | Promise<void>
  onOpenRename: (path: string) => void
  onDelete: (path: string, isDir: boolean) => void | Promise<void>
  onMoveToRoot?: (path: string, isDir: boolean) => void | Promise<void>
}>()

const treeRef = ref()
const searchKeyword = ref('')

function filterNode(value: string, data: TreeNode) {
  if (!value) return true
  return (data.title || '').toLowerCase().includes(value.toLowerCase())
}

watch(searchKeyword, (val) => {
  treeRef.value?.filter(val)
})
</script>

<template>
  <aside class="scripts-sidebar" :class="{ mobile: isMobile }" v-show="!isMobile || !mobileShowEditor">
    <header class="sidebar-top">
      <div class="sidebar-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索文件或目录"
          clearable
          :prefix-icon="Search"
          class="sidebar-search-input"
        />
      </div>

      <div class="sidebar-toolbar">
        <div class="sidebar-toolbar-label">
          <span class="label-main">脚本文件</span>
        </div>
        <div class="sidebar-toolbar-actions">
          <el-dropdown trigger="click" placement="bottom-end">
            <el-button class="primary-new-btn" type="primary" plain>
              <el-icon><DocumentAdd /></el-icon>
              <span>新建</span>
              <el-icon class="chevron"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="onOpenCreateFile">
                  <el-icon><DocumentAdd /></el-icon>新建文件
                </el-dropdown-item>
                <el-dropdown-item @click="onOpenCreateDir">
                  <el-icon><FolderAdd /></el-icon>新建目录
                </el-dropdown-item>
                <el-dropdown-item divided @click="onOpenUpload">
                  <el-icon><Upload /></el-icon>上传文件
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>

          <el-tooltip content="刷新" placement="bottom">
            <button class="icon-btn" aria-label="刷新" @click="onRefresh">
              <el-icon :size="15"><Refresh /></el-icon>
            </button>
          </el-tooltip>
        </div>
      </div>
    </header>

    <div class="sidebar-tree" v-loading="treeLoading">
      <el-tree
        ref="treeRef"
        :data="fileTree"
        node-key="key"
        :props="{ children: 'children', label: 'title' }"
        :highlight-current="true"
        :expand-on-click-node="true"
        :filter-node-method="filterNode"
        draggable
        :allow-drag="allowDrag"
        :allow-drop="allowDrop"
        empty-text="暂无脚本文件"
        @node-drop="onNodeDrop"
        @node-click="onNodeClick"
      >
        <template #default="{ data }">
          <ScriptTreeNode :data="data" :on-open-rename="onOpenRename" :on-delete="onDelete" :on-move-to-root="onMoveToRoot" />
        </template>
      </el-tree>
    </div>

    <footer class="sidebar-footer">
      <button class="runner-card" @click="onOpenCodeRunner" aria-label="打开代码运行器">
        <div class="runner-card-icon">
          <el-icon :size="16"><VideoPlay /></el-icon>
        </div>
        <div class="runner-card-body">
          <span class="runner-card-title">代码运行器</span>
          <span class="runner-card-desc">粘贴片段即刻执行</span>
        </div>
      </button>
    </footer>
  </aside>
</template>

<style scoped lang="scss">
.scripts-sidebar {
  width: 300px;
  min-width: 300px;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0;
  padding: 0;
  /* 卡片表面/边框/阴影由 index.vue 的 :deep(.scripts-sidebar) 统一负责，
     这里不再画 border-right 分隔线与方向性投影（两卡独立后无需分隔） */
  background: var(--el-bg-color);
  box-sizing: border-box;
  font-family: var(--dd-font-ui);
  overflow: hidden;
}

.sidebar-top {
  display: flex;
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 80%, transparent);
  padding-bottom: 10px;
  position: relative;
  flex-direction: column;
  gap: 10px;
  flex-shrink: 0;
  padding: 16px 14px 0;
}

.sidebar-search-input {
  :deep(.el-input__wrapper) {
    border-radius: 8px;
    padding: 4px 12px;
    box-shadow: 0 0 0 1px var(--el-border-color-lighter) inset;
    transition: box-shadow 0.2s, background 0.2s;
    background: var(--el-fill-color-light);
  }

  :deep(.el-input__wrapper.is-focus) {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--el-color-primary) 45%, transparent) inset;
    background: var(--el-bg-color);
  }

  :deep(.el-input__inner) {
    font-size: 13px;
    font-family: var(--dd-font-ui);
  }
}

.sidebar-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0 0;
}

.sidebar-toolbar-label {
  display: flex;
  align-items: baseline;
  gap: 6px;

  .label-main {
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--el-text-color-secondary);
  }
}

.sidebar-toolbar-actions {
  display: flex;
  padding: 2px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--el-fill-color-light) 84%, transparent);
  align-items: center;
  gap: 6px;
}

.primary-new-btn {
  height: 30px;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
  padding: 0 10px;
  border-radius: 8px;
  font-size: 12.5px;
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 4px;

  .chevron {
    font-size: 10px;
    margin-left: 2px;
    opacity: 0.7;
    transition: transform 0.18s ease;
  }

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 10px 20px rgba(64, 158, 255, 0.12);
  }

  &:hover .chevron {
    transform: translateY(1px);
  }
}

.icon-btn {
  width: 30px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.5);
  height: 30px;
  padding: 0;
  border: 1px solid var(--el-border-color-lighter);
  background: transparent;
  border-radius: 8px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s, border-color 0.15s;

  &:hover {
    color: var(--el-color-primary);
    border-color: color-mix(in srgb, var(--el-color-primary) 40%, var(--el-border-color-lighter));
    background: color-mix(in srgb, var(--el-color-primary) 6%, transparent);
    transform: translateY(-1px);
    box-shadow: 0 8px 18px rgba(15, 23, 42, 0.08);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--el-color-primary) 50%, transparent);
    outline-offset: 1px;
  }
}

.sidebar-tree {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 8px 10px 10px;

  :deep(.el-tree) {
    background: transparent;
    color: inherit;
    min-width: 0;
  }

  :deep(.el-tree-node),
  :deep(.el-tree-node__children) {
    min-width: 0;
  }

  :deep(.el-tree-node__content) {
    height: 34px;
    transition: background 0.15s, transform 0.16s ease, box-shadow 0.16s ease;
    min-width: 0;
    padding-left: 4px;
    border-radius: 6px;
    transition: background 0.15s;
    font-size: 13px;
    overflow: hidden;
  }

  :deep(.el-tree-node__content:hover) {
    background: var(--el-fill-color-light);
    transform: translateX(2px);
    box-shadow: inset 2px 0 0 color-mix(in srgb, var(--el-color-primary) 30%, transparent);
  }

  :deep(.el-tree-node.is-current > .el-tree-node__content) {
    background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
    position: relative;

    &::before {
      content: '';
      position: absolute;
      left: 0;
      top: 6px;
      bottom: 6px;
      width: 2.5px;
      border-radius: 2px;
      background: var(--el-color-primary);
    }
  }

  :deep(.el-tree-node.is-drop-inner > .el-tree-node__content) {
    background: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
    outline: 2px dashed var(--el-color-primary);
    outline-offset: -2px;
  }

  :deep(.el-tree__drop-indicator) {
    height: 2px;
    background: var(--el-color-primary);
    border-radius: 1px;
  }

  :deep(.el-tree-node.is-dragging > .el-tree-node__content) {
    opacity: 0.4;
  }

  :deep(.el-tree-node__expand-icon) {
    color: var(--el-text-color-placeholder);
    font-size: 12px;
  }
}

.sidebar-footer {
  flex-shrink: 0;
  padding: 10px 14px 14px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.runner-card {
  width: 100%;
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-light);
  color: inherit;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, border-color 0.15s, box-shadow 0.2s;

  &:hover {
    background: color-mix(in srgb, var(--scripts-accent, #22c55e) 8%, var(--el-fill-color-light));
    border-color: color-mix(in srgb, var(--scripts-accent, #22c55e) 40%, var(--el-border-color-lighter));
    box-shadow: 0 10px 22px rgba(15, 23, 42, 0.08);
    transform: translateY(-2px);
  }

  &::after {
    content: "";
    position: absolute;
    inset: -55% 0 auto;
    height: 70%;
    background: linear-gradient(180deg, rgba(255,255,255,0.3), transparent);
    opacity: 0;
    transform: translateY(-10px);
    transition: opacity 180ms ease, transform 180ms ease;
    pointer-events: none;
  }

  &:hover::after {
    opacity: 1;
    transform: translateY(0);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--scripts-accent, #22c55e) 70%, transparent);
    outline-offset: 2px;
  }
}

.runner-card-icon {
  width: 30px;
  box-shadow: 0 8px 18px rgba(34, 197, 94, 0.12);
  height: 30px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--scripts-accent, #22c55e);
  background: color-mix(in srgb, var(--scripts-accent, #22c55e) 12%, transparent);
  flex-shrink: 0;
}

.runner-card-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.runner-card-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  line-height: 1.2;
}

.runner-card-desc {
  font-size: 11.5px;
  color: var(--el-text-color-secondary);
  line-height: 1.2;
  letter-spacing: 0.1px;
}

.scripts-sidebar.mobile {
  width: 100%;
  min-width: 0;
  min-height: 0;

  .sidebar-toolbar {
    padding: 0;
  }

  :deep(.tree-node) {
    .tree-node-actions,
    .tree-node-ext {
      opacity: 1;
    }
  }
}
</style>


<style lang="scss">
html.dark {
  /* 暗色卡面/边框由 --el-bg-color 与 index.vue 的卡片样式自动适配，
     此处仅保留内部分区线与控件底色的暗色覆盖 */
  .scripts-sidebar .sidebar-top,
  .scripts-sidebar .sidebar-footer {
    border-color: rgba(255,255,255,0.08);
  }

  .scripts-sidebar .sidebar-toolbar-actions,
  .scripts-sidebar .runner-card {
    background: color-mix(in srgb, var(--el-bg-color-overlay) 92%, black);
  }

  .scripts-sidebar .primary-new-btn {
    box-shadow: 0 10px 20px rgba(37, 99, 235, 0.16);
  }

  .scripts-sidebar .runner-card:hover {
    box-shadow: 0 14px 26px rgba(0,0,0,0.28);
  }
}
</style>
