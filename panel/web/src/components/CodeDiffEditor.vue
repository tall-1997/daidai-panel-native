<script setup lang="ts">
import {
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  provide,
  ref,
  shallowRef,
} from "vue";
import {
  EDITOR_ENGINE_CHANGE_EVENT,
  readStoredEditorEngine,
  resolveEditorEngine,
} from "@/utils/editorEngine";
import {
  MONACO_LOADING_DELAY_MS,
  MONACO_LOAD_RETRY_KEY,
  MonacoDiffLoadError,
  MonacoDiffLoading,
} from "@/utils/monacoWarmup";
// 失败态按钮的样式在这里引、不在首屏的 monacoWarmup.ts 里引，理由同 CodeEditor.vue 那句
import "element-plus/theme-chalk/el-button.css";

/**
 * 版本对比编辑器（分发层）。
 *
 * 本组件自己不画任何东西，只负责按引擎偏好在两个实现之间二选一：
 *   - CodeMirrorDiffEditor.vue：默认实现，也是移动端唯一实现；
 *   - MonacoDiffEditor.vue：桌面端可选。
 * 判定规则（含「触摸设备硬回落 CodeMirror」）全在 utils/editorEngine.ts 里，这里不重复判断。
 *
 * props 与两个实现逐字一致，无 emit、无具名导出、无 defineExpose ——
 * 版本对比是只读的一次性视图，调用点（ScriptManageDialogs.vue 的「版本对比」弹窗）
 * 只往里传值，不从里面取任何东西。
 *
 * 🔴 模板必须是**单个** `<component :is>` 根节点：不套 wrapper `<div>`，也**不在根上放注释**
 * （dev 构建会保留注释节点，根就变成 Fragment，$attrs 透传要靠 DEV_ROOT_FRAGMENT 那套
 * dev 专用机制兜，平白让 dev 与 prod 走两条不同的路径；同样的约束见 CodeEditor.vue）。
 * 理由：调用点是 `<CodeDiffEditor class="version-diff-editor" />`，而那个类挂着
 * `flex: 1 1 0` / `min-height: 0` 这类只对「弹窗 flex 容器的直接子元素」成立的规则。
 * 套一层的话类落在中间那层 div 上，真正的编辑器拿到的是没有高度约束的父容器，
 * 表现是对比区被压成一条或者把弹窗顶穿。
 * 同理本组件不写 `<style>`：布局由调用点给，配色由两个实现各自给。
 */
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

// 两个实现都异步引入：Monaco 那份必须懒加载（不切过去的用户一个字节都不该下载），
// CodeMirror 那份也顺手切出去 —— @codemirror/merge 只有这个弹窗用得到。
// 本组件在调用点本来就是 defineAsyncComponent 引入的，再切一层不会多出一次可感知的等待。
const CodeMirrorDiffEditor = defineAsyncComponent(
  () => import("./CodeMirrorDiffEditor.vue"),
);

// Monaco 那份带加载态与失败态（#133 / #126）：loader 同时拉 monacoEngine、「重试」换一个新定义，
// 理由与 CodeEditor.vue 里 MonacoEditor 那段注释逐条相同（那边写得全，这里不重抄）。
// CodeMirror 那份不加：失败态里的「改用 CodeMirror」对它没有意义；它加载失败时仍由
// main.ts 的 vite:preloadError 自动刷新兜底。
function createMonacoDiffComponent() {
  return defineAsyncComponent({
    loader: () =>
      Promise.all([
        import("./MonacoDiffEditor.vue"),
        import("@/utils/monacoEngine"),
      ]).then(([diffModule]) => diffModule),
    loadingComponent: MonacoDiffLoading,
    delay: MONACO_LOADING_DELAY_MS,
    errorComponent: MonacoDiffLoadError,
  });
}
const MonacoDiffEditor = shallowRef(createMonacoDiffComponent());
provide(MONACO_LOAD_RETRY_KEY, () => {
  MonacoDiffEditor.value = createMonacoDiffComponent();
});

const engine = ref(resolveEditorEngine(readStoredEditorEngine()));

/**
 * 用户在齿轮菜单里切了引擎。
 * 换引擎必然意味着换实现组件，Vue 会拆掉旧实例重建新的，对比视图重新渲染一遍即可 ——
 * 它是只读视图，没有未保存内容会丢。
 */
function syncEngine() {
  engine.value = resolveEditorEngine(readStoredEditorEngine());
}

onMounted(() => {
  window.addEventListener(EDITOR_ENGINE_CHANGE_EVENT, syncEngine);
});

onBeforeUnmount(() => {
  window.removeEventListener(EDITOR_ENGINE_CHANGE_EVENT, syncEngine);
});
</script>

<template>
  <component
    :is="engine === 'monaco' ? MonacoDiffEditor : CodeMirrorDiffEditor"
    :original-value="props.originalValue"
    :modified-value="props.modifiedValue"
    :language="props.language"
    :readonly="props.readonly"
    :render-side-by-side="props.renderSideBySide"
    :ignore-trim-whitespace="props.ignoreTrimWhitespace"
    :hide-unchanged-regions="props.hideUnchangedRegions"
    :context-line-count="props.contextLineCount"
  />
</template>
