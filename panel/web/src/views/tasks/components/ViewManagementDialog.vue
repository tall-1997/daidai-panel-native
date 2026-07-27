<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Rank, View, Hide, Edit, Delete } from '@element-plus/icons-vue'
import { taskViewApi, type TaskView } from '@/api/taskView'
import { useResponsive } from '@/composables/useResponsive'

interface ManagedView extends TaskView {
  // Local working copy of `hidden` that may diverge from the saved value until
  // the user clicks 保存. The original `hidden` is kept on the TaskView shape.
  _hidden: boolean
}

const props = defineProps<{
  modelValue: boolean
  views: TaskView[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
  edit: [view: TaskView]
  delete: [viewId: number]
}>()

const { dialogFullscreen } = useResponsive()
const saving = ref(false)
const managed = ref<ManagedView[]>([])
const listContainer = ref<HTMLElement | null>(null)
let sortableInstance: any = null
let sortableLoader: Promise<any> | null = null

const hiddenCount = computed(() => managed.value.filter(v => v._hidden).length)
const visibleCount = computed(() => managed.value.length - hiddenCount.value)

function cloneToManaged(source: TaskView[]): ManagedView[] {
  return source.map(v => ({ ...v, _hidden: Boolean(v.hidden) }))
}

function loadSortable() {
  if (!sortableLoader) {
    sortableLoader = import('sortablejs').then(mod => mod.default)
  }
  return sortableLoader
}

async function initSortable() {
  teardownSortable()
  if (!listContainer.value) return
  try {
    const Sortable = await loadSortable()
    sortableInstance = Sortable.create(listContainer.value, {
      animation: 150,
      handle: '.view-drag-handle',
      ghostClass: 'view-row-ghost',
      chosenClass: 'view-row-chosen',
      dragClass: 'view-row-dragging',
      forceFallback: true,
      onEnd: (evt: any) => {
        const { oldIndex, newIndex } = evt
        if (oldIndex == null || newIndex == null || oldIndex === newIndex) return
        const [moved] = managed.value.splice(oldIndex, 1)
        if (!moved) return
        managed.value.splice(newIndex, 0, moved)
      }
    })
  } catch (err) {
    console.warn('failed to init sortable for view manager', err)
  }
}

function teardownSortable() {
  if (sortableInstance) {
    sortableInstance.destroy()
    sortableInstance = null
  }
}

function toggleHidden(view: ManagedView) {
  view._hidden = !view._hidden
}

function handleEdit(view: ManagedView) {
  emit('edit', view as TaskView)
  emit('update:modelValue', false)
}

async function handleDelete(view: ManagedView) {
  try {
    await ElMessageBox.confirm(`确认删除视图「${view.name}」吗？`, '删除确认', { type: 'warning' })
    emit('delete', view.id)
  } catch {
    // cancelled
  }
}

async function handleSave() {
  if (managed.value.length === 0) {
    emit('update:modelValue', false)
    return
  }
  saving.value = true
  try {
    // Dense re-numbering keeps sort_order contiguous and mirrors the
    // visible list order.
    const payload = managed.value.map((view, index) => ({
      id: view.id,
      sort_order: (index + 1) * 10,
      hidden: view._hidden
    }))
    await taskViewApi.reorder(payload)
    ElMessage.success('视图设置已保存')
    emit('saved')
    emit('update:modelValue', false)
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || err?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function handleClose(visible: boolean) {
  emit('update:modelValue', visible)
}

watch(
  () => props.modelValue,
  async (open) => {
    if (open) {
      managed.value = cloneToManaged(props.views)
      await nextTick()
      await initSortable()
    } else {
      teardownSortable()
    }
  }
)

watch(
  () => props.views,
  (next) => {
    if (props.modelValue) {
      managed.value = cloneToManaged(next)
    }
  },
  { deep: true }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="视图管理"
    width="520px"
    :fullscreen="dialogFullscreen"
    :lock-scroll="false"
    :close-on-click-modal="false"
    @update:model-value="handleClose"
  >
    <div v-if="managed.length === 0" class="view-manager-empty">
      还没有任何自定义视图，先从标签栏右侧的「+」创建一个试试。
    </div>
    <template v-else>
      <div class="view-manager-hint">
        拖动左侧把手调整顺序，右侧开关控制是否在标签栏展示。
        <span class="view-manager-counts">共 {{ managed.length }} 个 · 显示 {{ visibleCount }} · 隐藏 {{ hiddenCount }}</span>
      </div>
      <div ref="listContainer" class="view-manager-list">
        <div
          v-for="view in managed"
          :key="view.id"
          class="view-manager-row"
          :class="{ 'is-hidden': view._hidden }"
          :data-id="view.id"
        >
          <span class="view-drag-handle" :title="'拖动排序'">
            <el-icon><Rank /></el-icon>
          </span>
          <span class="view-row-name">{{ view.name }}</span>
          <div class="view-row-actions">
            <el-tooltip content="编辑" placement="top">
              <el-button size="small" type="primary" plain circle @click="handleEdit(view)">
                <el-icon><Edit /></el-icon>
              </el-button>
            </el-tooltip>
            <el-tooltip content="删除" placement="top">
              <el-button size="small" type="danger" plain circle @click="handleDelete(view)">
                <el-icon><Delete /></el-icon>
              </el-button>
            </el-tooltip>
            <el-tooltip :content="view._hidden ? '在标签栏隐藏' : '在标签栏显示'" placement="top">
              <el-switch
                :model-value="!view._hidden"
                inline-prompt
                :active-icon="View"
                :inactive-icon="Hide"
                @update:model-value="toggleHidden(view)"
              />
            </el-tooltip>
          </div>
        </div>
      </div>
    </template>

    <template #footer>
      <el-button @click="handleClose(false)">取消</el-button>
      <el-button
        type="primary"
        :loading="saving"
        :disabled="managed.length === 0"
        @click="handleSave"
      >
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.view-manager-empty {
  padding: 24px 0;
  text-align: center;
  color: var(--el-text-color-secondary);
}

.view-manager-hint {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 12px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.view-manager-counts {
  color: var(--el-text-color-placeholder);
}

.view-manager-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 420px;
  overflow-y: auto;
  padding-right: 4px;
}

.view-manager-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  background: var(--el-fill-color-lighter);
  border: 1px solid transparent;
  border-radius: 6px;
  transition: border-color 0.15s, background 0.15s, opacity 0.15s;

  &:hover {
    border-color: var(--el-border-color);
  }

  &.is-hidden {
    opacity: 0.55;
  }
}

.view-drag-handle {
  cursor: grab;
  color: var(--el-text-color-placeholder);
  display: inline-flex;
  align-items: center;
  padding: 2px;

  &:active {
    cursor: grabbing;
  }
}

.view-row-name {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  word-break: break-word;
}

.view-row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.view-row-ghost {
  opacity: 0.4;
  background: var(--el-color-primary-light-9);
}

.view-row-chosen {
  background: var(--el-fill-color);
}

.view-row-dragging {
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.12);
}
</style>
