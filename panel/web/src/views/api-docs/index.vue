<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Search } from '@element-plus/icons-vue'
import type { ApiCategory, ApiEndpoint } from './apiData'

type ApiDocsModule = typeof import('./apiData')

const apiDataModule = ref<ApiDocsModule | null>(null)
const apiCategories = ref<ApiCategory[]>([])
const apiLoading = ref(true)
const selectedId = ref('')
const searchText = ref('')
const codeTab = ref('Shell')
const helperTab = ref('JavaScript')
const copiedKey = ref('')
const mobileMenuOpen = ref(false)

const filteredCategories = computed(() => {
  if (!searchText.value.trim()) return apiCategories.value
  const kw = searchText.value.toLowerCase()
  return apiCategories.value
    .map(cat => ({
      ...cat,
      endpoints: cat.endpoints.filter(
        ep => ep.title.toLowerCase().includes(kw) ||
          ep.path.toLowerCase().includes(kw) ||
          ep.method.toLowerCase().includes(kw)
      ),
    }))
    .filter(cat => cat.endpoints.length > 0)
})

const currentEndpoint = computed<ApiEndpoint | null>(() => {
  for (const cat of filteredCategories.value) {
    for (const ep of cat.endpoints) {
      if (ep.id === selectedId.value) return ep
    }
  }
  return filteredCategories.value[0]?.endpoints[0] || null
})

const currentCategory = computed(() => {
  for (const cat of filteredCategories.value) {
    if (cat.endpoints.some(ep => ep.id === selectedId.value)) return cat
  }
  return filteredCategories.value[0] || null
})

const codeExamples = computed<Record<string, string>>(() => {
  if (!apiDataModule.value || !currentEndpoint.value) return {}
  return apiDataModule.value.generateCodeExamples(currentEndpoint.value)
})
const apiBaseOrigin = computed(() => {
  if (apiDataModule.value) {
    return apiDataModule.value.getApiBaseOrigin()
  }
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'http://localhost:5701'
})

const totalEndpoints = computed(() =>
  apiCategories.value.reduce((sum, cat) => sum + cat.endpoints.length, 0))

onMounted(async () => {
  const module = await import('./apiData')
  apiDataModule.value = module
  apiCategories.value = module.apiCategories
  selectedId.value = module.apiCategories[0]?.endpoints[0]?.id || ''
  apiLoading.value = false
})

watch(filteredCategories, (categories) => {
  const firstVisibleId = categories[0]?.endpoints[0]?.id || ''
  if (!firstVisibleId) {
    selectedId.value = ''
    return
  }

  const selectionStillVisible = categories.some(cat =>
    cat.endpoints.some(ep => ep.id === selectedId.value)
  )
  if (!selectionStillVisible) {
    selectedId.value = firstVisibleId
  }
}, { immediate: true })

watch(currentEndpoint, endpoint => {
  const names = Object.keys(endpoint?.helperExamples || {})
  helperTab.value = names[0] || 'JavaScript'
}, { immediate: true })

function handleSelect(id: string) {
  selectedId.value = id
  mobileMenuOpen.value = false
}

function handleCopy(text: string, key: string) {
  if (!text) return
  const doCopy = () => {
    copiedKey.value = key
    setTimeout(() => copiedKey.value = '', 1500)
  }
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(doCopy)
  } else {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    doCopy()
  }
}

function methodClass(method: string) {
  const m = method.toLowerCase()
  if (m === 'get') return 'method-get'
  if (m === 'post') return 'method-post'
  if (m === 'put') return 'method-put'
  if (m === 'delete') return 'method-delete'
  return ''
}


</script>

<template>
  <div class="api-docs-page dd-fixed-page">
    <div class="page-header">
      <h3 class="page-title">
        开发接口文档
      </h3>
      <el-button class="mobile-menu-btn" @click="mobileMenuOpen = true">
        <el-icon><Menu /></el-icon>
        接口列表
      </el-button>
    </div>

    <el-drawer v-model="mobileMenuOpen" title="接口列表" direction="ltr" size="280px" :z-index="2000">
      <div class="sider-search">
        <el-input v-model="searchText" placeholder="搜索接口..." clearable :prefix-icon="Search" />
        <div class="sider-count">共 {{ totalEndpoints }} 个接口</div>
      </div>
      <div class="sider-menu">
        <template v-for="cat in filteredCategories" :key="cat.key">
          <div class="menu-group-title">{{ cat.label }} ({{ cat.endpoints.length }})</div>
          <div
            v-for="ep in cat.endpoints" :key="ep.id"
            class="menu-item" :class="{ active: selectedId === ep.id }"
            @click="handleSelect(ep.id)"
          >
            <span class="method-badge method-badge-sm" :class="methodClass(ep.method)">{{ ep.method }}</span>
            <span class="menu-item-text">{{ ep.title }}</span>
          </div>
        </template>
      </div>
    </el-drawer>

    <div class="api-layout">
      <div class="api-sider">
        <div class="sider-search">
          <el-input v-model="searchText" placeholder="搜索接口..." clearable :prefix-icon="Search" />
          <div class="sider-count">共 {{ totalEndpoints }} 个接口</div>
        </div>
        <div class="sider-menu">
          <template v-for="cat in filteredCategories" :key="cat.key">
            <div class="menu-group-title">{{ cat.label }} ({{ cat.endpoints.length }})</div>
            <div
              v-for="ep in cat.endpoints" :key="ep.id"
              class="menu-item" :class="{ active: selectedId === ep.id }"
              @click="handleSelect(ep.id)"
            >
              <span class="method-badge method-badge-sm" :class="methodClass(ep.method)">{{ ep.method }}</span>
              <span class="menu-item-text">{{ ep.title }}</span>
            </div>
          </template>
        </div>
      </div>

      <div class="api-content">
        <template v-if="!apiLoading && currentEndpoint && currentCategory">
          <div class="api-breadcrumb">{{ currentCategory.label }} / {{ currentEndpoint.title }}</div>
          <h2 class="api-endpoint-title">{{ currentEndpoint.title }}</h2>

          <div class="url-bar">
            <span class="method-badge" :class="methodClass(currentEndpoint.method)">{{ currentEndpoint.method }}</span>
            <span class="url-path">{{ apiBaseOrigin }}{{ currentEndpoint.path }}</span>
            <el-tooltip :content="copiedKey === 'url' ? '已复制' : '复制 URL'" placement="top">
              <el-button text size="small" @click="handleCopy(`${apiBaseOrigin}${currentEndpoint.path}`, 'url')">
                <el-icon><DocumentCopy /></el-icon>
              </el-button>
            </el-tooltip>
          </div>

          <p class="api-description">{{ currentEndpoint.description }}</p>

          <div class="auth-banner" :class="currentEndpoint.auth === 'jwt' ? 'auth-jwt' : 'auth-none'">
            <el-icon v-if="currentEndpoint.auth === 'jwt'"><Lock /></el-icon>
            <el-icon v-else><Unlock /></el-icon>
            <span v-if="currentEndpoint.auth === 'jwt'">
              使用 JWT Token 鉴权，请在请求头中添加 <code>Authorization: Bearer &lt;TOKEN&gt;</code>
            </span>
            <span v-else>此接口无需鉴权即可访问</span>
          </div>

        <el-card
          v-if="currentEndpoint.pathParams?.length || currentEndpoint.queryParams?.length || currentEndpoint.bodyParams?.length"
          shadow="never" class="api-card"
        >
          <template #header>
            <div class="section-title">请求参数</div>
          </template>

          <template v-if="currentEndpoint.pathParams?.length">
            <div class="param-section-header">Path 参数</div>
            <el-table :data="currentEndpoint.pathParams" size="small" :show-header="true" :border="false">
              <el-table-column prop="name" label="参数名" width="150">
                <template #default="{ row }"><code class="param-name">{{ row.name }}</code></template>
              </el-table-column>
              <el-table-column prop="type" label="类型" width="90">
                <template #default="{ row }"><el-tag size="small" type="info" round>{{ row.type }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="required" label="必填" width="70">
                <template #default="{ row }">
                  <el-tag v-if="row.required" size="small" type="danger" round>必填</el-tag>
                  <el-tag v-else size="small" round>可选</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="说明" />
              <el-table-column prop="example" label="示例" width="130">
                <template #default="{ row }"><span class="param-example">{{ row.example || '-' }}</span></template>
              </el-table-column>
            </el-table>
          </template>

          <template v-if="currentEndpoint.queryParams?.length">
            <div class="param-section-header">Query 参数</div>
            <el-table :data="currentEndpoint.queryParams" size="small" :show-header="true" :border="false">
              <el-table-column prop="name" label="参数名" width="150">
                <template #default="{ row }"><code class="param-name">{{ row.name }}</code></template>
              </el-table-column>
              <el-table-column prop="type" label="类型" width="90">
                <template #default="{ row }"><el-tag size="small" type="info" round>{{ row.type }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="required" label="必填" width="70">
                <template #default="{ row }">
                  <el-tag v-if="row.required" size="small" type="danger" round>必填</el-tag>
                  <el-tag v-else size="small" round>可选</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="说明" />
              <el-table-column prop="example" label="示例" width="130">
                <template #default="{ row }"><span class="param-example">{{ row.example || '-' }}</span></template>
              </el-table-column>
            </el-table>
          </template>

          <template v-if="currentEndpoint.bodyParams?.length">
            <div class="param-section-header">Body 参数（JSON）</div>
            <el-table :data="currentEndpoint.bodyParams" size="small" :show-header="true" :border="false">
              <el-table-column prop="name" label="参数名" width="150">
                <template #default="{ row }"><code class="param-name">{{ row.name }}</code></template>
              </el-table-column>
              <el-table-column prop="type" label="类型" width="90">
                <template #default="{ row }"><el-tag size="small" type="info" round>{{ row.type }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="required" label="必填" width="70">
                <template #default="{ row }">
                  <el-tag v-if="row.required" size="small" type="danger" round>必填</el-tag>
                  <el-tag v-else size="small" round>可选</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="说明" />
              <el-table-column prop="example" label="示例" width="130">
                <template #default="{ row }"><span class="param-example">{{ row.example || '-' }}</span></template>
              </el-table-column>
            </el-table>
          </template>
        </el-card>

        <el-card shadow="never" class="api-card">
          <template #header>
            <div class="section-title">请求示例代码</div>
          </template>
          <el-tabs v-model="codeTab" class="code-tabs">
            <el-tab-pane v-for="lang in Object.keys(codeExamples)" :key="lang" :label="lang" :name="lang">
              <div class="code-block-wrapper">
                <el-tooltip :content="copiedKey === `code-${lang}` ? '已复制' : '复制代码'" placement="top">
                  <el-button
                    class="code-copy-btn"
                    size="small"
                    type="primary"
                    plain
                    @click="handleCopy(codeExamples[lang] || '', `code-${lang}`)"
                  >
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
                <pre class="code-block">{{ codeExamples[lang] }}</pre>
              </div>
            </el-tab-pane>
          </el-tabs>
        </el-card>

        <el-card
          v-if="currentEndpoint.helperExamples && Object.keys(currentEndpoint.helperExamples).length"
          shadow="never"
          class="api-card"
        >
          <template #header>
            <div class="section-title">脚本示例</div>
          </template>
          <el-tabs v-model="helperTab" class="code-tabs">
            <el-tab-pane
              v-for="lang in Object.keys(currentEndpoint.helperExamples)"
              :key="lang"
              :label="lang"
              :name="lang"
            >
              <div class="code-block-wrapper">
                <el-tooltip :content="copiedKey === `helper-${lang}` ? '已复制' : '复制代码'" placement="top">
                  <el-button
                    class="code-copy-btn"
                    size="small"
                    type="primary"
                    plain
                    @click="handleCopy(currentEndpoint.helperExamples?.[lang] || '', `helper-${lang}`)"
                  >
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
                <pre class="code-block">{{ currentEndpoint.helperExamples?.[lang] }}</pre>
              </div>
            </el-tab-pane>
          </el-tabs>
        </el-card>

        <el-card v-if="currentEndpoint.responseExample" shadow="never" class="api-card">
          <template #header>
            <div class="section-title">返回响应</div>
          </template>

          <div class="response-status">
            <span class="status-badge status-200">
              <span class="status-dot" />
              200 成功
            </span>
            <span class="response-type">application/json</span>
          </div>

          <div class="response-section">
            <div v-if="currentEndpoint.responseFields?.length" class="response-fields">
              <div class="response-label">Body 字段</div>
              <el-table :data="currentEndpoint.responseFields" size="small" :border="false">
                <el-table-column prop="name" label="字段" width="140">
                  <template #default="{ row }"><code class="param-name">{{ row.name }}</code></template>
                </el-table-column>
                <el-table-column prop="type" label="类型" width="80">
                  <template #default="{ row }"><el-tag size="small" type="info" round>{{ row.type }}</el-tag></template>
                </el-table-column>
                <el-table-column prop="description" label="说明" />
              </el-table>
            </div>

            <div class="response-example">
              <div class="response-label">响应示例</div>
              <div class="code-block-wrapper">
                <el-tooltip :content="copiedKey === 'resp' ? '已复制' : '复制'" placement="top">
                  <el-button
                    class="code-copy-btn"
                    size="small"
                    type="primary"
                    plain
                    @click="handleCopy(currentEndpoint.responseExample || '', 'resp')"
                  >
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
                <pre class="code-block" style="max-height: 360px">{{ currentEndpoint.responseExample }}</pre>
              </div>
            </div>
          </div>
        </el-card>
        </template>
        <el-empty v-else description="正在加载接口文档..." />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.api-docs-page {
  min-height: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;

  .page-title {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--el-text-color-primary);
  }

  .mobile-menu-btn {
    display: none;
  }
}

.api-layout {
  display: flex;
  gap: 16px;
  flex: 1 1 auto;
  min-height: 0;
}

.api-sider {
  width: 280px;
  flex-shrink: 0;
  background: var(--el-bg-color);
  // 圆角对齐卡片令牌、阴影引用全局静置卡片阴影，使侧栏与右侧文档卡观感统一
  border-radius: var(--dd-card-radius);
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: var(--dd-shadow-card);
  overflow: hidden;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.sider-search {
  padding: 16px 14px 8px;
  flex-shrink: 0;

  .sider-count {
    padding: 8px 2px 0;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}

.sider-menu {
  overflow-y: auto;
  padding-bottom: 12px;
  flex: 1;
}

.menu-group-title {
  padding: 12px 20px 4px;
  font-weight: 600;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--el-text-color-secondary);
}

.menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  margin: 2px 8px;
  border-radius: var(--dd-radius-sm);
  cursor: pointer;
  // 选中/hover 过渡走令牌（快 + 标准缓动），与全站侧边菜单观感一致
  transition:
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    color var(--dd-motion-fast) var(--dd-ease-standard),
    box-shadow var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover:not(.active) {
    background: var(--el-fill-color-light);
  }

  // 选中态对齐全局菜单：品牌色浅底渐变 + 左侧品牌色指示条
  &.active {
    background: linear-gradient(
      135deg,
      var(--el-color-primary-light-8),
      var(--el-color-primary-light-9)
    );
    box-shadow: inset 3px 0 0 var(--el-color-primary);

    .menu-item-text {
      color: var(--el-color-primary);
      font-weight: 600;
    }
  }

  .menu-item-text {
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--el-text-color-primary);
  }
}

.method-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 52px;
  padding: 1px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  font-family: var(--dd-font-mono);
  color: #fff;
  line-height: 20px;
}

.method-badge-sm {
  min-width: 40px;
  padding: 0 5px;
  font-size: 10px;
  line-height: 18px;
  border-radius: 3px;
}

.method-get { background: linear-gradient(135deg, #52c41a, #73d13d); }
.method-post { background: linear-gradient(135deg, #1677ff, #4096ff); }
.method-put { background: linear-gradient(135deg, #fa8c16, #ffa940); }
.method-delete { background: linear-gradient(135deg, #ff4d4f, #ff7875); }

.api-content {
  flex: 1;
  background: var(--el-bg-color);
  // 圆角对齐卡片令牌、阴影引用全局静置卡片阴影
  border-radius: var(--dd-card-radius);
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: var(--dd-shadow-card);
  padding: 28px 32px;
  overflow: auto;
  min-width: 0;
}

.api-breadcrumb {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
  letter-spacing: 0.3px;
}

.api-endpoint-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin: 0 0 16px 0;
  line-height: 1.3;
}

.url-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 20px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--dd-radius-sm);
  margin-bottom: 24px;
  flex-wrap: wrap;
  // 过渡走令牌（标准缓动），hover 阴影引用全局静置卡片阴影（暗色自动适配）
  transition:
    border-color var(--dd-motion-normal) var(--dd-ease-standard),
    box-shadow var(--dd-motion-normal) var(--dd-ease-standard);

  &:hover {
    border-color: var(--el-border-color);
    box-shadow: var(--dd-shadow-card);
  }
}

.url-path {
  font-family: var(--dd-font-mono);
  font-size: 14px;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.api-description {
  font-size: 14px;
  color: var(--el-text-color-regular);
  line-height: 1.8;
  margin-bottom: 20px;
}

.auth-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border-radius: var(--dd-radius-sm);
  margin-bottom: 20px;
  font-size: 13px;

  code {
    font-size: 12px;
    // 用 color-mix 基于当前文字色生成半透明底，明暗双主题都自适应
    background: color-mix(in srgb, currentColor 10%, transparent);
    padding: 2px 6px;
    border-radius: 4px;
  }

  // JWT / 无鉴权两种状态：用 Element Plus 语义色 + color-mix 生成浅底，明暗双主题自适应
  &.auth-jwt {
    background: color-mix(in srgb, var(--el-color-warning) 10%, var(--el-bg-color));
    border: 1px solid color-mix(in srgb, var(--el-color-warning) 32%, transparent);
    color: var(--el-color-warning);
  }

  &.auth-none {
    background: color-mix(in srgb, var(--el-color-success) 10%, var(--el-bg-color));
    border: 1px solid color-mix(in srgb, var(--el-color-success) 32%, transparent);
    color: var(--el-color-success);
  }
}

.api-card {
  // 圆角对齐卡片令牌；这些卡用了 shadow="never"，全局静置阴影不会生效，
  // 故给容器自身一条 lighter 边框 + 静置卡片阴影令牌，保持与全站卡片观感一致（暗色自动适配）。
  border-radius: var(--dd-card-radius);
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: var(--dd-shadow-card);
  margin-bottom: 20px;
  overflow: hidden;

  :deep(.el-card__header) {
    background: var(--el-fill-color-light);
    border-bottom: 1px solid var(--el-border-color-lighter);
    padding: 12px 20px;
  }

  :deep(.el-card__body) {
    padding: 0;
  }
}

.section-title {
  position: relative;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  padding-left: 12px;
  margin: 0;

  &::before {
    content: '';
    position: absolute;
    left: 0;
    top: 3px;
    bottom: 3px;
    width: 3px;
    border-radius: 2px;
    background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-light-3));
  }
}

.param-section-header {
  padding: 8px 16px;
  background: var(--el-fill-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-weight: 600;
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.param-name {
  font-size: 12px;
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
}

.param-example {
  font-family: var(--dd-font-mono);
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.code-tabs {
  padding: 0 16px;

  :deep(.el-tabs__header) {
    margin-bottom: 0;
  }

  :deep(.el-tabs__item) {
    font-size: 12px;
    padding: 0 12px;
  }
}

.code-block-wrapper {
  position: relative;
  border-radius: 8px;
  overflow: hidden;
  margin: 0;

  &:hover .code-copy-btn {
    opacity: 1;
  }
}

.code-block {
  background: #1a1a2e !important;
  color: #e0e0e0;
  padding: 20px;
  margin: 0;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.7;
  overflow: auto;
  max-height: 340px;
  white-space: pre;
  tab-size: 2;
}

.code-copy-btn {
  position: absolute;
  top: 10px;
  right: 10px;
  z-index: 2;
  opacity: 0;
  transition: opacity 0.2s ease;
}

.response-status {
  padding: 16px 20px 0;
  display: flex;
  align-items: center;
  gap: 12px;
}

.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 600;

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--el-color-success);
    display: inline-block;
  }
}

// 200 成功胶囊：用 Element Plus success 语义色 + color-mix 浅底，明暗双主题自适应
.status-200 {
  background: color-mix(in srgb, var(--el-color-success) 12%, var(--el-bg-color));
  color: var(--el-color-success);
  border: 1px solid color-mix(in srgb, var(--el-color-success) 32%, transparent);
}

.response-type {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.response-section {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
  padding: 16px 20px;
}

.response-fields {
  flex: 1;
  min-width: 300px;
}

.response-example {
  flex: 1;
  min-width: 300px;
}

.response-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
}

@media (max-width: 768px) {
  .api-sider {
    display: none;
  }

  .page-header .mobile-menu-btn {
    display: inline-flex;
  }

  .api-content {
    padding: 16px;
  }

  .api-content :deep(.el-table) {
    display: block;
    overflow-x: auto;
  }

  .code-copy-btn {
    opacity: 1;
  }

  .response-section {
    flex-direction: column;
  }

  .url-bar {
    padding: 10px 14px;
  }
}

// ===== 入场动画 =====
// 与全站列表页统一：克制的淡入上移，时长走令牌（--dd-motion-page），
// prefers-reduced-motion 时令牌自动降为 1ms 即等效关闭。
@keyframes dd-apidocs-rise-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

// 进入时侧栏 + 右侧文档区整体淡入上移（只对两个卡级容器各做一次，不给每个 section 重 stagger）。
.api-sider,
.api-content {
  animation: dd-apidocs-rise-in var(--dd-motion-page) var(--dd-ease-decelerate) both;
}

// 轻微错落：侧栏先入，右侧文档区略晚
.api-content {
  animation-delay: 60ms;
}
</style>
