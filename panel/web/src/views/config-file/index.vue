<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, CopyDocument, Document, Refresh } from '@element-plus/icons-vue'
import { configScriptApi } from '@/api/system'
import MonacoEditor from '@/components/MonacoEditor.vue'
import { copyText } from '@/utils/clipboard'

const content = ref('')
const savedContent = ref('')
const configPath = ref('config.sh')
const loading = ref(false)
const saving = ref(false)
const copying = ref(false)

const hasChanged = computed(() => content.value !== savedContent.value)
const lineCount = computed(() => content.value === '' ? 0 : content.value.split(/\r\n|\n|\r/).length)
const byteSizeLabel = computed(() => {
  const bytes = new Blob([content.value]).size
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
})

onMounted(() => {
  void loadConfigScript()
})

async function loadConfigScript(showSuccess = false) {
  loading.value = true
  try {
    const res = await configScriptApi.get()
    content.value = res.content ?? ''
    savedContent.value = res.content ?? ''
    configPath.value = res.path || 'config.sh'
    if (showSuccess) {
      ElMessage.success('配置文件已刷新')
    }
  } catch {
    content.value = ''
    savedContent.value = ''
    ElMessage.error('加载配置文件失败')
  } finally {
    loading.value = false
  }
}

async function saveConfigScript() {
  saving.value = true
  try {
    await configScriptApi.save(content.value)
    savedContent.value = content.value
    ElMessage.success('配置文件已保存')
  } catch {
    ElMessage.error('保存配置文件失败')
  } finally {
    saving.value = false
  }
}

async function copyConfigScript() {
  copying.value = true
  try {
    await copyText(content.value)
    ElMessage.success('配置文件内容已复制')
  } catch {
    ElMessage.error('复制失败，请检查浏览器权限或站点访问方式')
  } finally {
    copying.value = false
  }
}
</script>

<template>
  <div class="config-file-page dd-scroll-page dd-page-hide-heading">
    <div class="page-header">
      <div>
        <h2 class="page-title-with-icon">
          <el-icon><Document /></el-icon>
          <span>配置文件</span>
        </h2>
        <p class="page-subtitle">
          集中维护 <code>config.sh</code>，脚本运行前会自动加载这里的共享配置。
        </p>
      </div>
      <div class="header-actions">
        <el-tag v-if="hasChanged" type="warning" effect="plain">有未保存修改</el-tag>
        <el-tag v-else type="success" effect="plain">
          <el-icon><Check /></el-icon>
          已保存
        </el-tag>
      </div>
    </div>

    <div class="config-layout">
      <el-card class="editor-card" shadow="never" v-loading="loading">
        <template #header>
          <div class="editor-card__header">
            <div>
              <div class="editor-card__title">{{ configPath }}</div>
              <div class="editor-card__desc">按 Shell 语法编辑，每行一个变量或注释。</div>
            </div>
            <div class="editor-card__actions">
              <el-button :loading="loading" @click="loadConfigScript(true)">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
              <el-button :loading="copying" @click="copyConfigScript">
                <el-icon><CopyDocument /></el-icon>
                复制
              </el-button>
              <el-button
                type="primary"
                :loading="saving"
                :disabled="loading || !hasChanged"
                @click="saveConfigScript"
              >
                保存
              </el-button>
            </div>
          </div>
        </template>

        <!-- Monaco 初始化较重，等接口返回后再挂载，避免先闪一下空内容。 -->
        <MonacoEditor
          v-if="!loading"
          v-model="content"
          language="shell"
          min-height="560px"
        />
        <div v-else class="editor-placeholder">
          正在读取配置文件...
        </div>
      </el-card>

      <aside class="side-panel">
        <el-card class="info-card" shadow="never">
          <template #header>
            <span>文件说明</span>
          </template>
          <div class="info-list">
            <div class="info-row">
              <span>文件名</span>
              <code>{{ configPath }}</code>
            </div>
            <div class="info-row">
              <span>当前行数</span>
              <strong>{{ lineCount }}</strong>
            </div>
            <div class="info-row">
              <span>内容大小</span>
              <strong>{{ byteSizeLabel }}</strong>
            </div>
          </div>
        </el-card>

        <el-card class="tips-card" shadow="never">
          <template #header>
            <span>写法提示</span>
          </template>
          <ul class="tips-list">
            <li><code>KEY=VALUE</code>：写入普通变量。</li>
            <li><code>export KEY="VALUE"</code>：写入并导出变量。</li>
            <li><code>#</code> 开头表示注释，可记录用途。</li>
            <li>环境变量页面里的同名变量优先级更高。</li>
          </ul>
        </el-card>
      </aside>
    </div>
  </div>
</template>

<style scoped lang="scss">
.config-file-page {
  padding: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 18px;

  .page-title-with-icon {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 22px;
    font-weight: 700;
    color: var(--el-text-color-primary);
    line-height: 1.3;
  }

  .page-subtitle {
    margin: 6px 0 0;
    font-size: 13px;
    color: var(--el-text-color-secondary);
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
}

.config-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 16px;
  align-items: start;
}

.editor-card,
.info-card,
.tips-card {
  border-radius: var(--dd-card-radius);
  border-color: var(--el-border-color-lighter);
  box-shadow: var(--dd-shadow-card);
}

.editor-card {
  overflow: hidden;

  :deep(.el-card__header) {
    padding: 16px 18px;
  }

  :deep(.el-card__body) {
    padding: 0;
  }
}

.editor-card__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.editor-card__title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.editor-card__desc {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.editor-card__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.editor-placeholder {
  min-height: 560px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-lighter);
  font-size: 14px;
}

.side-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.info-card,
.tips-card {
  :deep(.el-card__header) {
    padding: 14px 16px;
    font-weight: 700;
  }

  :deep(.el-card__body) {
    padding: 16px;
  }
}

.info-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.info-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: var(--el-text-color-secondary);

  code,
  strong {
    color: var(--el-text-color-primary);
    font-weight: 600;
  }
}

.tips-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.6;
}

code {
  padding: 1px 5px;
  border-radius: var(--dd-radius-sm);
  background: var(--el-fill-color-lighter);
  color: var(--el-text-color-primary);
  font-family: var(--dd-font-mono);
  font-size: 12px;
}

@media (max-width: 1080px) {
  .config-layout {
    grid-template-columns: 1fr;
  }

  .side-panel {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
  }

  .editor-card__header,
  .editor-card__actions {
    align-items: stretch;
  }

  .editor-card__actions {
    width: 100%;

    .el-button {
      flex: 1;
    }
  }

  .side-panel {
    grid-template-columns: 1fr;
  }
}
</style>
