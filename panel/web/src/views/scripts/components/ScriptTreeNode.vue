<script setup lang="ts">
import { computed } from 'vue'
import { Delete, Edit, FolderRemove, MoreFilled } from '@element-plus/icons-vue'
import type { TreeNode } from '../types'

const props = defineProps<{
  data: TreeNode
  onOpenRename: (path: string) => void
  onDelete: (path: string, isDir: boolean) => void | Promise<void>
  onMoveToRoot?: (path: string, isDir: boolean) => void | Promise<void>
}>()

const isInSubDir = computed(() => props.data.key.includes('/'))

const dotColor = computed(() => {
  if (!props.data.isLeaf) return 'var(--scripts-folder-dot, #94a3b8)'
  const ext = props.data.title.split('.').pop()?.toLowerCase()
  switch (ext) {
    case 'js':
      return '#facc15'
    case 'ts':
      return '#38bdf8'
    case 'py':
      return '#22c55e'
    case 'sh':
      return '#4ade80'
    case 'json':
      return '#fb923c'
    case 'yaml':
    case 'yml':
      return '#f87171'
    case 'md':
      return '#818cf8'
    case 'html':
      return '#fb7185'
    case 'css':
      return '#60a5fa'
    case 'go':
      return '#06b6d4'
    default:
      return 'var(--el-text-color-placeholder)'
  }
})

const extLabel = computed(() => {
  if (!props.data.isLeaf) return ''
  const parts = props.data.title.split('.')
  if (parts.length < 2) return ''
  return (parts.pop() || '').toUpperCase()
})
</script>

<template>
  <div class="tree-node" :class="{ 'is-folder': !data.isLeaf, 'is-leaf': data.isLeaf }">
    <span class="tree-node-dot" :style="{ background: dotColor }" aria-hidden="true"></span>
    <span class="tree-node-label">{{ data.title }}</span>
    <span v-if="extLabel" class="tree-node-ext">{{ extLabel }}</span>
    <div class="tree-node-actions" @click.stop>
      <el-dropdown trigger="click" size="small">
        <button class="more-btn" aria-label="更多操作">
          <el-icon :size="16"><MoreFilled /></el-icon>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="onOpenRename(data.key)">
              <el-icon><Edit /></el-icon>重命名
            </el-dropdown-item>
            <el-dropdown-item v-if="isInSubDir && onMoveToRoot" @click="onMoveToRoot(data.key, !data.isLeaf)">
              <el-icon><FolderRemove /></el-icon>移动到根目录
            </el-dropdown-item>
            <el-dropdown-item divided @click="onDelete(data.key, !data.isLeaf)">
              <el-icon><Delete /></el-icon><span style="color: var(--el-color-danger)">删除</span>
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tree-node {
  display: flex;
  border-radius: 8px;
  transition: background-color 0.16s ease, transform 0.16s ease;
  align-items: center;
  gap: 10px;
  flex: 1;
  width: 100%;
  min-width: 0;
  padding: 0 2px;
  font-family: var(--dd-font-ui);
  overflow: hidden;
}

.tree-node-dot {
  width: 8px;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--el-bg-color) 70%, transparent);
  transition: transform 0.2s;
}

.tree-node.is-folder .tree-node-dot {
  border-radius: 2px;
  width: 9px;
  height: 9px;
  opacity: 0.75;
}

.tree-node-label {
  flex: 1;
  transition: color 0.15s ease;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13.5px;
  color: var(--el-text-color-primary);
  letter-spacing: 0.1px;
}

.tree-node.is-folder .tree-node-label {
  font-weight: 500;
}

.tree-node-ext {
  font-size: 9.5px;
  transform: translateY(2px);
  font-weight: 700;
  font-family: var(--dd-font-mono);
  padding: 2px 6px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--el-fill-color) 85%, transparent);
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  letter-spacing: 0.4px;
  line-height: 1.3;
  opacity: 0;
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.tree-node-actions {
  opacity: 0;
  transform: translateX(4px);
  transition: opacity 0.15s ease, transform 0.15s ease;
  flex-shrink: 0;

  .more-btn {
    cursor: pointer;
    width: 24px;
    height: 24px;
    padding: 0;
    border: none;
    background: transparent;
    border-radius: 6px;
    color: var(--el-text-color-secondary);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: background 0.15s, color 0.15s, transform 0.15s ease, box-shadow 0.15s ease;

    &:hover {
      background: var(--el-fill-color);
      color: var(--el-color-primary);
      transform: translateY(-1px);
      box-shadow: 0 6px 14px rgba(15, 23, 42, 0.08);
    }

    &:focus-visible {
      outline: 2px solid color-mix(in srgb, var(--el-color-primary) 50%, transparent);
      outline-offset: 1px;
    }
  }
}

.tree-node:hover {
  background: color-mix(in srgb, var(--el-fill-color-light) 86%, transparent);
  .tree-node-label { color: var(--el-color-primary); }
  transform: translateX(1px);

  .tree-node-dot {
    transform: scale(1.12);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  }

  .tree-node-ext,
  .tree-node-actions {
    opacity: 1;
    transform: translate(0, 0);
  }
}
</style>
