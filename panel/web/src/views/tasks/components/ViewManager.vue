<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { taskViewApi, type TaskView, type TaskViewFilter, type TaskViewSortRule } from '@/api/taskView'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Close, Edit, Setting } from '@element-plus/icons-vue'
import { useResponsive } from '@/composables/useResponsive'
import ViewManagementDialog from './ViewManagementDialog.vue'

const emit = defineEmits<{
  'view-change': [filters: TaskViewFilter[], sortRules: TaskViewSortRule[]]
}>()

const { dialogFullscreen } = useResponsive()
const views = ref<TaskView[]>([])
const activeViewId = ref<number | null>(null)
const showDialog = ref(false)
const isEditMode = ref(false)
const editingViewId = ref<number | null>(null)
const showManagementDialog = ref(false)

const visibleViews = computed(() => views.value.filter(view => !view.hidden))

const filterFields = [
  { value: 'command', label: '命令' },
  { value: 'name', label: '名称' },
  { value: 'cron_expression', label: '定时规则' },
  { value: 'status', label: '状态' },
  { value: 'labels', label: '标签' },
  { value: 'subscription', label: '订阅' }
]

const statusOptions = [
  { label: '已启用 / 空闲中', value: '1' },
  { label: '已禁用', value: '0' },
  { label: '运行中', value: '2' },
  { label: '排队中', value: '0.5' },
]

const filterOperators = [
  { value: 'contains', label: '包含' },
  { value: 'not_contains', label: '不包含' },
  { value: 'equals', label: '等于' },
  { value: 'not_equals', label: '不等于' }
]

const sortDirections = [
  { value: 'asc', label: '升序' },
  { value: 'desc', label: '降序' }
]

const editForm = ref({
  name: '',
  filters: [{ field: 'command', operator: 'contains', value: '' }] as TaskViewFilter[],
  sortRules: [] as TaskViewSortRule[]
})

async function loadViews() {
  try {
    views.value = await taskViewApi.list()
  } catch {
    views.value = []
  }
  // If the currently active view was just hidden from the management dialog,
  // fall back to the 全部 tab so the user isn't looking at a tab they can no
  // longer see.
  if (activeViewId.value !== null) {
    const current = views.value.find(v => v.id === activeViewId.value)
    if (!current || current.hidden) {
      selectView(null)
    }
  }
}

function openManagementDialog() {
  showManagementDialog.value = true
}

async function handleManagementSaved() {
  await loadViews()
}

function selectView(viewId: number | null) {
  activeViewId.value = viewId
  if (!viewId) {
    emit('view-change', [], [])
    return
  }
  const view = views.value.find(v => v.id === viewId)
  if (view) {
    try {
      const filters = JSON.parse(view.filters || '[]')
      const sortRules = JSON.parse(view.sort_rules || '[]')
      emit('view-change', filters, sortRules)
    } catch {
      emit('view-change', [], [])
    }
  }
}

function openCreateDialog() {
  isEditMode.value = false
  editingViewId.value = null
  editForm.value = {
    name: '',
    filters: [{ field: 'command', operator: 'contains', value: '' }],
    sortRules: []
  }
  showDialog.value = true
}

function openEditDialog(view: TaskView) {
  isEditMode.value = true
  editingViewId.value = view.id
  let filters: TaskViewFilter[] = []
  let sortRules: TaskViewSortRule[] = []
  try {
    filters = JSON.parse(view.filters || '[]')
  } catch { /* ignore */ }
  try {
    sortRules = JSON.parse(view.sort_rules || '[]')
  } catch { /* ignore */ }
  if (filters.length === 0) {
    filters = [{ field: 'command', operator: 'contains', value: '' }]
  }
  editForm.value = {
    name: view.name,
    filters,
    sortRules
  }
  showDialog.value = true
}

function addFilter() {
  editForm.value.filters.push({ field: 'command', operator: 'contains', value: '' })
}

function removeFilter(index: number) {
  editForm.value.filters.splice(index, 1)
}

function addSortRule() {
  editForm.value.sortRules.push({ field: 'name', direction: 'asc' })
}

function removeSortRule(index: number) {
  editForm.value.sortRules.splice(index, 1)
}

function isStatusField(filter: TaskViewFilter) {
  return filter.field === 'status'
}

async function handleSave() {
  if (!editForm.value.name.trim()) {
    ElMessage.warning('请输入视图名称')
    return
  }
  const validFilters = editForm.value.filters.filter(f => f.value.trim() !== '')
  if (validFilters.length === 0) {
    ElMessage.warning('请至少添加一个有效的筛选条件')
    return
  }
  try {
    const payload = {
      name: editForm.value.name,
      filters: JSON.stringify(validFilters),
      sort_rules: JSON.stringify(editForm.value.sortRules)
    }
    if (isEditMode.value && editingViewId.value) {
      await taskViewApi.update(editingViewId.value, payload)
      ElMessage.success('视图更新成功')
      // If editing the active view, re-apply its filters
      if (activeViewId.value === editingViewId.value) {
        emit('view-change', validFilters, editForm.value.sortRules)
      }
    } else {
      await taskViewApi.create(payload)
      ElMessage.success('视图创建成功')
    }
    showDialog.value = false
    await loadViews()
  } catch {
    ElMessage.error(isEditMode.value ? '更新失败' : '创建失败')
  }
}

async function handleDelete(viewId: number) {
  try {
    await ElMessageBox.confirm('确认删除该视图吗？', '删除确认', { type: 'warning' })
    await doDeleteView(viewId)
  } catch {}
}

async function doDeleteView(viewId: number) {
  try {
    await taskViewApi.delete(viewId)
    ElMessage.success('已删除')
    if (activeViewId.value === viewId) {
      selectView(null)
    }
    await loadViews()
  } catch {}
}

onMounted(loadViews)

defineExpose({ loadViews })
</script>

<template>
  <div class="view-manager">
    <div class="view-tabs">
      <el-button
        :type="activeViewId === null ? 'primary' : 'default'"
        size="small"
        @click="selectView(null)"
      >
        全部
      </el-button>
      <el-button
        v-for="view in visibleViews"
        :key="view.id"
        :type="activeViewId === view.id ? 'primary' : 'default'"
        size="small"
        class="view-tab-btn"
        @click="selectView(view.id)"
      >
        {{ view.name }}
      </el-button>
      <el-button size="small" @click="openCreateDialog">
        <el-icon><Plus /></el-icon>
      </el-button>
      <el-tooltip v-if="views.length > 0" content="视图管理" placement="top">
        <el-button size="small" @click="openManagementDialog">
          <el-icon><Setting /></el-icon>
        </el-button>
      </el-tooltip>
    </div>

    <ViewManagementDialog
      v-model="showManagementDialog"
      :views="views"
      @saved="handleManagementSaved"
      @edit="openEditDialog"
      @delete="doDeleteView"
    />

    <el-dialog
      v-model="showDialog"
      :title="isEditMode ? '编辑视图' : '创建视图'"
      width="600px"
      :fullscreen="dialogFullscreen"
      :lock-scroll="false"
    >
      <el-form :label-width="dialogFullscreen ? 'auto' : '90px'" :label-position="dialogFullscreen ? 'top' : 'right'">
        <el-form-item label="视图名称" required>
          <el-input v-model="editForm.name" placeholder="请输入视图名称" />
        </el-form-item>

        <el-form-item label="筛选条件" required>
          <div class="filter-list">
            <div v-for="(filter, index) in editForm.filters" :key="index" class="filter-row">
              <el-select v-model="filter.field" style="width: 120px" size="small" @change="filter.value = ''">
                <el-option v-for="f in filterFields" :key="f.value" :label="f.label" :value="f.value" />
              </el-select>
              <el-select v-model="filter.operator" style="width: 100px" size="small">
                <el-option v-for="op in filterOperators" :key="op.value" :label="op.label" :value="op.value" />
              </el-select>
              <el-select
                v-if="isStatusField(filter)"
                v-model="filter.value"
                placeholder="选择状态"
                size="small"
                style="flex: 1"
              >
                <el-option v-for="opt in statusOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
              </el-select>
              <el-input v-else v-model="filter.value" placeholder="请输入内容" size="small" style="flex: 1" />
              <el-button v-if="editForm.filters.length > 1" :icon="Delete" size="small" circle @click="removeFilter(index)" />
            </div>
            <el-button size="small" type="primary" link @click="addFilter">+ 新增筛选条件</el-button>
          </div>
        </el-form-item>

        <el-form-item label="排序方式">
          <div class="filter-list">
            <div v-for="(rule, index) in editForm.sortRules" :key="index" class="filter-row">
              <el-select v-model="rule.field" style="width: 120px" size="small">
                <el-option v-for="f in filterFields" :key="f.value" :label="f.label" :value="f.value" />
              </el-select>
              <el-select v-model="rule.direction" style="width: 100px" size="small">
                <el-option v-for="d in sortDirections" :key="d.value" :label="d.label" :value="d.value" />
              </el-select>
              <el-button :icon="Delete" size="small" circle @click="removeSortRule(index)" />
            </div>
            <el-button size="small" type="primary" link @click="addSortRule">+ 新增排序方式</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">{{ isEditMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.view-manager {
  margin-bottom: 12px;
}

.view-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.view-tab-btn {
  min-width: 64px;
  padding: 0 16px;
  height: 32px;
  font-size: 13px;
}

.filter-list {
  width: 100%;
}

.filter-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

@media (max-width: 768px) {
  .filter-row {
    flex-wrap: wrap;
  }
}
</style>
