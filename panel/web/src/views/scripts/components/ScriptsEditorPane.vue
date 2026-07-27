<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Clock,
  Close,
  Delete,
  Document,
  Download,
  Edit,
  MagicStick,
  MoreFilled,
  Plus,
  VideoPlay,
} from "@element-plus/icons-vue";
import MonacoEditor from "@/components/MonacoEditor.vue";
import { loadMonacoEditor } from "@/utils/monaco";

const fileContent = defineModel<string>("fileContent", { required: true });
const isEditing = defineModel<boolean>("isEditing", { required: true });

const props = defineProps<{
  isMobile: boolean;
  mobileShowEditor: boolean;
  selectedFile: string;
  isBinary: boolean;
  hasChanges: boolean;
  saving: boolean;
  formatting: boolean;
  loading: boolean;
  editorLanguage: string;
  editorAutoFocusTicket?: number;
  onMobileBack: () => void;
  onDebugRun: () => void | Promise<void>;
  onOpenCreateFile: () => void | Promise<void>;
  onAddToTask: () => void;
  onSave: () => void | Promise<void>;
  onCancelEdit: () => void | Promise<void>;
  onFormat: () => void | Promise<void>;
  onLoadVersions: () => void | Promise<void>;
  onOpenRename: () => void;
  onDownload: () => void;
  onDelete: () => void | Promise<void>;
}>();

const fileName = computed(() => {
  if (!props.selectedFile) return "";
  return props.selectedFile.split("/").pop() || props.selectedFile;
});

const filePath = computed(() => {
  if (!props.selectedFile) return "";
  const parts = props.selectedFile.split("/");
  parts.pop();
  return parts;
});

const languageLabel = computed(() => {
  const lang = (props.editorLanguage || "").toLowerCase();
  if (!lang) return "";
  const map: Record<string, string> = {
    javascript: "JS",
    typescript: "TS",
    python: "PY",
    shell: "SH",
    bash: "SH",
    yaml: "YAML",
    json: "JSON",
    markdown: "MD",
    html: "HTML",
    css: "CSS",
    go: "GO",
    plaintext: "TXT",
  };
  return map[lang] || lang.toUpperCase().slice(0, 4);
});

const fileSizeLabel = computed(() => {
  if (props.isBinary) return "";
  if (typeof fileContent.value !== "string") return "";
  const bytes = new Blob([fileContent.value]).size;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
});

const lineCountLabel = computed(() => {
  if (props.isBinary || !fileContent.value) return "";
  const count = fileContent.value.split("\n").length;
  return `${count} 行`;
});

function startEdit() {
  isEditing.value = true;
}

const monacoEditorRef = ref<{ focus?: () => void } | null>(null);

onMounted(() => {
  void loadMonacoEditor().catch((error) => {
    console.warn("Monaco 编辑器预加载失败，将在打开文件时重试。", error);
  });
});

watch(
  () =>
    [
      isEditing.value,
      props.editorAutoFocusTicket,
      props.loading,
      props.isBinary,
      props.selectedFile,
    ] as const,
  ([editing, focusTicket, loading, binary, file]) => {
    if (!editing || loading || binary || !file || !focusTicket) return;
    void nextTick(() => {
      monacoEditorRef.value?.focus?.();
    });
  },
);
</script>

<template>
  <section
    class="scripts-editor"
    :class="{ mobile: isMobile }"
    v-show="!isMobile || mobileShowEditor"
  >
    <!-- Empty state -->
    <div v-if="!selectedFile" class="editor-empty animate-fade-in-up">
      <div class="empty-card">
        <div class="empty-aura" aria-hidden="true"></div>
        <div class="empty-badge">
          <el-icon :size="20"><Plus /></el-icon>
        </div>
        <h2 class="empty-title">新建一个脚本开始使用</h2>
        <p class="empty-subtitle">
          从左侧选择已有脚本，或直接创建一个新文件并立即开始编辑、调试和添加任务。
        </p>
        <div class="empty-actions">
          <el-button
            class="create-cta"
            type="primary"
            size="large"
            @click="onOpenCreateFile"
          >
            <el-icon><Plus /></el-icon>新建脚本
          </el-button>
        </div>
      </div>
    </div>

    <template v-else>
      <!-- Hero header -->
      <header class="editor-hero animate-fade-in-up">
        <div class="hero-file">
          <el-button
            v-if="isMobile"
            class="mobile-back"
            text
            @click="onMobileBack"
            aria-label="返回文件列表"
          >
            <el-icon :size="18"><ArrowLeft /></el-icon>
          </el-button>
          <div class="file-icon" aria-hidden="true">
            <el-icon :size="18"><Document /></el-icon>
          </div>
          <div class="file-meta">
            <nav
              v-if="filePath.length > 0 && !isMobile"
              class="breadcrumb"
              aria-label="路径"
            >
              <template v-for="(seg, idx) in filePath" :key="idx">
                <span class="breadcrumb-seg">{{ seg }}</span>
                <el-icon
                  v-if="idx < filePath.length - 1"
                  class="breadcrumb-sep"
                  :size="10"
                >
                  <ArrowRight />
                </el-icon>
              </template>
            </nav>
            <div class="file-title-row">
              <h1 class="file-title" :title="selectedFile">{{ fileName }}</h1>
              <span v-if="languageLabel" class="file-pill file-pill--lang">{{
                languageLabel
              }}</span>
              <span v-if="isBinary" class="file-pill file-pill--binary"
                >二进制</span
              >
              <span
                v-else-if="fileSizeLabel && !isMobile"
                class="file-pill file-pill--muted"
                >{{ fileSizeLabel }}</span
              >
              <span
                v-if="hasChanges"
                class="unsaved-pulse"
                role="status"
                aria-label="文件有未保存的改动"
              >
                <span class="unsaved-dot"></span>
                <span class="unsaved-label">未保存</span>
              </span>
            </div>
          </div>
        </div>

        <div class="hero-actions">
          <el-button
            v-if="!isEditing"
            class="action-btn"
            :size="isMobile ? 'small' : 'default'"
            :disabled="isBinary"
            @click="startEdit"
          >
            <el-icon><Edit /></el-icon><span v-if="!isMobile">编辑</span>
          </el-button>
          <template v-else>
            <el-button
              class="action-btn action-btn--primary"
              type="primary"
              :size="isMobile ? 'small' : 'default'"
              :loading="saving"
              :disabled="!hasChanges || isBinary"
              @click="onSave"
            >
              <el-icon><Check /></el-icon><span v-if="!isMobile">保存</span>
            </el-button>
            <el-button
              class="action-btn action-btn--cancel"
              :size="isMobile ? 'small' : 'default'"
              :disabled="saving"
              @click="onCancelEdit"
            >
              <el-icon><Close /></el-icon><span v-if="!isMobile">退出编辑</span>
            </el-button>
          </template>

          <el-button
            class="action-btn action-btn--run"
            :size="isMobile ? 'small' : 'default'"
            :disabled="isBinary"
            @click="onDebugRun"
          >
            <el-icon><VideoPlay /></el-icon><span v-if="!isMobile">调试</span>
          </el-button>

          <el-button
            class="action-btn action-btn--task"
            :size="isMobile ? 'small' : 'default'"
            :disabled="isBinary"
            @click="onAddToTask"
          >
            <el-icon><Plus /></el-icon><span v-if="!isMobile">添加任务</span>
          </el-button>

          <el-dropdown trigger="click" placement="bottom-end">
            <el-button
              class="action-btn"
              :size="isMobile ? 'small' : 'default'"
              aria-label="更多操作"
            >
              <el-icon><MoreFilled /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item
                  v-if="isEditing"
                  @click="onFormat"
                  :disabled="isBinary"
                >
                  <el-icon><MagicStick /></el-icon>格式化
                </el-dropdown-item>
                <el-dropdown-item @click="onLoadVersions" :disabled="isBinary">
                  <el-icon><Clock /></el-icon>版本历史
                </el-dropdown-item>
                <el-dropdown-item @click="onOpenRename">
                  <el-icon><Edit /></el-icon>重命名
                </el-dropdown-item>
                <el-dropdown-item @click="onDownload">
                  <el-icon><Download /></el-icon>下载
                </el-dropdown-item>
                <el-dropdown-item divided @click="onDelete">
                  <el-icon><Delete /></el-icon>
                  <span style="color: var(--el-color-danger)">删除</span>
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <!-- Editor body -->
      <div class="editor-body animate-fade-in-up delay-50" v-loading="loading">
        <div v-if="isBinary" class="binary-card">
          <div class="binary-card-title">二进制文件</div>
          <p class="binary-card-text">
            该文件为二进制格式，无法在线编辑。可通过右上角「更多 →
            下载」取回文件。
          </p>
        </div>
        <MonacoEditor
          ref="monacoEditorRef"
          v-else
          v-model="fileContent"
          :language="editorLanguage"
          :readonly="!isEditing"
          class="code-editor"
        />
      </div>

      <!-- Status strip -->
      <footer v-if="!isBinary && selectedFile" class="editor-statusbar animate-fade-in-up delay-100">
        <div class="status-group">
          <span v-if="languageLabel" class="status-item status-item--lang">{{
            languageLabel
          }}</span>
          <span v-if="lineCountLabel" class="status-item">{{
            lineCountLabel
          }}</span>
          <span v-if="fileSizeLabel" class="status-item">{{
            fileSizeLabel
          }}</span>
        </div>
        <div class="status-group">
          <span class="status-item">UTF-8</span>
          <span class="status-item">LF</span>
          <span
            class="status-item"
            :class="{ 'status-item--accent': isEditing }"
          >
            {{ isEditing ? "编辑中" : "只读" }}
          </span>
        </div>
      </footer>
    </template>
  </section>
</template>

<style scoped lang="scss">
.scripts-editor {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  /* 卡片表面用令牌，明暗自动适配（卡片边框/圆角/阴影由 index.vue 负责） */
  background: var(--el-bg-color);
  animation: dd-editor-shell-in 360ms var(--dd-ease-emphasized) both;
  font-family: var(--dd-font-ui);
  overflow: hidden;
}

/* ---------------- Empty state ---------------- */
.editor-empty {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 24px;
}

.empty-card {
  position: relative;
  max-width: 480px;
  width: 100%;
  padding: 36px 32px 32px;
  text-align: center;
  border-radius: 16px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
  isolation: isolate;
}

.empty-aura {
  position: absolute;
  inset: -2px;
  z-index: -1;
  padding: 1px;
  border-radius: inherit;
  background: linear-gradient(
    135deg,
    color-mix(in srgb, #409eff 40%, transparent) 0%,
    color-mix(in srgb, #36cfc9 30%, transparent) 50%,
    color-mix(in srgb, #10b981 26%, transparent) 100%
  );
  -webkit-mask:
    linear-gradient(#000 0 0) content-box,
    linear-gradient(#000 0 0);
  -webkit-mask-composite: xor;
  mask-composite: exclude;
  opacity: 0.7;
  pointer-events: none;
}

.empty-badge {
  width: 44px;
  height: 44px;
  margin: 0 auto 14px;
  border-radius: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(135deg, #1890ff, #36cfc9);
  box-shadow: 0 6px 18px -8px rgba(24, 144, 255, 0.42);
}

.empty-title {
  font-size: 18px;
  font-weight: 600;
  margin: 0 0 6px;
  letter-spacing: 0.2px;
  color: var(--el-text-color-primary);
}

.empty-subtitle {
  font-size: 13px;
  line-height: 1.55;
  margin: 0 0 20px;
  color: var(--el-text-color-secondary);
}

.empty-actions {
  display: inline-flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: center;
}

.create-cta {
  background: linear-gradient(135deg, #2563eb, #0ea5e9);
  transition: transform 0.18s ease, box-shadow 0.2s ease, filter 0.2s ease;
  border: none;
  min-width: 160px;

  &:hover,
  &:focus {
    background: linear-gradient(135deg, #1d4ed8, #0284c7);
    border: none;
    transform: translateY(-1px);
    box-shadow: 0 14px 28px rgba(37, 99, 235, 0.2);
    filter: saturate(1.05);
  }
}

/* ---------------- Hero header ---------------- */
.editor-hero {
  padding: 14px 22px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  /* 卡片内顶部分区线（明暗令牌自适配） */
  border-bottom: 1px solid var(--el-border-color-lighter);
  box-shadow: 0 8px 18px rgba(15, 23, 42, 0.03);
  background: var(--el-bg-color);
  flex-shrink: 0;
  position: relative;
  min-width: 0;
}

.hero-file {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}

.file-icon {
  width: 36px;
  box-shadow: 0 8px 18px rgba(64, 158, 255, 0.08);
  transition: transform 0.18s ease, box-shadow 0.18s ease;
  height: 36px;
  border-radius: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--el-color-primary);
  background: rgba(64, 158, 255, 0.1);
  flex-shrink: 0;
}

.file-meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  color: var(--el-text-color-placeholder);
  line-height: 1.2;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;

  .breadcrumb-seg {
    font-family: var(--dd-font-mono);
    letter-spacing: 0.2px;
  }

  .breadcrumb-sep {
    opacity: 0.5;
    flex-shrink: 0;
  }
}

.file-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.file-title {
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.1px;
  color: var(--el-text-color-primary);
  margin: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-pill {
  display: inline-flex;
  transition: transform 0.16s ease, background-color 0.16s ease, color 0.16s ease;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  border-radius: 999px;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.4px;
  font-family: var(--dd-font-mono);
  line-height: 1;
}

.file-pill--lang {
  background: rgba(64, 158, 255, 0.1);
  color: var(--el-color-primary);
}

.file-pill--muted {
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
}

.file-pill--binary {
  background: rgba(230, 162, 60, 0.12);
  color: #e6a23c;
}

.unsaved-pulse {
  display: inline-flex;
  box-shadow: inset 0 0 0 1px rgba(230, 162, 60, 0.08);
  align-items: center;
  gap: 5px;
  height: 20px;
  padding: 0 9px 0 6px;
  border-radius: 999px;
  font-size: 11px;
  color: #e6a23c;
  background: rgba(230, 162, 60, 0.1);

  .unsaved-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #e6a23c;
    animation: unsaved-pulse 1.6s ease-in-out infinite;
  }

  .unsaved-label {
    font-weight: 600;
    letter-spacing: 0.3px;
  }
}

@keyframes unsaved-pulse {
  0%,
  100% {
    transform: scale(1);
    opacity: 1;
  }
  50% {
    transform: scale(1.25);
    opacity: 0.55;
  }
}

@media (prefers-reduced-motion: reduce) {
  .unsaved-dot {
    animation: none;
  }
}

.hero-actions {
  display: inline-flex;
  padding: 4px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--el-fill-color-light) 84%, transparent);
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.action-btn {
  border-radius: 8px;
  font-weight: 500;
  transition: transform 0.16s ease, box-shadow 0.18s ease, border-color 0.18s ease;

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 8px 18px rgba(15, 23, 42, 0.08);
  }

  &:active {
    transform: scale(0.985);
  }
}

.action-btn--primary {
  box-shadow: 0 4px 12px -6px
    color-mix(in srgb, var(--el-color-primary) 50%, transparent);
}

.action-btn--cancel {
  color: var(--el-text-color-regular);

  &:hover:not(.is-disabled) {
    color: var(--el-color-danger);
    border-color: color-mix(
      in srgb,
      var(--el-color-danger) 40%,
      var(--el-border-color)
    );
    background: color-mix(in srgb, var(--el-color-danger) 6%, transparent);
  }
}

.action-btn--run {
  --el-button-bg-color: rgba(103, 194, 58, 0.12);
  --el-button-border-color: rgba(103, 194, 58, 0.35);
  --el-button-hover-bg-color: rgba(103, 194, 58, 0.2);
  --el-button-hover-border-color: #67c23a;
  --el-button-hover-text-color: #67c23a;
  --el-button-text-color: #67c23a;
}

.action-btn--task {
  --el-button-bg-color: rgba(37, 99, 235, 0.1);
  --el-button-border-color: rgba(37, 99, 235, 0.28);
  --el-button-hover-bg-color: rgba(37, 99, 235, 0.16);
  --el-button-hover-border-color: rgba(37, 99, 235, 0.55);
  --el-button-hover-text-color: #2563eb;
  --el-button-text-color: #2563eb;

  &.is-disabled {
    opacity: 0.72;
  }
}

/* ---------------- Editor body ---------------- */
.editor-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;

  .code-editor {
    flex: 1;
    height: 100%;
    min-height: 0;
  }
}

.binary-card {
  margin: 24px;
  padding: 24px 28px;
  border: 1px dashed var(--el-border-color);
  border-radius: 12px;
  background: var(--el-fill-color-light);

  .binary-card-title {
    font-size: 14px;
    font-weight: 600;
    font-family: var(--dd-font-mono);
    letter-spacing: 0.4px;
    color: var(--el-text-color-primary);
    margin-bottom: 6px;
  }

  .binary-card-text {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    margin: 0;
    line-height: 1.55;
  }
}

/* ---------------- Status bar ---------------- */
.editor-statusbar {
  flex-shrink: 0;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.6);
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 18px;
  /* 卡片内底部分区线 + 次级底色（明暗令牌自适配） */
  border-top: 1px solid var(--el-border-color-lighter);
  font-family: var(--dd-font-mono);
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  background: var(--el-fill-color-light);
}

.status-group {
  display: inline-flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.status-item {
  letter-spacing: 0.4px;
  transition: color 0.16s ease, opacity 0.16s ease;
}

.status-item--lang {
  color: var(--el-color-primary);
  font-weight: 600;
}

.status-item--accent {
  color: #67c23a;
  font-weight: 600;
}

/* ---------------- Mobile ---------------- */
.mobile-back {
  padding: 4px;
  margin-right: -2px;
}

.scripts-editor.mobile {
  .editor-hero {
    padding: 10px 12px;
    gap: 8px;
    align-items: flex-start;
    flex-wrap: wrap;

    .hero-file {
      gap: 8px;
      width: 100%;
      min-width: 0;
    }

    .file-icon {
      width: 30px;
      height: 30px;
      border-radius: 8px;
    }

    .file-title {
      font-size: 15px;
    }

    .hero-actions {
      width: 100%;
      justify-content: flex-end;
      gap: 6px;
      flex-wrap: wrap;
    }
  }

  .editor-body {
    .code-editor {
      min-height: 0;
    }
  }

  .editor-statusbar {
    padding: 5px 12px;
    font-size: 10.5px;
  }

  .empty-card {
    padding: 28px 20px;
  }
}


@keyframes dd-editor-shell-in {
  from {
    opacity: 0;
    transform: translate3d(0, 12px, 0);
  }
  to {
    opacity: 1;
    transform: translate3d(0, 0, 0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .scripts-editor {
    animation: none;
  }
}

</style>
