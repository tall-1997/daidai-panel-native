<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import type * as MonacoType from "monaco-editor";
import { loadMonacoEditor } from "@/utils/monaco";
import LoadingMotion from "./LoadingMotion.vue";

const props = defineProps<{
  modelValue: string;
  language?: string;
  readonly?: boolean;
  minHeight?: string | number;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const editorRef = ref<HTMLElement>();
const isLoading = ref(true);
const loadError = ref("");
let editor: MonacoType.editor.IStandaloneCodeEditor | null = null;
let monacoInstance: typeof MonacoType | null = null;

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

onMounted(async () => {
  if (!editorRef.value) return;

  try {
    loadError.value = "";
    const { monaco, source } = await loadMonacoEditor();
    monacoInstance = monaco as typeof MonacoType;
    if (!editorRef.value) return;
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

    editor = monacoInstance.editor.create(editorRef.value, {
      value: props.modelValue,
      language: props.language || "javascript",
      theme: theme.themeName,
      automaticLayout: true,
      fontSize: 14,
      minimap: { enabled: true },
      scrollBeyondLastLine: false,
      readOnly: props.readonly || false,
      tabSize: 2,
      wordWrap: "on",
    });

    if (source === "cdn") {
      console.warn("Monaco 编辑器当前已回退到 CDN 资源。");
    }

    editor!.onDidChangeModelContent(() => {
      if (editor) {
        emit("update:modelValue", editor.getValue());
      }
    });
  } catch (error) {
    console.error("Monaco 编辑器初始化失败", error);
    loadError.value = "编辑器加载失败，请检查网络或稍后重试。";
  } finally {
    isLoading.value = false;
  }
});

watch(
  () => props.modelValue,
  (newValue) => {
    if (editor && newValue !== editor.getValue()) {
      editor.setValue(newValue);
    }
  },
);

watch(
  () => props.language,
  (newLang) => {
    if (editor && monacoInstance) {
      const model = editor.getModel();
      if (model) {
        monacoInstance.editor.setModelLanguage(model, newLang || "javascript");
      }
    }
  },
);

watch(
  () => props.readonly,
  (newReadonly) => {
    if (editor) {
      editor.updateOptions({ readOnly: newReadonly });
    }
  },
);

onBeforeUnmount(() => {
  editor?.dispose();
  editor = null;
});

defineExpose({
  format: () => {
    if (editor) {
      editor.getAction("editor.action.formatDocument")?.run();
    }
  },
  focus: () => editor?.focus(),
  getValue: () => editor?.getValue() || "",
  setValue: (value: string) => editor?.setValue(value),
});

function darkModeColor(
  background: string,
  darkValue: string,
  lightValue: string,
) {
  return isDarkColor(background) ? darkValue : lightValue;
}

function resolveMinHeight(value: string | number | undefined) {
  if (typeof value === "number") {
    return `${value}px`;
  }
  if (typeof value === "string" && value.trim()) {
    return value;
  }
  return "400px";
}
</script>

<template>
  <div
    class="monaco-editor-wrapper"
    :style="{ '--monaco-editor-min-height': resolveMinHeight(props.minHeight) }"
  >
    <div v-if="isLoading" class="monaco-loading">
      <LoadingMotion
        variant="spinner"
        size="lg"
        label="编辑器加载中..."
        tone="primary"
      />
    </div>
    <div v-else-if="loadError" class="monaco-loading monaco-error">
      <span>{{ loadError }}</span>
    </div>
    <div
      ref="editorRef"
      class="monaco-editor-container"
      v-show="!isLoading && !loadError"
    ></div>
  </div>
</template>

<style scoped>
.monaco-editor-wrapper {
  width: 100%;
  min-height: var(--monaco-editor-min-height, 400px);
  height: var(--monaco-editor-min-height, 400px);
  position: relative;
}

.monaco-editor-container {
  width: 100%;
  height: 100%;
  /* Monaco 初始化时只读取容器 clientHeight。
     普通卡片没有固定父级高度时，内层容器必须继承最小高度，否则会被算成 0 高度，只剩一条横线且无法输入。 */
  min-height: inherit;
}

.monaco-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: var(--monaco-editor-min-height, 400px);
  gap: 12px;
  color: var(--el-text-color-secondary);
  font-size: 14px;
  background: var(--dd-editor-bg-color, #111827);
  color: var(--dd-editor-fg-color, #e5e7eb);
  border-radius: 4px;
}

.monaco-error {
  color: #f56c6c;
  text-align: center;
}
</style>
