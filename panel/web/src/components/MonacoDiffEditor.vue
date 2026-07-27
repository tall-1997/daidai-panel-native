<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import type * as MonacoType from "monaco-editor";
import { loadMonacoEditor } from "@/utils/monaco";
import LoadingMotion from "./LoadingMotion.vue";

const props = withDefaults(
  defineProps<{
    originalValue: string;
    modifiedValue: string;
    language?: string;
    readonly?: boolean;
    renderSideBySide?: boolean;
    ignoreTrimWhitespace?: boolean;
    hideUnchangedRegions?: boolean;
    contextLineCount?: number;
  }>(),
  {
    language: "plaintext",
    readonly: true,
    renderSideBySide: true,
    ignoreTrimWhitespace: false,
    hideUnchangedRegions: false,
    contextLineCount: 3,
  },
);

const editorRef = ref<HTMLElement>();
const isLoading = ref(true);
const loadError = ref("");
let editor: MonacoType.editor.IStandaloneDiffEditor | null = null;
let monacoInstance: typeof MonacoType | null = null;
let originalModel: MonacoType.editor.ITextModel | null = null;
let modifiedModel: MonacoType.editor.ITextModel | null = null;
let resizeObserver: ResizeObserver | null = null;
let layoutTimer: ReturnType<typeof setTimeout> | null = null;

const DEFAULT_EDITOR_BACKGROUND = "#111827";
const DEFAULT_EDITOR_FOREGROUND = "#e5e7eb";

function readRootCssVar(name: string, fallback: string) {
  if (typeof window === "undefined") {
    return fallback;
  }
  const value = window
    .getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value || fallback;
}

function parseColor(color: string) {
  const text = color.trim();
  if (!text) return null;

  if (text.startsWith("#")) {
    const hex = text.slice(1);
    if (hex.length === 3) {
      const r = Number.parseInt(hex.charAt(0) + hex.charAt(0), 16);
      const g = Number.parseInt(hex.charAt(1) + hex.charAt(1), 16);
      const b = Number.parseInt(hex.charAt(2) + hex.charAt(2), 16);
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)
        ? null
        : { r, g, b };
    }
    if (hex.length === 6 || hex.length === 8) {
      const offset = hex.length === 8 ? 2 : 0;
      const r = Number.parseInt(hex.slice(offset, offset + 2), 16);
      const g = Number.parseInt(hex.slice(offset + 2, offset + 4), 16);
      const b = Number.parseInt(hex.slice(offset + 4, offset + 6), 16);
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)
        ? null
        : { r, g, b };
    }
  }

  const match = text.match(
    /^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*[0-9.]+\s*)?\)$/i,
  );
  if (!match) return null;

  const r = Number.parseInt(match[1] ?? "", 10);
  const g = Number.parseInt(match[2] ?? "", 10);
  const b = Number.parseInt(match[3] ?? "", 10);
  return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)
    ? null
    : { r, g, b };
}

function isDarkColor(color: string) {
  const rgb = parseColor(color);
  if (!rgb) {
    return true;
  }
  const toLinear = (channel: number) => {
    const value = channel / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  const luminance =
    0.2126 * toLinear(rgb.r) +
    0.7152 * toLinear(rgb.g) +
    0.0722 * toLinear(rgb.b);
  return luminance < 0.45;
}

function darkModeColor(
  background: string,
  darkValue: string,
  lightValue: string,
) {
  return isDarkColor(background) ? darkValue : lightValue;
}

function resolveEditorTheme() {
  const background = readRootCssVar(
    "--dd-editor-bg-color",
    DEFAULT_EDITOR_BACKGROUND,
  );
  const foreground = readRootCssVar(
    "--dd-editor-fg-color",
    DEFAULT_EDITOR_FOREGROUND,
  );
  const darkMode = isDarkColor(background);

  return {
    background,
    foreground,
    base: darkMode ? "vs-dark" : "vs",
    themeName: darkMode ? "dd-editor-dark" : "dd-editor-light",
  };
}

function disposeModels() {
  originalModel?.dispose();
  modifiedModel?.dispose();
  originalModel = null;
  modifiedModel = null;
}

function createModels() {
  if (!monacoInstance) return;

  disposeModels();
  originalModel = monacoInstance.editor.createModel(
    props.originalValue,
    props.language,
  );
  modifiedModel = monacoInstance.editor.createModel(
    props.modifiedValue,
    props.language,
  );
  editor?.setModel({
    original: originalModel,
    modified: modifiedModel,
  });
}

function clearLayoutTimer() {
  if (layoutTimer) {
    clearTimeout(layoutTimer);
    layoutTimer = null;
  }
}

function scheduleEditorLayout(delay = 0) {
  clearLayoutTimer();
  layoutTimer = setTimeout(() => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        editor?.layout();
      });
    });
  }, delay);
}

onMounted(async () => {
  if (!editorRef.value) return;

  try {
    loadError.value = "";
    const { monaco, source } = await loadMonacoEditor();
    monacoInstance = monaco as typeof MonacoType;
    if (!editorRef.value) return;
    isLoading.value = false;
    await nextTick();

    const theme = resolveEditorTheme();
    monacoInstance.editor.defineTheme(theme.themeName, {
      base: theme.base as "vs" | "vs-dark",
      inherit: true,
      rules: [],
      colors: {
        "editor.background": theme.background,
        "editor.foreground": theme.foreground,
        "editorLineNumber.foreground": darkModeColor(
          theme.background,
          "#6b7280",
          "#94a3b8",
        ),
        "editorCursor.foreground": darkModeColor(
          theme.background,
          "#34d399",
          "#2563eb",
        ),
        "editor.selectionBackground": darkModeColor(
          theme.background,
          "#134e4acc",
          "#bfdbfe",
        ),
        "editor.inactiveSelectionBackground": darkModeColor(
          theme.background,
          "#1f2937aa",
          "#dbeafe",
        ),
      },
    });

    editor = monacoInstance.editor.createDiffEditor(editorRef.value, {
      theme: theme.themeName,
      automaticLayout: true,
      readOnly: props.readonly,
      originalEditable: false,
      renderSideBySide: props.renderSideBySide,
      ignoreTrimWhitespace: props.ignoreTrimWhitespace,
      enableSplitViewResizing: true,
      scrollBeyondLastLine: false,
      fontSize: 14,
      minimap: { enabled: false },
      wordWrap: "on",
      diffWordWrap: "on",
      hideUnchangedRegions: {
        enabled: props.hideUnchangedRegions,
        contextLineCount: props.contextLineCount,
        minimumLineCount: 2,
      },
    });

    createModels();
    scheduleEditorLayout(30);

    if (typeof ResizeObserver !== "undefined" && editorRef.value) {
      resizeObserver = new ResizeObserver(() => {
        scheduleEditorLayout();
      });
      resizeObserver.observe(editorRef.value);
    }

    if (source === "cdn") {
      console.warn("Monaco Diff 编辑器当前已回退到 CDN 资源。");
    }
  } catch (error) {
    console.error("Monaco Diff 编辑器初始化失败", error);
    loadError.value = "对比编辑器加载失败，请检查网络或稍后重试。";
  } finally {
    if (loadError.value) {
      isLoading.value = false;
    }
  }
});

watch(
  () => props.originalValue,
  (newValue) => {
    if (originalModel && newValue !== originalModel.getValue()) {
      originalModel.setValue(newValue);
      scheduleEditorLayout();
    }
  },
);

watch(
  () => props.modifiedValue,
  (newValue) => {
    if (modifiedModel && newValue !== modifiedModel.getValue()) {
      modifiedModel.setValue(newValue);
      scheduleEditorLayout();
    }
  },
);

watch(
  () => props.language,
  (newLanguage) => {
    if (!monacoInstance) return;
    if (originalModel) {
      monacoInstance.editor.setModelLanguage(
        originalModel,
        newLanguage || "plaintext",
      );
    }
    if (modifiedModel) {
      monacoInstance.editor.setModelLanguage(
        modifiedModel,
        newLanguage || "plaintext",
      );
    }
    scheduleEditorLayout();
  },
);

watch(
  () => props.readonly,
  (newReadonly) => {
    editor?.updateOptions({ readOnly: newReadonly });
    scheduleEditorLayout();
  },
);

watch(
  () => props.renderSideBySide,
  (newValue) => {
    editor?.updateOptions({ renderSideBySide: newValue });
    scheduleEditorLayout(20);
  },
);

watch(
  () => props.ignoreTrimWhitespace,
  (newValue) => {
    editor?.updateOptions({ ignoreTrimWhitespace: newValue });
    scheduleEditorLayout(20);
  },
);

watch(
  () => props.hideUnchangedRegions,
  (newValue) => {
    editor?.updateOptions({
      hideUnchangedRegions: {
        enabled: newValue,
        contextLineCount: props.contextLineCount,
        minimumLineCount: 2,
      },
    });
    scheduleEditorLayout(20);
  },
);

watch(
  () => props.contextLineCount,
  (newValue) => {
    editor?.updateOptions({
      hideUnchangedRegions: {
        enabled: props.hideUnchangedRegions,
        contextLineCount: newValue,
        minimumLineCount: 2,
      },
    });
    scheduleEditorLayout(20);
  },
);

onBeforeUnmount(() => {
  clearLayoutTimer();
  resizeObserver?.disconnect();
  resizeObserver = null;
  editor?.setModel(null);
  editor?.dispose();
  editor = null;
  disposeModels();
});
</script>

<template>
  <div class="monaco-diff-wrapper">
    <div ref="editorRef" class="monaco-diff-container"></div>
    <div v-if="isLoading" class="monaco-diff-loading monaco-diff-overlay">
      <LoadingMotion
        variant="spinner"
        size="lg"
        label="对比编辑器加载中..."
        tone="primary"
      />
    </div>
    <div
      v-else-if="loadError"
      class="monaco-diff-loading monaco-diff-error monaco-diff-overlay"
    >
      <span>{{ loadError }}</span>
    </div>
  </div>
</template>

<style scoped>
.monaco-diff-wrapper {
  width: 100%;
  height: 100%;
  min-height: 420px;
  position: relative;
  overflow: hidden;
}

.monaco-diff-container {
  width: 100%;
  height: 100%;
}

.monaco-diff-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 420px;
  gap: 12px;
  background: var(--dd-editor-bg-color, #111827);
  color: var(--dd-editor-fg-color, #e5e7eb);
  border-radius: 4px;
  font-size: 14px;
}

.monaco-diff-overlay {
  position: absolute;
  inset: 0;
}

.monaco-diff-error {
  color: #f56c6c;
  text-align: center;
}
</style>
